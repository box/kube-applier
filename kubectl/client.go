// Package kubectl provides a kubectl Client for interacting with the kubernetes
// apiserver.
package kubectl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/utilitywarehouse/kube-applier/metrics"
)

var (
	// The output is omitted if it contains any of these terms when there is an
	// error running `kubectl apply -f <path>`
	omitErrOutputTerms   = []string{"Secret", "base64"}
	omitErrOutputMessage = "Some error output has been omitted because it may contain sensitive data\n"

	// Used in sanitiseCmdStr
	sanitiseCmdStrRe = regexp.MustCompile(`--token=[\S]+`)
)

func sanitiseCmdStr(cmdStr string) string {
	return sanitiseCmdStrRe.ReplaceAllString(cmdStr, "--token=<omitted>")
}

// ApplyOptions configure kubectl apply
type ApplyOptions struct {
	DryRunStrategy string
	Environment    []string
	Namespace      string
	PruneWhitelist []string
	ServerSide     bool
	Token          string
}

// Args returns the flags that should be passed to exec.Command
func (o *ApplyOptions) Args() []string {
	args := []string{}

	if o.Token != "" {
		args = append(args, fmt.Sprintf("--token=%s", o.Token))
	}

	if o.Namespace != "" {
		args = append(args, []string{"-n", o.Namespace}...)
	}

	if o.DryRunStrategy != "" {
		args = append(args, fmt.Sprintf("--dry-run=%s", o.DryRunStrategy))
	}

	if o.ServerSide {
		args = append(args, []string{"--server-side", "--force-conflicts"}...)
	}

	if len(o.PruneWhitelist) > 0 {
		args = append(args, []string{"--prune", "--all"}...)
		for _, w := range o.PruneWhitelist {
			args = append(args, "--prune-whitelist="+w)
		}
	}

	return args
}

func (o *ApplyOptions) setCommandEnvironment(cmd *exec.Cmd) {
	if len(o.Environment) > 0 {
		cmd.Env = append(os.Environ(), o.Environment...)
	}
}

// Client enables communication with the Kubernetes API Server through kubectl commands.
type Client struct {
	Host        string
	Label       string
	KubeCtlPath string
	KubeCtlOpts []string
}

func NewClient(host, label, kubeCtlPath string, kubeCtlOpts []string) *Client {
	if kubeCtlPath == "" {
		kubeCtlPath = exec.Command("kubectl").String()
	}
	return &Client{
		Host:        host,
		Label:       label,
		KubeCtlPath: kubeCtlPath,
		KubeCtlOpts: kubeCtlOpts,
	}
}

// Apply attempts to "kubectl apply" the files located at path. It returns the
// full apply command and its output.
func (c *Client) Apply(ctx context.Context, path string, options ApplyOptions) (string, string, error) {
	var kustomize bool
	if _, err := os.Stat(path + "/kustomization.yaml"); err == nil {
		kustomize = true
	} else if _, err := os.Stat(path + "/kustomization.yml"); err == nil {
		kustomize = true
	} else if _, err := os.Stat(path + "/Kustomization"); err == nil {
		kustomize = true
	}
	if kustomize {
		cmd, out, err := c.applyKustomize(ctx, path, options)
		return sanitiseCmdStr(cmd), out, err
	}
	cmd, out, err := c.applyPath(ctx, path, options)
	return sanitiseCmdStr(cmd), out, err
}

// KubectlPath returns the filesystem path to the kubectl binary
func (c *Client) KubectlPath() string {
	kubectlCmd := exec.Command(c.KubeCtlPath)
	return kubectlCmd.String()
}

// KustomizePath returns the filesystem path to the kustomize binary
func (c *Client) KustomizePath() string {
	kustomizeCmd := exec.Command("kustomize")
	return kustomizeCmd.String()
}

// applyPath runs `kubectl apply -f <path>`
func (c *Client) applyPath(ctx context.Context, path string, options ApplyOptions) (string, string, error) {
	cmdStr, out, err := c.apply(ctx, path, []byte{}, options)
	if err != nil {
		// Filter potential secret leaks out of the output
		return cmdStr, filterErrOutput(out), err
	}

	return cmdStr, out, nil
}

// applyKustomize does a `kustomize build | kubectl apply -f -` on the path
func (c *Client) applyKustomize(ctx context.Context, path string, options ApplyOptions) (string, string, error) {
	var kustomizeStdout, kustomizeStderr bytes.Buffer

	kustomizeCmd := exec.CommandContext(ctx, "kustomize", "build", path)
	options.setCommandEnvironment(kustomizeCmd)
	kustomizeCmd.Stdout = &kustomizeStdout
	kustomizeCmd.Stderr = &kustomizeStderr

	err := kustomizeCmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			err = errors.Wrap(ctx.Err(), err.Error())
		}
		return kustomizeCmd.String(), kustomizeStderr.String(), err
	}

	// Split the stdout into secrets and other resources
	stdout, err := io.ReadAll(&kustomizeStdout)
	if err != nil {
		return kustomizeCmd.String(), "error reading kustomize output", err
	}
	resources, secrets, err := splitSecrets(stdout)
	if err != nil {
		return kustomizeCmd.String(), "error extracting secrets from kustomize output", err
	}
	if len(resources) == 0 && len(secrets) == 0 {
		return kustomizeCmd.String(), "", fmt.Errorf("No resources were extracted from the kustomize output")
	}

	// This is the command we are effectively applying. In actuality we're splitting it into two
	// separate invocations of kubectl but we'll return this as the command
	// string.
	displayArgs := []string{}
	if c.Host != "" {
		displayArgs = append(displayArgs, "--server", c.Host)
	}
	displayArgs = append(displayArgs, "apply", "-f", "-")
	displayArgs = append(displayArgs, options.Args()...)
	// Add opts that are specific to this client
	displayArgs = append(c.KubeCtlOpts, displayArgs...)
	kubectlCmd := exec.Command(c.KubeCtlPath, displayArgs...)
	cmdStr := kustomizeCmd.String() + " | " + kubectlCmd.String()

	var kubectlOut string

	if len(resources) > 0 {
		// Don't prune secrets
		resourcesPruneWhitelist := []string{}
		for _, w := range options.PruneWhitelist {
			if w != "core/v1/Secret" {
				resourcesPruneWhitelist = append(resourcesPruneWhitelist, w)
			}
		}

		resourcesOptions := options
		resourcesOptions.PruneWhitelist = resourcesPruneWhitelist

		_, out, err := c.apply(ctx, "-", resources, resourcesOptions)
		kubectlOut = kubectlOut + out
		if err != nil {
			return cmdStr, kubectlOut, err
		}
	}

	if len(secrets) > 0 {
		// Only prune secrets
		var secretsPruneWhitelist []string
		for _, w := range options.PruneWhitelist {
			if w == "core/v1/Secret" {
				secretsPruneWhitelist = append(secretsPruneWhitelist, w)
			}
		}

		secretsOptions := options
		secretsOptions.PruneWhitelist = secretsPruneWhitelist

		_, out, err := c.apply(ctx, "-", secrets, secretsOptions)
		if err != nil {
			// Don't append the actual output, as the error output
			// from kubectl can leak the content of secrets
			kubectlOut = kubectlOut + omitErrOutputMessage
			return cmdStr, kubectlOut, err
		}
		kubectlOut = kubectlOut + out
	}

	return cmdStr, kubectlOut, nil
}

// apply runs `kubectl apply`
func (c *Client) apply(ctx context.Context, path string, stdin []byte, options ApplyOptions) (string, string, error) {
	args := []string{}
	if c.Host != "" {
		args = append(args, "--server", c.Host)
	}
	args = append(args, "apply", "-f", path)
	if path != "-" {
		args = append(args, "-R")
	}
	args = append(args, options.Args()...)
	// Add opts that are specific to this client
	args = append(c.KubeCtlOpts, args...)

	kubectlCmd := exec.CommandContext(ctx, c.KubeCtlPath, args...)
	options.setCommandEnvironment(kubectlCmd)
	if path == "-" {
		if len(stdin) == 0 {
			return "", "", fmt.Errorf("path can't be %s when stdin is empty", path)
		}
		kubectlCmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := kubectlCmd.CombinedOutput()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			metrics.UpdateKubectlExitCodeCount(options.Namespace, e.ExitCode())
		}
		if ctx.Err() == context.DeadlineExceeded {
			err = errors.Wrap(ctx.Err(), err.Error())
		}
		return kubectlCmd.String(), string(out), err
	}
	metrics.UpdateKubectlExitCodeCount(options.Namespace, 0)

	return kubectlCmd.String(), string(out), nil
}

// filterErrOutput squashes output that may contain potentially sensitive
// information
func filterErrOutput(out string) string {
	for _, term := range omitErrOutputTerms {
		if strings.Contains(out, term) {
			return omitErrOutputMessage
		}
	}

	return out
}

// splitSecrets will take a yaml file and separate the resources into Secrets
// and other resources. This allows Secrets to be applied separately to other
// resources.
func splitSecrets(yamlData []byte) (resources, secrets []byte, err error) {
	objs, err := splitYAML(yamlData)
	if err != nil {
		return resources, secrets, err
	}

	secretsDocs := [][]byte{}
	resourcesDocs := [][]byte{}
	for _, obj := range objs {
		y, err := yaml.Marshal(obj)
		if err != nil {
			return resources, secrets, err
		}
		if obj.Object["kind"] == "Secret" {
			secretsDocs = append(secretsDocs, y)
		} else {
			resourcesDocs = append(resourcesDocs, y)
		}
	}

	secrets = bytes.Join(secretsDocs, []byte("---\n"))
	resources = bytes.Join(resourcesDocs, []byte("---\n"))

	return resources, secrets, nil
}

// splitYAML splits a YAML file into unstructured objects. Returns list of all unstructured objects
// found in the yaml. If an error occurs, returns objects that have been parsed so far too.
//
// Taken from the gitops-engine:
//  - https://github.com/argoproj/gitops-engine/blob/cc0fb5531c29c193291a7f97a50921f544b2d3b9/pkg/utils/kube/kube.go#L282-L310
func splitYAML(yamlData []byte) ([]*unstructured.Unstructured, error) {
	// Similar way to what kubectl does
	// https://github.com/kubernetes/cli-runtime/blob/master/pkg/resource/visitor.go#L573-L600
	// Ideally k8s.io/cli-runtime/pkg/resource.Builder should be used instead of this method.
	// E.g. Builder does list unpacking and flattening and this code does not.
	d := kubeyaml.NewYAMLOrJSONDecoder(bytes.NewReader(yamlData), 4096)
	var objs []*unstructured.Unstructured
	for {
		ext := runtime.RawExtension{}
		if err := d.Decode(&ext); err != nil {
			if err == io.EOF {
				break
			}
			return objs, fmt.Errorf("failed to unmarshal manifest: %v", err)
		}
		ext.Raw = bytes.TrimSpace(ext.Raw)
		if len(ext.Raw) == 0 || bytes.Equal(ext.Raw, []byte("null")) {
			continue
		}
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal(ext.Raw, u); err != nil {
			return objs, fmt.Errorf("failed to unmarshal manifest: %v", err)
		}
		objs = append(objs, u)
	}
	return objs, nil
}
