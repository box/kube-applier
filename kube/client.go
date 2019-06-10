package kube

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

const (
	// Default location of the service-account token on the cluster
	tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// Location of the kubeconfig template file within the container - see ADD command in Dockerfile
	kubeconfigTemplatePath = "/templates/kubeconfig"

	// Location of the kubeconfig template for temporary templates - see ADD command in Dockerfile
	tempkubeConfigTemplatePath = "/templates/tempKubeConfig"

	// Location of the written kubeconfig file within the container
	kubeconfigFilePath = "/etc/kubeconfig"
)

// To make testing possible
var execCommand = exec.Command

//todo(catalin-ilea) Add core/v1/Secret when we plug in strongbox
var pruneWhitelist = []string{
	"apps/v1/DaemonSet",
	"apps/v1/Deployment",
	"apps/v1/StatefulSet",
	"autoscaling/v1/HorizontalPodAutoscaler",
	"batch/v1/Job",
	"core/v1/ConfigMap",
	"core/v1/Pod",
	"core/v1/Service",
	"core/v1/ServiceAccount",
	"networking.k8s.io/v1beta1/Ingress",
	"networking.k8s.io/v1/NetworkPolicy",
}

// AutomaticDeploymentOption type used for labels
type AutomaticDeploymentOption string

// Automatic Deployment labels
const (
	DryRun AutomaticDeploymentOption = "dry-run"
	On     AutomaticDeploymentOption = "on"
	Off    AutomaticDeploymentOption = "off"
)

// ClientInterface allows for mocking out the functionality of Client when testing the full process of an apply run.
type ClientInterface interface {
	Apply(path, namespace, serviceAccount string, dryRun, prune, delegate, kustomize bool) (string, string, error)
	GetNamespaceStatus(namespace string) (AutomaticDeploymentOption, error)
	GetNamespaceUserSecretName(namespace, username string) (string, error)
	GetUserDataFromSecret(namespace, secret string) (string, string, error)
	SAToken(namespace, serviceAccount string) (string, error)
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
// delegate - attempt to "kubectl apply" the files located at path using a
//          delegate service account under the given namespace.
//          The service account must exist for the given namespace and
//          must contain a secret that include token and ca.cert.  It returns the
//          full apply command and its output.
//
// kustomize - Do a `kubectl apply -k` on the path, set to if there is a
//             `kustomization.yaml` found in the path
func (c *Client) Apply(path, namespace, serviceAccount string, dryRun, prune, delegate, kustomize bool) (string, string, error) {
	var args []string

	if kustomize {
		args = []string{"kubectl", "apply", fmt.Sprintf("--server-dry-run=%t", dryRun), "-k", path, fmt.Sprintf("-l %s!=%s", c.Label, Off), "-n", namespace}
	} else {
		args = []string{"kubectl", "apply", fmt.Sprintf("--server-dry-run=%t", dryRun), "-R", "-f", path, fmt.Sprintf("-l %s!=%s", c.Label, Off), "-n", namespace}
	}

	if prune {
		args = append(args, "--prune")
		for _, w := range pruneWhitelist {
			args = append(args, "--prune-whitelist="+w)
		}
	}

	if delegate {
		token, err := c.SAToken(namespace, serviceAccount)
		if err != nil {
			return "", "", fmt.Errorf("error getting token for serviceaccount: %v", err)
		}
		args = append(args, fmt.Sprintf("--token=%s", token))
	} else if c.Server != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", kubeconfigFilePath))
	}

	kubectlCmd := exec.Command(args[0], args[1:]...)

	cmdStr := sanitiseCmdStr(strings.Join(args, " "))

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

// GetNamespaceStatus returns the AutmaticDeployment label for the given namespace
func (c *Client) GetNamespaceStatus(namespace string) (AutomaticDeploymentOption, error) {
	args := []string{"kubectl", "get", "namespace", namespace, "-o", "json", "-n", namespace}
	if c.Server != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", kubeconfigFilePath))
	}
	stdout, err := execCommand(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			c.Metrics.UpdateKubectlExitCodeCount(namespace, e.ExitCode())
		}
		return Off, err
	}
	c.Metrics.UpdateKubectlExitCodeCount(namespace, 0)

	var nr struct {
		Metadata struct {
			Labels map[string]string
		}
	}
	if err := json.Unmarshal(stdout, &nr); err != nil {
		return Off, err
	}

	return AutomaticDeploymentOption(nr.Metadata.Labels[c.Label]), nil
}

// GetNamespaceUserSecretName returns the first secret name found for the given user
func (c *Client) GetNamespaceUserSecretName(namespace, username string) (string, error) {
	args := []string{"kubectl", "get", "serviceaccount", username, "-o", "json", "-n", namespace}
	if c.Server != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", kubeconfigFilePath))
	}
	stdout, err := execCommand(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			c.Metrics.UpdateKubectlExitCodeCount(namespace, e.ExitCode())
		}
		return "", errors.Errorf("error while getting SA %s:%s %v", namespace, username, err)
	}

	c.Metrics.UpdateKubectlExitCodeCount(namespace, 0)

	type secret struct {
		Name string
	}
	var nr struct {
		Secrets []secret
	}
	if err := json.Unmarshal(stdout, &nr); err != nil {
		return "", err
	}

	if len(nr.Secrets) > 1 {
		log.Logger.Info("Found many secrets for kube-applier using the first on the list", "namespace", namespace)
	}
	if len(nr.Secrets) == 0 {
		return "", errors.Errorf("no secrets found for %s user", username)
	}
	return nr.Secrets[0].Name, nil
}

// GetUserDataFromSecret returns the token and the ca.crt path
func (c *Client) GetUserDataFromSecret(namespace, secret string) (string, string, error) {
	args := []string{"kubectl", "get", "secret", secret, "-o", "json", "-n", namespace}
	if c.Server != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", kubeconfigFilePath))
	}
	stdout, err := execCommand(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			c.Metrics.UpdateKubectlExitCodeCount(namespace, e.ExitCode())
		}
		return "", "", err
	}
	c.Metrics.UpdateKubectlExitCodeCount(namespace, 0)

	var nr struct {
		Data map[string]string
	}
	if err := json.Unmarshal(stdout, &nr); err != nil {
		return "", "", err
	}

	token, ok := nr.Data["token"]
	if !ok {
		return "", "", errors.Errorf("secret %s missing token", secret)
	}
	cert, ok := nr.Data["ca.crt"]
	if !ok {
		return "", "", errors.Errorf("secret %s missing ca.crt", secret)
	}
	return token, cert, nil
}

// SAToken: Returns the base64 decoded token string for the given ns/sa
func (c *Client) SAToken(namespace, serviceAccount string) (string, error) {

	secretName, err := c.GetNamespaceUserSecretName(namespace, serviceAccount)
	if err != nil {
		return "", errors.Errorf("error getting secret name for %s : %v", serviceAccount, err)
	}
	encToken, _, err := c.GetUserDataFromSecret(namespace, secretName)
	if err != nil {
		return "", errors.Errorf("error getting data for secret %s : %v", secretName, err)
	}

	// Get token and write the temp kubeconfig
	token, err := base64.StdEncoding.DecodeString(encToken)
	if err != nil {
		return "", errors.Errorf("Error while decoding token for %s : %v", serviceAccount, err)
	}

	return string(token), nil
}

func sanitiseCmdStr(cmdStr string) string {
	// Omit token string if included in the ccd output
	r := regexp.MustCompile(`--token=[\S]+`)
	return r.ReplaceAllString(cmdStr, "--token=<omitted>")
}
