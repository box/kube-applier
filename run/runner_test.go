package run

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	gomegatypes "github.com/onsi/gomega/types"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/kubectl"
)

func TestApplyOptions_PruneWhitelist(t *testing.T) {
	assert := assert.New(t)

	applyOptions := &ApplyOptions{
		NamespacedResources: []string{"a", "b", "c"},
		ClusterResources:    []string{"d", "e", "f"},
	}

	testCases := []struct {
		options   *ApplyOptions
		app       *kubeapplierv1alpha1.Application
		blacklist []string
		expected  []string
	}{
		{
			&ApplyOptions{},
			&kubeapplierv1alpha1.Application{},
			[]string{},
			nil,
		},
		{
			&ApplyOptions{},
			&kubeapplierv1alpha1.Application{
				Spec: kubeapplierv1alpha1.ApplicationSpec{
					Prune: true,
				},
			},
			[]string{},
			nil,
		},
		{
			applyOptions,
			&kubeapplierv1alpha1.Application{
				Spec: kubeapplierv1alpha1.ApplicationSpec{
					Prune: true,
				},
			},
			[]string{},
			[]string{"a", "b", "c"},
		},
		{
			applyOptions,
			&kubeapplierv1alpha1.Application{
				Spec: kubeapplierv1alpha1.ApplicationSpec{
					Prune:          true,
					PruneBlacklist: []string{"b"},
				},
			},
			[]string{"c"},
			[]string{"a"},
		},
		{
			applyOptions,
			&kubeapplierv1alpha1.Application{
				Spec: kubeapplierv1alpha1.ApplicationSpec{
					Prune:                 true,
					PruneBlacklist:        []string{"b"},
					PruneClusterResources: true,
				},
			},
			[]string{"c"},
			[]string{"a", "d", "e", "f"},
		},
	}

	for _, tc := range testCases {
		assert.Equal(tc.options.PruneWhitelist(tc.app, tc.blacklist), tc.expected)
	}
}

var _ = Describe("Runner", func() {
	var (
		testRunner                         Runner
		testRunQueue                       chan<- Request
		testApplyOptions                   *ApplyOptions
		testKubectlPath, testKustomizePath string
	)

	BeforeEach(func() {
		testRunner = Runner{
			Clock:      &zeroClock{},
			DryRun:     false,
			KubeClient: testKubeClient,
			KubectlClient: &kubectl.Client{
				Host:    testConfig.Host,
				Timeout: time.Duration(time.Minute),
			},
			PruneBlacklist: []string{"apps/v1/ControllerRevision"},
			RepoPath:       "../testdata/manifests",
		}
		testRunQueue = testRunner.Start()
		kubectlPath := testRunner.KubectlClient.KubectlPath()
		Expect(kubectlPath).ShouldNot(BeEmpty())
		testKubectlPath = kubectlPath
		kustomizePath := testRunner.KubectlClient.KustomizePath()
		Expect(kustomizePath).ShouldNot(BeEmpty())
		testKustomizePath = kustomizePath

		cr, nr, err := testRunner.KubeClient.PrunableResourceGVKs()
		Expect(err).Should(BeNil())
		testApplyOptions = &ApplyOptions{
			ClusterResources:    cr,
			NamespacedResources: nr,
		}
	})

	Context("When operating on an empty Application list", func() {
		It("Should be a no-op", func() {
			appList := []kubeapplierv1alpha1.Application{}
			appListExpected := []kubeapplierv1alpha1.Application{}

			for i := range appList {
				testRunQueue <- Request{
					Type:        PollingRun,
					Application: &appList[i],
				}
			}
			testRunner.Stop()

			Expect(appList).Should(Equal(appListExpected))
		})
	})

	Context("When operating on an Application list", func() {
		It("Should update the Status subresources accordingly", func() {
			appList := []kubeapplierv1alpha1.Application{
				{
					TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "appA",
						Namespace: "app-a",
					},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						Prune:          true,
						RepositoryPath: "app-a",
					},
				},
				{
					TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "appB",
						Namespace: "app-b",
					},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						Prune:                 true,
						PruneClusterResources: true,
						RepositoryPath:        "app-b",
					},
				},
				{
					TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "appC",
						Namespace: "app-c",
					},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						DryRun:         true,
						Prune:          true,
						PruneBlacklist: []string{"core/v1/Pod"},
						RepositoryPath: "app-c",
					},
				},
			}

			expectedStatus := []*kubeapplierv1alpha1.ApplicationStatusRun{
				{
					Command:      "",
					ErrorMessage: "",
					Finished:     metav1.Time{},
					Output: `namespace/app-a created
deployment.apps/test-deployment created
`,
					Started: metav1.Time{},
					Success: true,
					Type:    PollingRun.String(),
				},
				{
					Command:      "",
					ErrorMessage: "exit status 1",
					Finished:     metav1.Time{},
					Output: `namespace/app-b created
error: error validating "../testdata/manifests/app-b/deployment.yaml": error validating data: ValidationError(Deployment.spec.template.spec): missing required field "containers" in io.k8s.api.core.v1.PodSpec; if you choose to ignore these errors, turn validation off with --validate=false
`,
					Started: metav1.Time{},
					Success: false,
					Type:    PollingRun.String(),
				},
				{
					Command:      "",
					ErrorMessage: "exit status 1",
					Finished:     metav1.Time{},
					Output: `namespace/app-c created (server dry run)
Error from server (NotFound): error when creating "../testdata/manifests/app-c/deployment.yaml": namespaces "app-c" not found
`,
					Started: metav1.Time{},
					Success: false,
					Type:    PollingRun.String(),
				},
			}

			// construct expected app list
			expected := make([]kubeapplierv1alpha1.Application, len(appList))
			for i := range appList {
				expected[i] = appList[i]
				expected[i].Status = kubeapplierv1alpha1.ApplicationStatus{LastRun: expectedStatus[i]}
				headCommitHash, err := (&git.Util{RepoPath: testRunner.RepoPath}).HeadHashForPaths(expected[i].Spec.RepositoryPath)
				Expect(err).To(BeNil())
				expected[i].Status.LastRun.Commit = headCommitHash
			}

			By("Applying all the Applications and populating their Status subresource with the results")

			for i := range appList {
				testRunQueue <- Request{
					Type:        PollingRun,
					Application: &appList[i],
				}
			}
			testRunner.Stop()

			for i := range appList {
				Expect(appList[i]).Should(matchApplication(expected[i], testKubectlPath, "", testRunner.RepoPath, testApplyOptions.PruneWhitelist(&appList[i], testRunner.PruneBlacklist)))
			}
		})
	})

	Context("When operating on an Application that uses kustomize", func() {
		It("Should be able to build and apply", func() {
			app := kubeapplierv1alpha1.Application{
				TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "appA",
					Namespace: "app-a-kustomize",
				},
				Spec: kubeapplierv1alpha1.ApplicationSpec{
					Prune:          true,
					RepositoryPath: "app-a-kustomize",
				},
			}

			headCommitHash, err := (&git.Util{RepoPath: testRunner.RepoPath}).HeadHashForPaths(app.Spec.RepositoryPath)
			Expect(err).To(BeNil())
			expected := app
			expected.Status = kubeapplierv1alpha1.ApplicationStatus{
				LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
					Command:      "",
					Commit:       headCommitHash,
					ErrorMessage: "exit status 1",
					Finished:     metav1.Time{},
					Output: `namespace/app-a-kustomize created
deployment.apps/test-deployment created
Some error output has been omitted because it may contain sensitive data
`,
					Started: metav1.Time{},
					Success: false,
					Type:    PollingRun.String(),
				},
			}

			testRunQueue <- Request{
				Type:        PollingRun,
				Application: &app,
			}
			testRunner.Stop()

			Expect(app).Should(matchApplication(expected, testKubectlPath, testKustomizePath, testRunner.RepoPath, testApplyOptions.PruneWhitelist(&app, testRunner.PruneBlacklist)))
		})
	})

	Context("When operating on an Application that defines a strongbox keyring", func() {
		It("Should be able to apply encrypted files, given a strongbox keyring secret", func() {
			// Instead of creating the namespace using the test kube client, we
			// instead use a "hack" here by requesting a run for an Application
			// pointing to a single file that defines the namespace. This is to
			// avoid kubectl apply warnings in the output below.
			testRunQueue <- Request{
				Type: PollingRun,
				Application: &kubeapplierv1alpha1.Application{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foobar",
						Namespace: "app-d",
					},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						Prune:          false,
						RepositoryPath: "app-d/00-namespace.yaml",
					},
				},
			}
			ns := &corev1.Namespace{}
			Eventually(
				func() bool {
					return testKubeClient.Get(context.TODO(), client.ObjectKey{Name: "app-d"}, ns) == nil
				},
				time.Second*15,
				time.Second,
			).Should(BeTrue())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "strongbox",
					Namespace: ns.Name,
				},
				StringData: map[string]string{
					".strongbox_keyring": `keyentries:
- description: foobar
  key-id: G4M/cCqr+LZtEyQbAjSu5SMEcnVTj2IkWahrkOUq/J4=
  key: QxK6PHX37IybXRshJZy4IXRjCdFFsE0wdiYlfeGP1QA=`,
				},
				Type: corev1.SecretTypeOpaque,
			}
			Expect(testKubeClient.Create(context.TODO(), secret)).To(BeNil())
			secretEmpty := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "strongbox-empty",
					Namespace: ns.Name,
				},
				Type: corev1.SecretTypeOpaque,
			}
			Expect(testKubeClient.Create(context.TODO(), secretEmpty)).To(BeNil())

			appList := []kubeapplierv1alpha1.Application{
				{
					TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "appD",
						Namespace: ns.Name,
					},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						Prune:          true,
						RepositoryPath: "app-d",
					},
				},
				{
					TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "appD",
						Namespace: ns.Name,
					},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						Prune:                     true,
						RepositoryPath:            "app-d",
						StrongboxKeyringSecretRef: "invalid",
					},
				},
				{
					TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "appD",
						Namespace: ns.Name,
					},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						Prune:                     true,
						RepositoryPath:            "app-d",
						StrongboxKeyringSecretRef: secretEmpty.Name,
					},
				},
				{
					TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "appD",
						Namespace: ns.Name,
					},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						Prune:                     true,
						RepositoryPath:            "app-d",
						StrongboxKeyringSecretRef: secret.Name,
					},
				},
			}

			headCommitHash, err := (&git.Util{RepoPath: testRunner.RepoPath}).HeadHashForPaths("app-d")
			Expect(err).To(BeNil())
			Expect(headCommitHash).ToNot(BeEmpty())

			expectedStatus := []*kubeapplierv1alpha1.ApplicationStatusRun{
				{
					Command:      "",
					Commit:       headCommitHash,
					ErrorMessage: "exit status 1",
					Finished:     metav1.Time{},
					Output: `namespace/app-d unchanged
error: error validating "../testdata/manifests/app-d/deployment.yaml": error validating data: invalid object to validate; if you choose to ignore these errors, turn validation off with --validate=false
`,
					Started: metav1.Time{},
					Success: false,
					Type:    PollingRun.String(),
				},
				{
					Command:      "^.*$",
					Commit:       "",
					ErrorMessage: `secrets "invalid" not found`,
					Finished:     metav1.Time{},
					Output:       "",
					Started:      metav1.Time{},
					Success:      false,
					Type:         PollingRun.String(),
				},
				{
					Command:      "^.*$",
					Commit:       "",
					ErrorMessage: `Secret app-d/strongbox-empty does not contain key '.strongbox_keyring'`,
					Finished:     metav1.Time{},
					Output:       "",
					Started:      metav1.Time{},
					Success:      false,
					Type:         PollingRun.String(),
				},
				{
					Command:      "",
					Commit:       headCommitHash,
					ErrorMessage: "",
					Finished:     metav1.Time{},
					Output: `namespace/app-d unchanged
deployment.apps/test-deployment created
`,
					Started: metav1.Time{},
					Success: true,
					Type:    PollingRun.String(),
				},
			}

			// construct expected app list
			expected := make([]kubeapplierv1alpha1.Application, len(appList))
			for i := range appList {
				expected[i] = appList[i]
				expected[i].Status = kubeapplierv1alpha1.ApplicationStatus{LastRun: expectedStatus[i]}
			}

			for i := range appList {
				testRunQueue <- Request{
					Type:        PollingRun,
					Application: &appList[i],
				}
			}

			Eventually(
				func() bool {
					deployment := &appsv1.Deployment{}
					return testKubeClient.Get(context.TODO(), client.ObjectKey{Namespace: ns.Name, Name: "test-deployment"}, deployment) == nil
				},
				time.Second*15,
				time.Second,
			).Should(BeTrue())

			testRunner.Stop()

			for i := range appList {
				Expect(appList[i]).Should(matchApplication(expected[i], testKubectlPath, "", testRunner.RepoPath, testApplyOptions.PruneWhitelist(&appList[i], testRunner.PruneBlacklist)))
			}
		})
	})
})

func matchApplication(expected kubeapplierv1alpha1.Application, kubectlPath, kustomizePath, repoPath string, pruneWhitelist []string) gomegatypes.GomegaMatcher {
	commandMatcher := Ignore()
	if expected.Status.LastRun.Command != "^.*$" {
		commandExtraArgs := expected.Status.LastRun.Command
		if expected.Spec.DryRun {
			commandExtraArgs += " --dry-run=server"
		} else {
			commandExtraArgs += " --dry-run=none"
		}
		if expected.Spec.Prune {
			commandExtraArgs += fmt.Sprintf(" --prune --all --prune-whitelist=%s", strings.Join(pruneWhitelist, " --prune-whitelist="))
		}
		if kustomizePath == "" {
			commandMatcher = MatchRegexp(
				"^%s --server %s apply -f [^ ]+/%s -R -n %s%s",
				kubectlPath,
				testConfig.Host,
				expected.Spec.RepositoryPath,
				expected.Namespace,
				commandExtraArgs,
			)
		} else {
			commandMatcher = MatchRegexp(
				"^%s build [^ ]+/%s | %s --server %s apply -f - -R -n %s%s",
				kustomizePath,
				expected.Spec.RepositoryPath,
				kubectlPath,
				testConfig.Host,
				expected.Namespace,
				commandExtraArgs,
			)
		}
	}
	return MatchAllFields(Fields{
		"TypeMeta":   Equal(expected.TypeMeta),
		"ObjectMeta": Equal(expected.ObjectMeta),
		"Spec":       Equal(expected.Spec),
		"Status": MatchAllFields(Fields{
			"LastRun": PointTo(MatchAllFields(Fields{
				"Command":      commandMatcher,
				"Commit":       Equal(expected.Status.LastRun.Commit),
				"ErrorMessage": Equal(expected.Status.LastRun.ErrorMessage),
				"Finished": And(
					Equal(expected.Status.LastRun.Finished),
					// Ideally we would be comparing to actual's Started but since it
					// should be equal to expected' Started, this is equivalent.
					MatchAllFields(Fields{
						"Time": BeTemporally(">=", expected.Status.LastRun.Started.Time),
					}),
				),
				"Output": MatchRegexp("^%s$", strings.Replace(
					regexp.QuoteMeta(expected.Status.LastRun.Output),
					regexp.QuoteMeta(repoPath),
					"[^ ]+",
					-1,
				)),
				"Started": Equal(expected.Status.LastRun.Started),
				"Success": Equal(expected.Status.LastRun.Success),
				"Type":    Equal(expected.Status.LastRun.Type),
			})),
		}),
	})
}
