package kube

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

type testCase struct {
	clientMajor string
	clientMinor string
	serverMajor string
	serverMinor string
	expected    error
}

func TestClientIsCompatible(t *testing.T) {

	// Bad clientMinor string
	tc := testCase{"1", "abcd", "1", "0", errors.Errorf("error checking kubectl version: unable to parse client minor release from string \"abcd\"")}
	createAndAssert(t, tc)

	// Bad serverMinor string
	tc = testCase{"1", "0", "1", "defg", errors.Errorf("error checking kubectl version: unable to parse server minor release from string \"defg\"")}
	createAndAssert(t, tc)

	// Client 1.0, Server 1.0
	tc = testCase{"1", "0", "1", "0", nil}
	createAndAssert(t, tc)

	// Client 1.0, Server 1.1
	tc = testCase{"1", "0", "1", "1", nil}
	createAndAssert(t, tc)

	// Client 1.0, Server 1.2
	tc = testCase{"1", "0", "1", "2", errors.New("kubectl client and server versions are incompatible. Client is 1.0; server is 1.2. Client must be same minor release as server or one minor release behind server")}
	createAndAssert(t, tc)

	// Client 1.1, Server 1.0
	tc = testCase{"1", "1", "1", "0", errors.New("kubectl client and server versions are incompatible. Client is 1.1; server is 1.0. Client must be same minor release as server or one minor release behind server")}
	createAndAssert(t, tc)

	// Client 1.1, Server 1.1+
	tc = testCase{"1", "1", "1", "1+", nil}
	createAndAssert(t, tc)

	// Client 1.1+, Server 1.1
	tc = testCase{"1", "1+", "1", "1", nil}
	createAndAssert(t, tc)

	// Client 1.1, Server 1.2+
	tc = testCase{"1", "1", "1", "2+", nil}
	createAndAssert(t, tc)

	// Client 1.2, Server 1.1+
	tc = testCase{"1", "2", "1", "1+", errors.New("kubectl client and server versions are incompatible. Client is 1.2; server is 1.1+. Client must be same minor release as server or one minor release behind server")}
	createAndAssert(t, tc)
}

func createAndAssert(t *testing.T, tc testCase) {
	assert := assert.New(t)
	err := isCompatible(tc.clientMajor, tc.clientMinor, tc.serverMajor, tc.serverMinor)
	if err != nil && tc.expected != nil {
		// errors now will have a different stack of callers so just compare the messages
		assert.Equal(tc.expected.Error(), err.Error())
	} else {
		assert.Equal(tc.expected, err)
	}
}

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

	testClient := &Client{}
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

	testClient := &Client{}
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
