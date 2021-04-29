package kube

import (
	"fmt"
	"testing"

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
	tc := testCase{"1", "abcd", "1", "0", fmt.Errorf("Error checking kubectl version: unable to parse client minor release from string \"abcd\"")}
	createAndAssert(t, tc)

	// Bad serverMinor string
	tc = testCase{"1", "0", "1", "defg", fmt.Errorf("Error checking kubectl version: unable to parse server minor release from string \"defg\"")}
	createAndAssert(t, tc)

	// Client 1.0, Server 1.0
	tc = testCase{"1", "0", "1", "0", nil}
	createAndAssert(t, tc)

	// Client 1.0, Server 1.1
	tc = testCase{"1", "0", "1", "1", nil}
	createAndAssert(t, tc)

	// Client 1.0, Server 1.2
	tc = testCase{"1", "0", "1", "2", fmt.Errorf("Error: kubectl client and server versions are incompatible. Client is 1.0; server is 1.2. Client must be same minor release as server or one minor release behind server.")}
	createAndAssert(t, tc)

	// Client 1.1, Server 1.0
	tc = testCase{"1", "1", "1", "0", nil}
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
	tc = testCase{"1", "2", "1", "1+", nil}
	createAndAssert(t, tc)
}

func createAndAssert(t *testing.T, tc testCase) {
	assert := assert.New(t)
	err := isCompatible(tc.clientMajor, tc.clientMinor, tc.serverMajor, tc.serverMinor)
	assert.Equal(tc.expected, err)
}
