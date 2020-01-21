package kube

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

const (
	// Default location of the service-account token on the cluster
	tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// Location of the kubeconfig template file within the container - see ADD command in Dockerfile
	kubeconfigTemplatePath = "/templates/kubeconfig"

	// Location of the written kubeconfig file within the container
	kubeconfigFilePath = "/etc/kubeconfig"

	enabledAnnotation        = "kube-applier.io/enabled"
	dryRunAnnotation         = "kube-applier.io/dry-run"
	pruneWhitelistAnnotation = "kube-applier.io/prune-whitelist"
)

// To make testing possible
var execCommand = exec.Command

// KAAnnotations contains the standard set of annotations on the Namespace
// resource defining behaviour for that Namespace
type KAAnnotations struct {
	Enabled        string
	DryRun         string
	PruneWhitelist string
}

// ClientInterface allows for mocking out the functionality of Client when testing the full process of an apply run.
type ClientInterface interface {
	Apply(path, namespace string, dryRun, kustomize bool, pruneWhitelist []string) (string, string, error)
	NamespaceAnnotations(namespace string) (KAAnnotations, error)
}

// Client enables communication with the Kubernetes API Server through kubectl commands.
// The Server field enables discovery of the API server when kube-proxy is not configured (see README.md for more information).
type Client struct {
	Server  string
	Label   string
	Metrics metrics.PrometheusInterface
}

// Configure writes the kubeconfig file to be used for authenticating kubectl commands.
func (c *Client) Configure() error {
	// No need to write a kubeconfig file if Server is not specified (API server will be discovered via kube-proxy).
	if c.Server == "" {
		return nil
	}

	f, err := os.Create(kubeconfigFilePath)
	if err != nil {
		return errors.Wrap(err, "creating kubeconfig file failed")
	}
	defer f.Close()

	token, err := ioutil.ReadFile(tokenPath)
	if err != nil {
		return errors.Wrap(err, "cannot access token for kubeconfig file")
	}

	var data struct {
		Token  string
		Server string
	}
	data.Token = string(token)
	data.Server = c.Server

	template, err := sysutil.CreateTemplate(kubeconfigTemplatePath)
	if err != nil {
		return errors.Wrap(err, "parsing kubeconfig template failed")
	}
	if err := template.Execute(f, data); err != nil {
		return errors.Wrap(err, "applying kubeconfig template failed")
	}

	return nil
}

// Apply attempts to "kubectl apply" the files located at path. It returns the
// full apply command and its output.
//
// kustomize - Do a `kustomize build | kubectl apply -f -` on the path, set to if there is a
//             `kustomization.yaml` found in the path
func (c *Client) Apply(path, namespace string, dryRun, kustomize bool, pruneWhitelist []string) (string, string, error) {
	var args []string

	if kustomize {
		args = []string{"kubectl", "apply", fmt.Sprintf("--server-dry-run=%t", dryRun), "-f", "-", "-n", namespace}
	} else {
		args = []string{"kubectl", "apply", fmt.Sprintf("--server-dry-run=%t", dryRun), "-R", "-f", path, "-n", namespace}
	}

	if len(pruneWhitelist) > 0 {
		args = append(args, "--prune")
		args = append(args, "--all")

		for _, w := range pruneWhitelist {
			args = append(args, "--prune-whitelist="+w)
		}
	}

	if c.Server != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", kubeconfigFilePath))
	}

	kubectlCmd := exec.Command(args[0], args[1:]...)

	var cmdStr string
	if kustomize {
		cmdStr = "kustomize build " + path + " | " + strings.Join(args, " ")
		kustomizeCmd := exec.Command("kustomize", "build", path)
		pipe, err := kustomizeCmd.StdoutPipe()
		if err != nil {
			return cmdStr, "", err
		}
		kubectlCmd.Stdin = pipe

		err = kustomizeCmd.Start()
		if err != nil {
			fmt.Printf("%s", err)
			return cmdStr, "", err
		}
	} else {
		cmdStr = strings.Join(args, " ")
	}

	out, err := kubectlCmd.CombinedOutput()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			c.Metrics.UpdateKubectlExitCodeCount(namespace, e.ExitCode())
		}
		return cmdStr, string(out), err
	}
	c.Metrics.UpdateKubectlExitCodeCount(path, 0)

	return cmdStr, string(out), err
}

// NamespaceAnnotations returns string values of kube-applier annotaions
func (c *Client) NamespaceAnnotations(namespace string) (KAAnnotations, error) {
	kaa := KAAnnotations{}
	args := []string{"kubectl", "get", "namespace", namespace, "-o", "json"}
	if c.Server != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", kubeconfigFilePath))
	}
	stdout, err := execCommand(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			c.Metrics.UpdateKubectlExitCodeCount(namespace, e.ExitCode())
		}
		return kaa, err
	}
	c.Metrics.UpdateKubectlExitCodeCount(namespace, 0)

	var nr struct {
		Metadata struct {
			Annotations map[string]string
		}
	}
	if err := json.Unmarshal(stdout, &nr); err != nil {
		return kaa, err
	}

	kaa.Enabled = nr.Metadata.Annotations[enabledAnnotation]
	kaa.DryRun = nr.Metadata.Annotations[dryRunAnnotation]
	kaa.PruneWhitelist = nr.Metadata.Annotations[pruneWhitelistAnnotation]

	return kaa, nil
}
