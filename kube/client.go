package kube

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/utilitywarehouse/kube-applier/log"
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
	"extensions/v1beta1/Ingress",
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
	Apply(path, namespace string, dryRun, prune, strict, kustomize bool) (string, string, error)
	CheckVersion() error
	GetNamespaceStatus(namespace string) (AutomaticDeploymentOption, error)
	GetNamespaceUserSecretName(namespace, username string) (string, error)
	GetUserDataFromSecret(namespace, secret string) (string, string, error)
	CreateTempConfig(namespace, serviceAccount string) (string, string, error)
}

// Client enables communication with the Kubernetes API Server through kubectl commands.
// The Server field enables discovery of the API server when kube-proxy is not configured (see README.md for more information).
type Client struct {
	Server string
	Label  string
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

// CheckVersion returns an error if the server and client have incompatible versions, otherwise returns nil.
func (c *Client) CheckVersion() error {
	args := []string{"kubectl", "version"}
	if c.Server != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", kubeconfigFilePath))
	}
	stdout, err := execCommand(args[0], args[1:]...).CombinedOutput()
	output := strings.TrimSuffix(string(stdout), "\n")
	if err != nil {
		return errors.Wrapf(err, "checking kubectl version failed: %v", output)
	}

	// Using regular expressions, parse for the Major and Minor version numbers for both client and server.
	clientPattern := regexp.MustCompile("(?:Client Version: version.Info{Major:\"([0-9+]+)\", Minor:\"([0-9+]+)\")")
	serverPattern := regexp.MustCompile("(?:Server Version: version.Info{Major:\"([0-9+]+)\", Minor:\"([0-9+]+)\")")

	clientInfo := clientPattern.FindAllStringSubmatch(output, -1)
	clientMajor := clientInfo[0][1]
	clientMinor := clientInfo[0][2]

	serverInfo := serverPattern.FindAllStringSubmatch(output, -1)
	serverMajor := serverInfo[0][1]
	serverMinor := serverInfo[0][2]

	return isCompatible(clientMajor, clientMinor, serverMajor, serverMinor)
}

// isCompatible compares the major and minor release numbers for the client and server, returning nil if they are compatible and an error otherwise.
func isCompatible(clientMajor, clientMinor, serverMajor, serverMinor string) error {
	incompatible := errors.Errorf("kubectl client and server versions are incompatible. Client is %s.%s; server is %s.%s. Client must be same minor release as server or one minor release behind server", clientMajor, clientMinor, serverMajor, serverMinor)

	if strings.Replace(clientMajor, "+", "", -1) != strings.Replace(serverMajor, "+", "", -1) {
		return incompatible
	}
	clientMinorInt, err := strconv.Atoi(strings.Replace(clientMinor, "+", "", -1))
	if err != nil {
		return errors.Errorf("error checking kubectl version: unable to parse client minor release from string \"%v\"", clientMinor)
	}
	serverMinorInt, err := strconv.Atoi(strings.Replace(serverMinor, "+", "", -1))
	if err != nil {
		return errors.Errorf("error checking kubectl version: unable to parse server minor release from string \"%v\"", serverMinor)
	}

	minorDiff := serverMinorInt - clientMinorInt
	if minorDiff != 0 && minorDiff != 1 {
		return incompatible
	}
	return nil
}

// Apply attempts to "kubectl apply" the files located at path. It returns the
// full apply command and its output.
//
// strict - attempt to "kubectl apply" the files located at path using a
//          `kube-applier` service account under the given namespace.
//          `kube-applier` service account must exist for the given namespace and
//          must contain a secret that include token and ca.cert.  It returns the
//          full apply command and its output.
//
// kustomize - Do a `kuztomize build` on the path before piping to `kubectl
//             apply`, set to if there is a `kustomization.yaml` found in the path
func (c *Client) Apply(path, namespace string, dryRun, prune, strict, kustomize bool) (string, string, error) {
	var args []string

	if kustomize {
		args = []string{"kubectl", "apply", fmt.Sprintf("--dry-run=%t", dryRun), "-f", "-", fmt.Sprintf("-l %s!=%s", c.Label, Off), "-n", namespace}
	} else {
		args = []string{"kubectl", "apply", fmt.Sprintf("--dry-run=%t", dryRun), "-R", "-f", path, fmt.Sprintf("-l %s!=%s", c.Label, Off), "-n", namespace}
	}

	if prune {
		args = append(args, "--prune")
		for _, w := range pruneWhitelist {
			args = append(args, "--prune-whitelist="+w)
		}
	}

	if strict {
		tempKubeConfigFilepath, tempCertFilepath, err := c.CreateTempConfig(namespace, "kube-applier")
		if err != nil {
			return "", "", fmt.Errorf("error creating temp config: %v", err)
		}
		defer func() { os.Remove(tempKubeConfigFilepath); os.Remove(tempCertFilepath) }()
		args = append(args, fmt.Sprintf("--kubeconfig=%s", tempKubeConfigFilepath))
	} else if c.Server != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", kubeconfigFilePath))
	}

	kubectlCmd := exec.Command(args[0], args[1:]...)

	var out []byte
	var err error
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
			return cmdStr, "", err
		}
	} else {
		cmdStr = strings.Join(args, " ")
	}

	out, err = kubectlCmd.CombinedOutput()
	if err != nil {
		return cmdStr, string(out), err
	}

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
		return Off, err
	}

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
		return "", err
	}

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
		return "", "", err
	}

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

// CreateTempConfig generates a config file for a serviceAccount under a namespace and the respective ca.cert
// Caution: files should be deleted by caller later when not needed any more!!
func (c *Client) CreateTempConfig(namespace, serviceAccount string) (string, string, error) {
	f, err := ioutil.TempFile("", "tempKubeConfig")
	if err != nil {
		return "", "", errors.Wrap(err, "creating temp kubeconfig file failed")
	}
	defer f.Close()

	secretName, err := c.GetNamespaceUserSecretName(namespace, serviceAccount)
	if err != nil {
		return "", "", errors.Errorf("error getting secret name for %s : %v", serviceAccount, err)
	}
	encToken, encCert, err := c.GetUserDataFromSecret(namespace, secretName)
	if err != nil {
		return "", "", errors.Errorf("error getting data for secret %s : %v", secretName, err)
	}

	// Create certificate
	cert, err := base64.StdEncoding.DecodeString(encCert)
	if err != nil {
		return "", "", errors.Errorf("error while decoding ca.cert for %s : %v", serviceAccount, err)
	}
	certFile, err := ioutil.TempFile("", "temp-ca.crt")
	if err != nil {
		return "", "", errors.Wrap(err, "creating temp cert file failed")
	}
	defer certFile.Close()
	if _, err := certFile.Write(cert); err != nil {
		return "", "", errors.Wrap(err, "writing certificate to file failed")
	}

	// Get token and write the temp kubeconfig
	token, err := base64.StdEncoding.DecodeString(encToken)
	if err != nil {
		return "", "", errors.Errorf("Error while decoding token for %s : %v", serviceAccount, err)
	}

	var data struct {
		Cert   string
		Token  string
		Server string
		User   string
	}
	data.Cert = certFile.Name()
	data.Token = string(token)
	// The default empty "" server string will point to the running cluster's kube API
	data.Server = c.Server
	data.User = serviceAccount

	template, err := sysutil.CreateTemplate(tempkubeConfigTemplatePath)
	if err != nil {
		return "", "", errors.Wrap(err, "parsing kubeconfig template failed")
	}
	if err := template.Execute(f, data); err != nil {
		return "", "", errors.Wrap(err, "applying kubeconfig template failed")
	}
	return f.Name(), certFile.Name(), nil
}
