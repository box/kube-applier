package kube

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/utilitywarehouse/kube-applier/metrics"
)

var testServiceAccount = false
var testSecret = false

func MockCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	if testServiceAccount {
		cmd.Env = []string{"GO_WANT_SERVICE_ACCOUNT=1"}
	}
	if testSecret {
		cmd.Env = []string{"GO_WANT_SECRET=1"}
	}
	return cmd
}

var (
	kubeServiceAccount = `
{
	"other": {
		"key": "value"
	},
	"secrets": [
		{
			"name": "kube-applier-token-xm7qj"
		}
	]
}
`
	kubeSecret = `
{
	"other": "value",
	"data": {
		"namespace": "c3lzLW1vbg==",
		"ca.crt": "LS0tLS1CRUdJTiBDRVJUSUZJQ0F==",
		"token": "ZXlKaGJHY2lPaUpGVXpJMU5pSXNJblI1Y0NJNk=="
	}
}
`
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_SERVICE_ACCOUNT") == "1" {
		fmt.Fprintf(os.Stdout, kubeServiceAccount)
		os.Exit(0)
	}
	if os.Getenv("GO_WANT_SECRET") == "1" {
		fmt.Fprintf(os.Stdout, kubeSecret)
		os.Exit(0)
	}

}

func TestGetNamespaceUserSecretName(t *testing.T) {
	// Mock exec.Command
	execCommand = MockCommand
	defer func() { execCommand = exec.Command }()
	testServiceAccount = true
	defer func() { testServiceAccount = false }()

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	testClient := &Client{
		Metrics: metrics,
	}

	metrics.EXPECT().UpdateKubectlExitCodeCount("namespace", 0).Times(1)

	secret, err := testClient.GetNamespaceUserSecretName("namespace", "username")
	if err != nil {
		t.Fatal(err)
	}

	if secret != "kube-applier-token-xm7qj" {
		t.Fatal("Got unexpected secret!")
	}
}

func TestGetUserDataFromSecret(t *testing.T) {
	// Mock exec.Command
	execCommand = MockCommand
	defer func() { execCommand = exec.Command }()
	testSecret = true
	defer func() { testSecret = false }()

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	testClient := &Client{
		Metrics: metrics,
	}

	metrics.EXPECT().UpdateKubectlExitCodeCount("namespace", 0).Times(1)

	token, cert, err := testClient.GetUserDataFromSecret("namespace", "secretname")
	if err != nil {
		t.Fatal(err)
	}

	if token != "ZXlKaGJHY2lPaUpGVXpJMU5pSXNJblI1Y0NJNk==" {
		t.Fatal("Got unexpected token!")
	}
	if cert != "LS0tLS1CRUdJTiBDRVJUSUZJQ0F==" {
		t.Fatal("Got unexpected cert")
	}
}

func TestSanitiseCmdStr(t *testing.T) {
	testCmdStr := "$ kubectl apply --server-dry-run=false -R -f manifests -l automaticDeployment!=off -n namespace --token=xxxx"
	expectedStr := "$ kubectl apply --server-dry-run=false -R -f manifests -l automaticDeployment!=off -n namespace --token=<omitted>"

	assert.Equal(t, sanitiseCmdStr(testCmdStr), expectedStr, "cmd sanitisation failed")
}
