package run

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/kubectl"
)

var _ = Describe("Runner", func() {
	var (
		testRunner       Runner
		testApplyOptions = &ApplyOptions{
			ClusterResources:    []string{"core/v1/Namespace"},
			NamespacedResources: []string{"core/v1/Pod", "apps/v1/Deployment"},
		}
	)

	BeforeEach(func() {
		testRunner = Runner{
			KubectlClient: &kubectl.Client{
				Host:    testConfig.Host,
				Metrics: testMetricsClient,
				Timeout: time.Duration(time.Minute),
			},
			Metrics:        testMetricsClient,
			Clock:          &zeroClock{},
			DryRun:         false,
			PruneBlacklist: []string{"apps/v1/ControllerRevision"},
		}
	})

	Context("When operating on an empty Application list", func() {
		It("Should be a no-op", func() {
			appList := []kubeapplierv1alpha1.Application{}
			appListExpected := []kubeapplierv1alpha1.Application{}

			testRunner.Apply("../testdata/manifests", appList, testApplyOptions)

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
			expectedStatusRunInfo := kubeapplierv1alpha1.ApplicationStatusRunInfo{}

			kubectlPath, err := testRunner.KubectlClient.Path()
			Expect(err).Should(BeNil())
			Expect(kubectlPath).ShouldNot(BeEmpty())

			expectedStatus := []*kubeapplierv1alpha1.ApplicationStatusRun{
				{
					Command:      fmt.Sprintf("%s --server %s apply -f ../testdata/manifests/app-a -R -n app-a --dry-run=none --prune --all --prune-whitelist=core/v1/Pod --prune-whitelist=apps/v1/Deployment", kubectlPath, testConfig.Host),
					ErrorMessage: "",
					Finished:     metav1.Time{},
					Info:         expectedStatusRunInfo,
					Output: `namespace/app-a created
deployment.apps/test-deployment created
`,
					Started: metav1.Time{},
					Success: true,
				},
				{
					Command:      fmt.Sprintf("%s --server %s apply -f ../testdata/manifests/app-b -R -n app-b --dry-run=none --prune --all --prune-whitelist=core/v1/Pod --prune-whitelist=apps/v1/Deployment --prune-whitelist=core/v1/Namespace", kubectlPath, testConfig.Host),
					ErrorMessage: "exit status 1",
					Finished:     metav1.Time{},
					Info:         expectedStatusRunInfo,
					Output: `namespace/app-b created
error: error validating "../testdata/manifests/app-b/deployment.yaml": error validating data: ValidationError(Deployment.spec.template.spec): missing required field "containers" in io.k8s.api.core.v1.PodSpec; if you choose to ignore these errors, turn validation off with --validate=false
`,
					Started: metav1.Time{},
					Success: false,
				},
				{
					Command:      fmt.Sprintf("%s --server %s apply -f ../testdata/manifests/app-c -R -n app-c --dry-run=server --prune --all --prune-whitelist=apps/v1/Deployment", kubectlPath, testConfig.Host),
					ErrorMessage: "exit status 1",
					Finished:     metav1.Time{},
					Info:         expectedStatusRunInfo,
					Output: `namespace/app-c created (server dry run)
Error from server (NotFound): error when creating "../testdata/manifests/app-c/deployment.yaml": namespaces "app-c" not found
`,
					Started: metav1.Time{},
					Success: false,
				},
			}

			By("Applying all the Applications and populating their Status subresource with the results")
			testRunner.Apply("../testdata/manifests", appList, testApplyOptions)

			for i := range appList {
				Expect(appList[i].Status.LastRun).ShouldNot(BeNil())
				Expect(appList[i].Status.LastRun).Should(Equal(expectedStatus[i]))
			}
		})
	})
})
