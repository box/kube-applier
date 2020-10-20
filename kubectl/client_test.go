package kubectl

import (
	"testing"

	"github.com/go-test/deep"
)

func TestFilterErrOutput(t *testing.T) {
	testCases := []struct {
		output   string
		filtered bool
	}{
		{
			output:   `Error from server (BadRequest): error when creating "secrets/test.yaml": Secret in version "v1" cannot be handled as a Secret: v1.Secret.ObjectMeta: v1.ObjectMeta.TypeMeta: Kind: Data: decode base64: illegal base64 data at input byte 4, error found in #10 byte of ...|:"invalid"},"kind":"|..., bigger context ...|{"apiVersion":"v1","data":{"something":"invalid"},"kind":"Secret","metadata":{"annotations":{"kube|...`,
			filtered: true,
		},
		{
			output:   `The request is invalid: patch: Invalid value: "map[data:map[something:invalid] metadata:map[annotations:map[kubectl.kubernetes.io/last-applied-configuration:{\"apiVersion\":\"v1\",\"data\":{\"something\":\"invalid\"},\"kind\":\"Secret\",\"metadata\":{\"annotations\":{},\"name\":\"test\",\"namespace\":\"sys-auth\"},\"type\":\"Opaque\"}\n]]]": error decoding from json: illegal base64 data at input byte 4`,
			filtered: true,
		},
		{
			output:   `The request is invalid: patch: Invalid value: "map[data:map[something:map[]] metadata:map[annotations:map[kubectl.kubernetes.io/last-applied-configuration:{\"apiVersion\":\"v1\",\"data\":{\"something\":{}},\"kind\":\"Secret\",\"metadata\":{\"annotations\":{},\"name\":\"test\",\"namespace\":\"sys-auth\"},\"type\":\"Opaque\"}\n]]]": cannot restore slice from map`,
			filtered: true,
		},
		{
			output:   `Error from server (BadRequest): error when creating "secrets/test.yaml": Secret in version "v1" cannot be handled as a Secret: v1.Secret.Data: base64Codec: invalid input, error found in #10 byte of ...|omething":{}},"kind"|..., bigger context ...|{"apiVersion":"v1","data":{"something":{}},"kind":"Secret","metadata":{"annotations":{"ku|...`,
			filtered: true,
		},
		{
			output: `namespace/sys-auth configured
rolebinding.rbac.authorization.k8s.io/vault-configmap-applier unchanged
Warning: kubectl apply should be used on resource created by either kubectl create --save-config or kubectl apply
configmap/vault-tls configured
clusterrole.rbac.authorization.k8s.io/system:metrics-server unchanged
secret/k8s-auth-conf unchanged
Error from server (BadRequest): error when creating "secrets/test.yaml": Secret in version "v1" cannot be handled as a Secret: v1.Secret.ObjectMeta: v1.ObjectMeta.TypeMeta: Kind: Data: decode base64: illegal base64 data at input byte 4, error found in #10 byte of ...|:"invalid"},"kind":"|..., bigger context ...|{"apiVersion":"v1","data":{"something":"invalid"},"kind":"Secret","metadata":{"annotations":{"kube|...
`,
			filtered: true,
		},
		{
			output: `namespace/sys-auth configured (server dry run)
rolebinding.rbac.authorization.k8s.io/vault-configmap-applier unchanged (server dry run)
Warning: kubectl apply should be used on resource created by either kubectl create --save-config or kubectl apply
configmap/vault-tls configured (server dry run)
clusterrole.rbac.authorization.k8s.io/system:metrics-server unchanged (server dry run)
secret/k8s-auth-conf unchanged (server dry run)
The request is invalid: patch: Invalid value: "map[data:map[something:invalid] metadata:map[annotations:map[kubectl.kubernetes.io/last-applied-configuration:{\"apiVersion\":\"v1\",\"data\":{\"something\":\"invalid\"},\"kind\":\"Secret\",\"metadata\":{\"annotations\":{},\"name\":\"test\",\"namespace\":\"sys-auth\"},\"type\":\"Opaque\"}\n]]]": error decoding from json: illegal base64 data at input byte 4
`,
			filtered: true,
		},
		{
			output:   `The Secret "test_" is invalid: metadata.name: Invalid value: "test_": a DNS-1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')`,
			filtered: true,
		},
		{
			output:   `The request is invalid: patch: Invalid value: "map[data:map[invalid:map[]] metadata:map[annotations:map[kubectl.kubernetes.io/last-applied-configuration:{\"apiVersion\":\"v1\",\"data\":{\"invalid\":{}},\"kind\":\"ConfigMap\",\"metadata\":{\"annotations\":{},\"name\":\"test\",\"namespace\":\"labs\"}}\n]]]": unrecognized type: string`,
			filtered: false,
		},
		{
			output: `namespace/sys-auth configured (server dry run)
rolebinding.rbac.authorization.k8s.io/vault-configmap-applier unchanged (server dry run)
Warning: kubectl apply should be used on resource created by either kubectl create --save-config or kubectl apply
configmap/vault-tls configured (server dry run)
clusterrole.rbac.authorization.k8s.io/system:metrics-server unchanged (server dry run)
secret/k8s-auth-conf unchanged (server dry run)
`,
			filtered: false,
		},
		{
			output: `namespace/sys-auth configured (server dry run)
rolebinding.rbac.authorization.k8s.io/vault-configmap-applier unchanged (server dry run)
Warning: kubectl apply should be used on resource created by either kubectl create --save-config or kubectl apply
configmap/vault-tls configured (server dry run)
clusterrole.rbac.authorization.k8s.io/system:metrics-server unchanged (server dry run)
secret/k8s-auth-conf unchanged (server dry run)
The request is invalid: patch: Invalid value: "map[data:map[invalid:map[]] metadata:map[annotations:map[kubectl.kubernetes.io/last-applied-configuration:{\"apiVersion\":\"v1\",\"data\":{\"invalid\":{}},\"kind\":\"ConfigMap\",\"metadata\":{\"annotations\":{},\"name\":\"test\",\"namespace\":\"labs\"}}\n]]]": unrecognized type: string
`,
			filtered: false,
		},
	}

	for _, tc := range testCases {
		var want string
		if tc.filtered {
			want = omitErrOutputMessage
		} else {
			want = tc.output
		}

		got := filterErrOutput(tc.output)

		if diff := deep.Equal(got, want); diff != nil {
			t.Error(diff)
		}
	}

	for _, term := range omitErrOutputTerms {
		want := omitErrOutputMessage
		got := filterErrOutput(term)

		if diff := deep.Equal(got, want); diff != nil {
			t.Error(diff)
		}
	}
}

func TestApplyFlagsArgs(t *testing.T) {
	testCases := []struct {
		flags ApplyFlags
		want  []string
	}{
		{
			flags: ApplyFlags{
				Namespace:      "example",
				DryRunStrategy: "server",
				PruneWhitelist: []string{
					"core/v1/ConfigMap",
					"core/v1/Pod",
					"rbac.authorization.k8s.io/v1beta1/RoleBinding",
				},
				ServerSide: true,
			},
			want: []string{"-n", "example", "--dry-run=server",
				"--server-side",
				"--prune", "--all",
				"--prune-whitelist=core/v1/ConfigMap",
				"--prune-whitelist=core/v1/Pod",
				"--prune-whitelist=rbac.authorization.k8s.io/v1beta1/RoleBinding",
			},
		},
		{
			flags: ApplyFlags{
				Namespace:      "example",
				DryRunStrategy: "server",
				PruneWhitelist: []string{
					"core/v1/ConfigMap",
					"core/v1/Pod",
					"rbac.authorization.k8s.io/v1beta1/RoleBinding",
				},
			},
			want: []string{"-n", "example", "--dry-run=server",
				"--prune", "--all",
				"--prune-whitelist=core/v1/ConfigMap",
				"--prune-whitelist=core/v1/Pod",
				"--prune-whitelist=rbac.authorization.k8s.io/v1beta1/RoleBinding",
			},
		},
		{
			flags: ApplyFlags{
				Namespace: "example",
				PruneWhitelist: []string{
					"core/v1/ConfigMap",
					"core/v1/Pod",
					"rbac.authorization.k8s.io/v1beta1/RoleBinding",
				},
			},
			want: []string{"-n", "example",
				"--prune", "--all",
				"--prune-whitelist=core/v1/ConfigMap",
				"--prune-whitelist=core/v1/Pod",
				"--prune-whitelist=rbac.authorization.k8s.io/v1beta1/RoleBinding",
			},
		},
		{
			flags: ApplyFlags{
				Namespace:      "example",
				DryRunStrategy: "server",
			},
			want: []string{"-n", "example", "--dry-run=server"},
		},
		{
			flags: ApplyFlags{
				PruneWhitelist: []string{
					"core/v1/ConfigMap",
					"core/v1/Pod",
					"rbac.authorization.k8s.io/v1beta1/RoleBinding",
				},
			},
			want: []string{"--prune", "--all",
				"--prune-whitelist=core/v1/ConfigMap",
				"--prune-whitelist=core/v1/Pod",
				"--prune-whitelist=rbac.authorization.k8s.io/v1beta1/RoleBinding",
			},
		},
	}

	for _, tc := range testCases {
		got := tc.flags.Args()

		if diff := deep.Equal(got, tc.want); diff != nil {
			t.Error(diff)
		}
	}
}

func TestSplitSecrets(t *testing.T) {
	testCases := []struct {
		yamlData      []byte
		resourcesData []byte
		secretsData   []byte
	}{
		{
			yamlData: []byte(`apiVersion: v1
kind: Secret
metadata:
  name: example 
  namespace: example-ns
stringData:
  some-key: some-value
  some-other-key: some-other-value
---
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    kube-applier.io/dry-run: "false"
    kube-applier.io/enabled: "true"
    kube-applier.io/prune: "true"
  labels:
    name: example-ns
    some-label: some-value
  name: example-ns
---
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    some-annotation: some-value
  name: example 
---
apiVersion: v1
data:
  some-value: c2Vuc2l0aXZlCg==
kind: Secret
metadata:
  name: example-1 
  namespace: example-ns
type: Opaque
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: example 
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
- kind: ServiceAccount
  name: kube-applier
  namespace: example-ns 
`),
			resourcesData: []byte(`apiVersion: v1
kind: Namespace
metadata:
  annotations:
    kube-applier.io/dry-run: "false"
    kube-applier.io/enabled: "true"
    kube-applier.io/prune: "true"
  labels:
    name: example-ns
    some-label: some-value
  name: example-ns
---
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    some-annotation: some-value
  name: example
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: example
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
- kind: ServiceAccount
  name: kube-applier
  namespace: example-ns
`),
			secretsData: []byte(`apiVersion: v1
kind: Secret
metadata:
  name: example
  namespace: example-ns
stringData:
  some-key: some-value
  some-other-key: some-other-value
---
apiVersion: v1
data:
  some-value: c2Vuc2l0aXZlCg==
kind: Secret
metadata:
  name: example-1
  namespace: example-ns
type: Opaque
`),
		},
		{
			yamlData: []byte(`apiVersion: v1
kind: Namespace
metadata:
  annotations:
    kube-applier.io/dry-run: "false"
    kube-applier.io/enabled: "true"
    kube-applier.io/prune: "true"
  labels:
    name: example-ns
    some-label: some-value
  name: example-ns
---
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    some-annotation: some-value
  name: example 
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: example 
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
- kind: ServiceAccount
  name: kube-applier
  namespace: example-ns 
`),
			resourcesData: []byte(`apiVersion: v1
kind: Namespace
metadata:
  annotations:
    kube-applier.io/dry-run: "false"
    kube-applier.io/enabled: "true"
    kube-applier.io/prune: "true"
  labels:
    name: example-ns
    some-label: some-value
  name: example-ns
---
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    some-annotation: some-value
  name: example
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: example
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
- kind: ServiceAccount
  name: kube-applier
  namespace: example-ns
`),
			secretsData: []byte{},
		},
		{
			yamlData: []byte(`apiVersion: v1
kind: Secret
metadata:
  name: example 
  namespace: example-ns
stringData:
  some-key: some-value
  some-other-key: some-other-value
---
apiVersion: v1
data:
  some-value: c2Vuc2l0aXZlCg==
kind: Secret
metadata:
  name: example-1 
  namespace: example-ns
type: Opaque
`),
			resourcesData: []byte{},
			secretsData: []byte(`apiVersion: v1
kind: Secret
metadata:
  name: example
  namespace: example-ns
stringData:
  some-key: some-value
  some-other-key: some-other-value
---
apiVersion: v1
data:
  some-value: c2Vuc2l0aXZlCg==
kind: Secret
metadata:
  name: example-1
  namespace: example-ns
type: Opaque
`),
		},
		{
			yamlData:      []byte{},
			resourcesData: []byte{},
			secretsData:   []byte{},
		},
	}

	for _, tc := range testCases {
		resources, secrets, err := splitSecrets(tc.yamlData)
		if err != nil {
			t.Error(err)
			continue
		}

		if diff := deep.Equal(string(resources), string(tc.resourcesData)); diff != nil {
			t.Error(diff)
		}

		if diff := deep.Equal(string(secrets), string(tc.secretsData)); diff != nil {
			t.Error(diff)
		}
	}
}
