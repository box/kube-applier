package metrics

import (
	"testing"

	"github.com/go-test/deep"
)

func TestParseKubectlOutput(t *testing.T) {
	output := `namespace/namespaceName configured
limitrange/limit-range configured
role.rbac.authorization.k8s.io/auth unchanged
rolebinding.rbac.authorization.k8s.io/rolebinding unchanged
serviceaccount/account unchanged
networkpolicy.networking.k8s.io/default unchanged
service/serviceName unchanged
deployment.apps/deploymentName unchanged`

	want := []Result{
		{"namespace", "namespaceName", "configured"},
		{"limitrange", "limit-range", "configured"},
		{"role.rbac.authorization.k8s.io", "auth", "unchanged"},
		{"rolebinding.rbac.authorization.k8s.io", "rolebinding", "unchanged"},
		{"serviceaccount", "account", "unchanged"},
		{"networkpolicy.networking.k8s.io", "default", "unchanged"},
		{"service", "serviceName", "unchanged"},
		{"deployment.apps", "deploymentName", "unchanged"},
	}

	got := parseKubectlOutput(output)

	if diff := deep.Equal(got, want); diff != nil {
		t.Error(diff)
	}
}

func TestParseKubectlServerDryRunOutput(t *testing.T) {
	output := `namespace/namespaceName configured (server dry run)
limitrange/limit-range configured (server dry run)
role.rbac.authorization.k8s.io/auth configured (server dry run)
rolebinding.rbac.authorization.k8s.io/rolebinding configured (server dry run)
serviceaccount/account configured (server dry run)
networkpolicy.networking.k8s.io/default configured (server dry run)
service/serviceName configured (server dry run)
deployment.apps/deploymentName configured (server dry run)`

	want := []Result{
		{"namespace", "namespaceName", "configured"},
		{"limitrange", "limit-range", "configured"},
		{"role.rbac.authorization.k8s.io", "auth", "configured"},
		{"rolebinding.rbac.authorization.k8s.io", "rolebinding", "configured"},
		{"serviceaccount", "account", "configured"},
		{"networkpolicy.networking.k8s.io", "default", "configured"},
		{"service", "serviceName", "configured"},
		{"deployment.apps", "deploymentName", "configured"},
	}

	got := parseKubectlOutput(output)

	if diff := deep.Equal(got, want); diff != nil {
		t.Error(diff)
	}
}
