package kubectl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

var (
	// To make testing possible
	execCommand = exec.Command
	// The output is omitted if it contains any of these terms
	// when there is an error running `kubectl apply -f <path>`
	omitErrOutputTerms   = []string{"Secret", "base64"}
	omitErrOutputMessage = "Some error output has been omitted because it may contain sensitive data\n"
)

// ClientInterface allows for mocking out the functionality of Client when testing the full process of an apply run.
type ClientInterface interface {
	Apply(path, namespace, dryRunStrategy string, kustomize bool, pruneWhitelist []string) (string, string, error)
}

// Client enables communication with the Kubernetes API Server through kubectl commands.
type Client struct {
	Label   string
	Metrics metrics.PrometheusInterface
	Timeout time.Duration
}

// Apply attempts to "kubectl apply" the files located at path. It returns the
// full apply command and its output.
func (c *Client) Apply(path, namespace, dryRunStrategy string, kustomize bool, pruneWhitelist []string) (string, string, error) {
	if kustomize {
		return c.applyKustomize(path, namespace, dryRunStrategy, pruneWhitelist)
	}
	return c.applyPath(path, namespace, dryRunStrategy, pruneWhitelist)
}

// applyPath runs `kubectl apply -f <path>`
func (c *Client) applyPath(path, namespace, dryRunStrategy string, pruneWhitelist []string) (string, string, error) {
	cmdStr, out, err := c.apply(path, namespace, dryRunStrategy, pruneWhitelist, []byte{})
	if err != nil {
		// Filter potential secret leaks out of the output
		return cmdStr, filterErrOutput(out), err
	}

	return cmdStr, out, nil
}

// applyKustomize does a `kustomize build | kubectl apply -f -` on the path
func (c *Client) applyKustomize(path, namespace, dryRunStrategy string, pruneWhitelist []string) (string, string, error) {
	var kustomizeStdout, kustomizeStderr bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	kustomizeCmd := exec.CommandContext(ctx, "kustomize", "build", path)
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
	stdout, err := ioutil.ReadAll(&kustomizeStdout)
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
	displayArgs := applyArgs("-", namespace, dryRunStrategy, pruneWhitelist)
	kubectlCmd := exec.Command(displayArgs[0], displayArgs[1:]...)
	cmdStr := kustomizeCmd.String() + " | " + kubectlCmd.String()

	var kubectlOut string

	if len(resources) > 0 {
		// Don't prune secrets
		resourcesPruneWhitelist := []string{}
		for _, w := range pruneWhitelist {
			if w != "core/v1/Secret" {
				resourcesPruneWhitelist = append(resourcesPruneWhitelist, w)
			}
		}

		_, out, err := c.apply("-", namespace, dryRunStrategy, resourcesPruneWhitelist, resources)
		kubectlOut = kubectlOut + out
		if err != nil {
			return cmdStr, kubectlOut, err
		}
	}

	if len(secrets) > 0 {
		// Only prune secrets
		var secretsPruneWhitelist []string
		for _, w := range pruneWhitelist {
			if w == "core/v1/Secret" {
				secretsPruneWhitelist = append(secretsPruneWhitelist, w)
			}
		}

		_, out, err := c.apply("-", namespace, dryRunStrategy, secretsPruneWhitelist, secrets)
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
func (c *Client) apply(path, namespace, dryRunStrategy string, pruneWhitelist []string, stdin []byte) (string, string, error) {
	args := applyArgs(path, namespace, dryRunStrategy, pruneWhitelist)

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	kubectlCmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if path == "-" {
		if len(stdin) == 0 {
			return "", "", fmt.Errorf("path can't be %s when stdin is empty", path)
		}
		kubectlCmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := kubectlCmd.CombinedOutput()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			c.Metrics.UpdateKubectlExitCodeCount(namespace, e.ExitCode())
		}
		if ctx.Err() == context.DeadlineExceeded {
			err = errors.Wrap(ctx.Err(), err.Error())
		}
		return kubectlCmd.String(), string(out), err
	}
	c.Metrics.UpdateKubectlExitCodeCount(namespace, 0)

	return kubectlCmd.String(), string(out), nil
}

// applyArgs constructs the arguments for `kubectl apply`
func applyArgs(path, namespace, dryRunStrategy string, pruneWhitelist []string) []string {
	args := []string{"kubectl", "apply"}

	if path != "" {
		args = append(args, []string{"-f", path}...)
		if path != "-" {
			args = append(args, "-R")
		}
	}

	if namespace != "" {
		args = append(args, []string{"-n", namespace}...)
	}

	if dryRunStrategy != "" {
		args = append(args, fmt.Sprintf("--dry-run=%s", dryRunStrategy))
	}

	if len(pruneWhitelist) > 0 {
		args = append(args, []string{"--prune", "--all"}...)
		for _, w := range pruneWhitelist {
			args = append(args, "--prune-whitelist="+w)
		}
	}

	return args
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
