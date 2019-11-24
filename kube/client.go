package kube

import (
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/box/kube-applier/sysutil"
)

const (
	// Default location of the service-account token on the cluster
	tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// Location of the kubeconfig template file within the container - see ADD command in Dockerfile
	kubeconfigTemplatePath = "/templates/kubeconfig"
)

// ClientInterface allows for mocking out the functionality of Client when testing the full process of an apply run.
//type ClientInterface interface {
//	Apply(string) (cmd, output string, err error)
//	CheckVersion() error
//}

// ClientInterface allows for mocking out the functionality of Client when testing the full process of an apply run.
type ClientInterface interface {
	Apply(string) (cmd, output string, err error)
	List(string) (cmd, output string, err error)
	Remove(string, string) (cmd, output string, err error)
	CheckVersion() error
}

// Client enables communication with the Kubernetes API Server through kubectl commands.
// The Server field enables discovery of the API server when kube-proxy is not configured (see README.md for more information).
type Client struct {
	Server string
	// Location of the written kubeconfig file within the container
	kubeconfigFilePath string
	// if <0, no verbosity level is specified in the commands run
	LogLevel int
}

// Configure writes the kubeconfig file to be used for authenticating kubectl commands.
func (c *Client) Configure() error {
	// No need to write a kubeconfig file if Server is not specified (API server will be discovered via kube-proxy).
	if c.Server == "" {
		return nil
	}

	f, err := ioutil.TempFile("", "kubeConfig")
	c.kubeconfigFilePath = f.Name()
	log.Printf("Using kubeConfig file: %s", c.kubeconfigFilePath)

	if err != nil {
		return fmt.Errorf("Error creating kubeconfig file: %v", err)
	}
	defer f.Close()

	token, err := ioutil.ReadFile(tokenPath)
	if err != nil {
		return fmt.Errorf("Error accessing token for kubeconfig file: %v", err)
	}

	var data struct {
		Token  string
		Server string
	}
	data.Token = string(token)
	data.Server = c.Server

	template, err := sysutil.CreateTemplate(kubeconfigTemplatePath)
	if err != nil {
		return fmt.Errorf("Error parsing kubeconfig template: %v", err)
	}
	if err := template.Execute(f, data); err != nil {
		return fmt.Errorf("Error applying kubeconfig template: %v", err)
	}

	return nil
}

// CheckVersion returns an error if the server and client have incompatible versions, otherwise returns nil.
func (c *Client) CheckVersion() error {
	args := []string{"kubectl", "version"}
	if c.LogLevel > -1 {
		args = append(args, fmt.Sprintf("-v=%d", c.LogLevel))
	}
	if c.Server != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", c.kubeconfigFilePath))
	}
	stdout, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	output := strings.TrimSuffix(string(stdout), "\n")
	if err != nil {
		return fmt.Errorf("Error checking kubectl version: %v", output)
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
	incompatible := fmt.Errorf("Error: kubectl client and server versions are incompatible. Client is %s.%s; server is %s.%s. Client must be same minor release as server or one minor release behind server.", clientMajor, clientMinor, serverMajor, serverMinor)

	if strings.Replace(clientMajor, "+", "", -1) != strings.Replace(serverMajor, "+", "", -1) {
		return incompatible
	}
	clientMinorInt, err := strconv.Atoi(strings.Replace(clientMinor, "+", "", -1))
	if err != nil {
		return fmt.Errorf("Error checking kubectl version: unable to parse client minor release from string \"%v\"", clientMinor)
	}
	serverMinorInt, err := strconv.Atoi(strings.Replace(serverMinor, "+", "", -1))
	if err != nil {
		return fmt.Errorf("Error checking kubectl version: unable to parse server minor release from string \"%v\"", serverMinor)
	}

	minorDiff := serverMinorInt - clientMinorInt
	if minorDiff != 0 && minorDiff != 1 {
		return incompatible
	}
	return nil
}

// Apply attempts to "kubectl apply" the file located at path.
// It returns the full apply command and its output.
func (c *Client) Apply(path string) (cmd, output string, err error) {
	args := []string{"kubectl", "apply", "-f", path}
	if c.LogLevel > -1 {
		args = append(args, fmt.Sprintf("-v=%d", c.LogLevel))
	}
	if c.Server != "" {
		args = append(args, fmt.Sprintf("--kubeconfig=%s", c.kubeconfigFilePath))
	}
	cmd = strings.Join(args, " ")
	stdout, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("Error: %v", err)
	}
	return cmd, string(stdout), err
}

// List attempts to "kubectl get" the specified resouceType and return a list of resource names.
func (c *Client) List(resourceType string) (cmd, output string, err error) {
    args := []string{"kubectl", "get", "--no-headers", "-o=custom-columns=NAME:.metadata.name", resourceType}
    if c.LogLevel > -1 {
        args = append(args, fmt.Sprintf("-v=%d", c.LogLevel))
    }
    if c.Server != "" {
        args = append(args, fmt.Sprintf("--kubeconfig=%s", c.kubeconfigFilePath))
    }
    cmd = strings.Join(args, " ")
    stdout, err := exec.Command(args[0], args[1:]...).CombinedOutput()
    if err != nil {
        err = fmt.Errorf("Error: %v", err)
    }
    return cmd, string(stdout), err
}

// Remove attempts to "kubectl delete" a specified resource
// It returns the full delete command and its output.
func (c *Client) Remove(resourceType string, resourceName string) (cmd, output string, err error) {
    args := []string{"kubectl", "delete", resourceType, resourceName}
    if c.LogLevel > -1 {
        args = append(args, fmt.Sprintf("-v=%d", c.LogLevel))
    }
    if c.Server != "" {
        args = append(args, fmt.Sprintf("--kubeconfig=%s", c.kubeconfigFilePath))
    }
    cmd = strings.Join(args, " ")
    stdout, err := exec.Command(args[0], args[1:]...).CombinedOutput()
    if err != nil {
        err = fmt.Errorf("Error: %v", err)
    }
    return cmd, string(stdout), err
}
