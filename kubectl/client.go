package kubectl

import (
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"
	"strings"

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

	var cmdStr string
	var kustomizeStderr io.ReadCloser
	if kustomize {
		cmdStr = "kustomize build " + path + " | " + strings.Join(args, " ")
		kustomizeCmd := exec.Command("kustomize", "build", path)
		kustomizeStdout, err := kustomizeCmd.StdoutPipe()
		if err != nil {
			return cmdStr, "", err
		}
		kubectlCmd.Stdin = kustomizeStdout
		kustomizeStderr, err = kustomizeCmd.StderrPipe()
		if err != nil {
			return cmdStr, "", err
		}

		err = kustomizeCmd.Start()
		if err != nil {
			fmt.Printf("%s", err)
			return cmdStr, "", err
		}
		defer func() {
			io.Copy(ioutil.Discard, kustomizeStdout)
			io.Copy(ioutil.Discard, kustomizeStderr)
			err = kustomizeCmd.Wait()
			if err != nil {
				fmt.Printf("%s", err)
			}
		}()
	} else {
		cmdStr = strings.Join(args, " ")
	}

	out, err := kubectlCmd.CombinedOutput()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			c.Metrics.UpdateKubectlExitCodeCount(namespace, e.ExitCode())
		}
		var str strings.Builder
		if kustomizeStderr != nil {
			kustomizeErr, _ := ioutil.ReadAll(kustomizeStderr)
			str.WriteString(string(kustomizeErr))
		}
		str.WriteString(string(out))
		return cmdStr, str.String(), err
	}
	c.Metrics.UpdateKubectlExitCodeCount(path, 0)

	return cmdStr, string(out), err
}
