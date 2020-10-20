package metrics

import (
	"os"
	"testing"

	"github.com/go-test/deep"
	"github.com/utilitywarehouse/kube-applier/log"
)

func TestMain(m *testing.M) {
	log.InitLogger("info")
	os.Exit(m.Run())
}

func TestParseKubectlOutput(t *testing.T) {
	output := `namespace/namespaceName configured
limitrange/limit-range configured
role.rbac.authorization.k8s.io/auth unchanged
rolebinding.rbac.authorization.k8s.io/rolebinding unchanged
serviceaccount/account unchanged
networkpolicy.networking.k8s.io/default unchanged
clusterrole.rbac.authorization.k8s.io/system:metrics-server unchanged
service/serviceName unchanged
deployment.apps/deploymentName unchanged
`

	want := []Result{
		{"namespace", "namespaceName", "configured"},
		{"limitrange", "limit-range", "configured"},
		{"role.rbac.authorization.k8s.io", "auth", "unchanged"},
		{"rolebinding.rbac.authorization.k8s.io", "rolebinding", "unchanged"},
		{"serviceaccount", "account", "unchanged"},
		{"networkpolicy.networking.k8s.io", "default", "unchanged"},
		{"clusterrole.rbac.authorization.k8s.io", "system:metrics-server", "unchanged"},
		{"service", "serviceName", "unchanged"},
		{"deployment.apps", "deploymentName", "unchanged"},
	}

	got := parseKubectlOutput(output)

	if diff := deep.Equal(got, want); diff != nil {
		t.Error(diff)
	}
}

func TestParseKubectlOutputServerDryRun(t *testing.T) {
	output := `namespace/namespaceName configured (server dry run)
limitrange/limit-range configured (server dry run)
role.rbac.authorization.k8s.io/auth configured (server dry run)
rolebinding.rbac.authorization.k8s.io/rolebinding configured (server dry run)
serviceaccount/account configured (server dry run)
networkpolicy.networking.k8s.io/default configured (server dry run)
clusterrole.rbac.authorization.k8s.io/system:metrics-server unchanged (server dry run)
service/serviceName configured (server dry run)
deployment.apps/deploymentName configured (server dry run)
`

	want := []Result{
		{"namespace", "namespaceName", "configured"},
		{"limitrange", "limit-range", "configured"},
		{"role.rbac.authorization.k8s.io", "auth", "configured"},
		{"rolebinding.rbac.authorization.k8s.io", "rolebinding", "configured"},
		{"serviceaccount", "account", "configured"},
		{"networkpolicy.networking.k8s.io", "default", "configured"},
		{"clusterrole.rbac.authorization.k8s.io", "system:metrics-server", "unchanged"},
		{"service", "serviceName", "configured"},
		{"deployment.apps", "deploymentName", "configured"},
	}

	got := parseKubectlOutput(output)

	if diff := deep.Equal(got, want); diff != nil {
		t.Error(diff)
	}
}

func TestParseKubectlOutputWarningLine(t *testing.T) {
	output := `namespace/sys-auth configured (server dry run)
rolebinding.rbac.authorization.k8s.io/vault-configmap-applier unchanged (server dry run)
Warning: kubectl apply should be used on resource created by either kubectl create --save-config or kubectl apply
configmap/vault-tls configured (server dry run)
clusterrole.rbac.authorization.k8s.io/system:metrics-server unchanged (server dry run)
secret/k8s-auth-conf unchanged (server dry run)
`

	want := []Result{
		{"namespace", "sys-auth", "configured"},
		{"rolebinding.rbac.authorization.k8s.io", "vault-configmap-applier", "unchanged"},
		{"configmap", "vault-tls", "configured"},
		{"clusterrole.rbac.authorization.k8s.io", "system:metrics-server", "unchanged"},
		{"secret", "k8s-auth-conf", "unchanged"},
	}

	got := parseKubectlOutput(output)

	if diff := deep.Equal(got, want); diff != nil {
		t.Error(diff)
	}
}

func TestParseKubectlServerSideApply(t *testing.T) {
	output := `namespace/namespaceName serverside-applied
limitrange/limit-range serverside-applied
role.rbac.authorization.k8s.io/auth serverside-applied
rolebinding.rbac.authorization.k8s.io/rolebinding serverside-applied
serviceaccount/account serverside-applied
networkpolicy.networking.k8s.io/default serverside-applied
clusterrole.rbac.authorization.k8s.io/system:metrics-server serverside-applied
service/serviceName serverside-applied
deployment.apps/deploymentName serverside-applied
`

	want := []Result{
		{"namespace", "namespaceName", "serverside-applied"},
		{"limitrange", "limit-range", "serverside-applied"},
		{"role.rbac.authorization.k8s.io", "auth", "serverside-applied"},
		{"rolebinding.rbac.authorization.k8s.io", "rolebinding", "serverside-applied"},
		{"serviceaccount", "account", "serverside-applied"},
		{"networkpolicy.networking.k8s.io", "default", "serverside-applied"},
		{"clusterrole.rbac.authorization.k8s.io", "system:metrics-server", "serverside-applied"},
		{"service", "serviceName", "serverside-applied"},
		{"deployment.apps", "deploymentName", "serverside-applied"},
	}

	got := parseKubectlOutput(output)

	if diff := deep.Equal(got, want); diff != nil {
		t.Error(diff)
	}
}
