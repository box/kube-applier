package kubectl

import (
	"bytes"
	"fmt"
	"os/exec"

	"github.com/utilitywarehouse/kube-applier/metrics"
)

// To make testing possible
var execCommand = exec.Command

// ClientInterface allows for mocking out the functionality of Client when testing the full process of an apply run.
type ClientInterface interface {
	Apply(path, namespace string, dryRun, kustomize bool, pruneWhitelist []string) (string, string, error)
}

// Client enables communication with the Kubernetes API Server through kubectl commands.
type Client struct {
	Label   string
	Metrics metrics.PrometheusInterface
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

	kubectlCmd := exec.Command(args[0], args[1:]...)
	cmdStr := kubectlCmd.String()

	if kustomize {
		var kustomizeStdout, kustomizeStderr bytes.Buffer

		kustomizeCmd := exec.Command("kustomize", "build", path)
		kustomizeCmd.Stdout = &kustomizeStdout
		kustomizeCmd.Stderr = &kustomizeStderr

		err := kustomizeCmd.Run()
		if err != nil {
			return kustomizeCmd.String(), kustomizeStderr.String(), err
		}

		kubectlCmd.Stdin = &kustomizeStdout
		cmdStr = kustomizeCmd.String() + " | " + cmdStr
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
