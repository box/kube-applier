package metrics

import (
	"testing"

	"github.com/go-test/deep"
)

func TestParseKubectlOutput(t *testing.T) {
	output := `namespace "namespaceName" configured
limitrange "limit-range" configured
role.rbac.authorization.k8s.io "auth" unchanged
rolebinding.rbac.authorization.k8s.io "rolebinding" unchanged
serviceaccount "account" unchanged
networkpolicy.networking.k8s.io "default" unchanged
service "serviceName" unchanged
deployment.apps "deploymentName" unchanged`

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

func TestParseKubectlDryRunOutput(t *testing.T) {
	output := `namespace "namespaceName" configured (dry run)
limitrange "limit-range" configured (dry run)
role.rbac.authorization.k8s.io "auth" configured (dry run)
rolebinding.rbac.authorization.k8s.io "rolebinding" configured (dry run)
serviceaccount "account" configured (dry run)
networkpolicy.networking.k8s.io "default" configured (dry run)
service "serviceName" configured (dry run)
deployment.apps "deploymentName" configured (dry run)`

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
