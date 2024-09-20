package kube

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testCase struct {
	kubectlStdout []byte
	expected      error
}

func stdoutGenerator(clientMajor, clientMinor, serverMajor, serverMinor string) []byte {
	return []byte(fmt.Sprintf(`{
	"clientVersion": {
	  "major": "%s",
	  "minor": "%s",
	  "gitVersion": "v1.27.16",
	  "gitCommit": "cbb86e0d7f4a049666fac0551e8b02ef3d6c3d9a",
	  "gitTreeState": "clean",
	  "buildDate": "2024-07-17T01:53:56Z",
	  "goVersion": "go1.22.5",
	  "compiler": "gc",
	  "platform": "linux/amd64"
	},
	"kustomizeVersion": "v5.0.1",
	"serverVersion": {
	  "major": "%s",
	  "minor": "%s",
	  "gitVersion": "v1.27.11-gke.1062001",
	  "gitCommit": "2cefeadcb4ec7d21d775d15012b02d3393a53548",
	  "gitTreeState": "clean",
	  "buildDate": "2024-04-16T20:17:53Z",
	  "goVersion": "go1.21.7 X:boringcrypto",
	  "compiler": "gc",
	  "platform": "linux/amd64"
	}
  }`, clientMajor, clientMinor, serverMajor, serverMinor))
}

func TestClientIsCompatible(t *testing.T) {

	// No server version to parse properly
	malformedOut := []byte(`{
		"clientVersion": {
			"major": "1",
			"minor": "20",
			"gitVersion": "v1.20.14",
			"gitCommit": "57a3aa3f1369xcf3db9c52d228c18db94fa81876",
			"gitTreeState": "clean",
			"buildDate": "2021-12-15T14:52:33Z",
			"goVersion": "go1.15.15",
			"compiler": "gc",
			"platform": "darwin/amd64"
		}
	}
	The connection to the server localhost:8080 was refused - did you specify the right host or port?`)
	tc := testCase{
		kubectlStdout: malformedOut,
		expected:      fmt.Errorf("Error: kube versions output is not of valid format\n%s\n", malformedOut),
	}
	createAndAssert(t, tc)

	// Bad json
	malformedOut = []byte(`lorem ipsum`)
	tc = testCase{
		kubectlStdout: malformedOut,
		expected:      fmt.Errorf("Error: kube versions output is not of valid format\n%s\n", malformedOut),
	}
	createAndAssert(t, tc)

	// Bad clientMinor string
	tc = testCase{stdoutGenerator("1", "abcd", "1", "0"), fmt.Errorf("Error checking kubectl version: unable to parse client minor release from string \"abcd\"")}
	createAndAssert(t, tc)

	// Bad serverMinor string
	tc = testCase{stdoutGenerator("1", "0", "1", "defg"), fmt.Errorf("Error checking kubectl version: unable to parse server minor release from string \"defg\"")}
	createAndAssert(t, tc)

	// Client 1.0, Server 1.0
	tc = testCase{stdoutGenerator("1", "0", "1", "0"), nil}
	createAndAssert(t, tc)

	// Client 1.0, Server 1.1
	tc = testCase{stdoutGenerator("1", "0", "1", "1"), nil}
	createAndAssert(t, tc)

	// Client 1.0, Server 1.2
	tc = testCase{stdoutGenerator("1", "0", "1", "2"), fmt.Errorf("Error: kubectl client and server versions are incompatible. Client is 1.0; server is 1.2. Client must be same minor release as server or one minor release behind server")}
	createAndAssert(t, tc)

	// Client 1.2, Server 1.0
	tc = testCase{stdoutGenerator("1", "2", "1", "0"), fmt.Errorf("Error: kubectl client and server versions are incompatible. Client is 1.2; server is 1.0. Client must be same minor release as server or one minor release behind server")}
	createAndAssert(t, tc)

	// Client 1.1, Server 1.0
	tc = testCase{stdoutGenerator("1", "1", "1", "0"), nil}
	createAndAssert(t, tc)

	// Client 1.1, Server 1.1+
	tc = testCase{stdoutGenerator("1", "1", "1", "1+"), nil}
	createAndAssert(t, tc)

	// Client 1.1+, Server 1.1
	tc = testCase{stdoutGenerator("1", "1+", "1", "1"), nil}
	createAndAssert(t, tc)

	// Client 1.1, Server 1.2+
	tc = testCase{stdoutGenerator("1", "1", "1", "2+"), nil}
	createAndAssert(t, tc)

	// Client 1.2, Server 1.1+
	tc = testCase{stdoutGenerator("1", "2", "1", "1+"), nil}
	createAndAssert(t, tc)
}

func createAndAssert(t *testing.T, tc testCase) {
	assert := assert.New(t)
	err := isCompatible(tc.kubectlStdout)
	assert.Equal(tc.expected, err)
}
