package run

import (
	"fmt"
	"testing"
	"time"

	"github.com/utilitywarehouse/kube-applier/kube"
	"github.com/utilitywarehouse/kube-applier/kubectl"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

var (
	testApplyOptions = &ApplyOptions{
		ClusterResources: []string{
			"core/v1/Namespace",
			"core/v1/PersistentVolume",
			"storage.k8s.io/v1/StorageClass",
		},
		NamespacedResources: []string{
			"core/v1/Pod",
			"apps/v1/Deployment",
			"autoscaling/v1/HorizontalPodAutoscaler",
		},
	}
	testAllResources = append(testApplyOptions.NamespacedResources, testApplyOptions.ClusterResources...)
)

type batchTestCase struct {
	ba           BatchApplier
	rootPath     string
	applyList    []string
	applyOptions *ApplyOptions

	expectedSuccesses []ApplyAttempt
	expectedFailures  []ApplyAttempt
}

type zeroClock struct{}

func (c *zeroClock) Now() time.Time                  { return time.Time{} }
func (c *zeroClock) Since(t time.Time) time.Duration { return time.Duration(0) }
func (c *zeroClock) Sleep(d time.Duration)           {}

func TestBatchApplierApply(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// Empty apply list
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
		},
		"",
		[]string{},
		testApplyOptions,
		[]ApplyAttempt{},
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplySuccess(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubeClient := kube.NewMockClientInterface(mockCtrl)
	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// All files succeed
	applyList := []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file1", kubeClient),
		expectApplyAndReturnSuccess("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file1", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file2", kubeClient),
		expectApplyAndReturnSuccess("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file2", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file3", kubeClient),
		expectApplyAndReturnSuccess("/repo/file3", kubectl.ApplyFlags{Namespace: "file3", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file3", metrics),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "", Info{}, time.Time{}, time.Time{}},
		{"file2", "cmd /repo/file2", "output /repo/file2", "", Info{}, time.Time{}, time.Time{}},
		{"file3", "cmd /repo/file3", "output /repo/file3", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplyFail(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubeClient := kube.NewMockClientInterface(mockCtrl)
	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// All files fail
	applyList := []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file1", kubeClient),
		expectApplyAndReturnFailure("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectFailureMetric("file1", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file2", kubeClient),
		expectApplyAndReturnFailure("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectFailureMetric("file2", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file3", kubeClient),
		expectApplyAndReturnFailure("/repo/file3", kubectl.ApplyFlags{Namespace: "file3", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectFailureMetric("file3", metrics),
	)
	failures := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "error /repo/file1", Info{}, time.Time{}, time.Time{}},
		{"file2", "cmd /repo/file2", "output /repo/file2", "error /repo/file2", Info{}, time.Time{}, time.Time{}},
		{"file3", "cmd /repo/file3", "output /repo/file3", "error /repo/file3", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
		},
		"/repo",
		applyList,
		testApplyOptions,
		[]ApplyAttempt{},
		failures,
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplyPartial(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// Some successes, some failures
	applyList := []string{"file1", "file2", "file3", "file4"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file1", kubeClient),
		expectApplyAndReturnSuccess("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file1", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file2", kubeClient),
		expectApplyAndReturnFailure("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectFailureMetric("file2", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file3", kubeClient),
		expectApplyAndReturnSuccess("/repo/file3", kubectl.ApplyFlags{Namespace: "file3", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file3", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file4", kubeClient),
		expectApplyAndReturnFailure("/repo/file4", kubectl.ApplyFlags{Namespace: "file4", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectFailureMetric("file4", metrics),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "", Info{}, time.Time{}, time.Time{}},
		{"file3", "cmd /repo/file3", "output /repo/file3", "", Info{}, time.Time{}, time.Time{}},
	}
	failures := []ApplyAttempt{
		{"file2", "cmd /repo/file2", "output /repo/file2", "error /repo/file2", Info{}, time.Time{}, time.Time{}},
		{"file4", "cmd /repo/file4", "output /repo/file4", "error /repo/file4", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		failures,
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplySuccessDryRun(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// All files succeed dry-run
	applyList := []string{"file1", "file2", "file3"}

	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file1", kubeClient),
		expectApplyAndReturnSuccess("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "server", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file1", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file2", kubeClient),
		expectApplyAndReturnSuccess("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "server", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file2", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file3", kubeClient),
		expectApplyAndReturnSuccess("/repo/file3", kubectl.ApplyFlags{Namespace: "file3", DryRunStrategy: "server", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file3", metrics),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "", Info{}, time.Time{}, time.Time{}},
		{"file2", "cmd /repo/file2", "output /repo/file2", "", Info{}, time.Time{}, time.Time{}},
		{"file3", "cmd /repo/file3", "output /repo/file3", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
			DryRun:        true,
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplySuccessDryRunNamespaces(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// All files succeed dry-run namespaces
	applyList := []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true", DryRun: "true"}, "file1", kubeClient),
		expectApplyAndReturnSuccess("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "server", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file1", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file2", kubeClient),
		expectApplyAndReturnSuccess("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file2", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true", DryRun: "true"}, "file3", kubeClient),
		expectApplyAndReturnSuccess("/repo/file3", kubectl.ApplyFlags{Namespace: "file3", DryRunStrategy: "server", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file3", metrics),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "", Info{}, time.Time{}, time.Time{}},
		{"file2", "cmd /repo/file2", "output /repo/file2", "", Info{}, time.Time{}, time.Time{}},
		{"file3", "cmd /repo/file3", "output /repo/file3", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
			DryRun:        false,
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplySuccessDryRunAndDryRunNamespaces(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// All files succeed dry-run and dry-run namespaces
	applyList := []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true", DryRun: "true"}, "file1", kubeClient),
		expectApplyAndReturnSuccess("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "server", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file1", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file2", kubeClient),
		expectApplyAndReturnSuccess("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "server", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file2", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true", DryRun: "true"}, "file3", kubeClient),
		expectApplyAndReturnSuccess("/repo/file3", kubectl.ApplyFlags{Namespace: "file3", DryRunStrategy: "server", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file3", metrics),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "", Info{}, time.Time{}, time.Time{}},
		{"file2", "cmd /repo/file2", "output /repo/file2", "", Info{}, time.Time{}, time.Time{}},
		{"file3", "cmd /repo/file3", "output /repo/file3", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
			DryRun:        true,
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplyDisabledNamespaces(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	//Disabled namespaces
	applyList := []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "false"}, "file1", kubeClient),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file2", kubeClient),
		expectApplyAndReturnSuccess("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file2", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "false"}, "file3", kubeClient),
	)
	successes := []ApplyAttempt{
		{"file2", "cmd /repo/file2", "output /repo/file2", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
			DryRun:        false,
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplyInvalidAnnotation(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	//Unsupported automatic deployment option on namespace
	applyList := []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "unsupportedOption"}, "file1", kubeClient),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file2", kubeClient),
		expectApplyAndReturnSuccess("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file2", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "unsupportedOption"}, "file3", kubeClient),
	)
	successes := []ApplyAttempt{
		{"file2", "cmd /repo/file2", "output /repo/file2", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
			DryRun:        false,
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplySuccessPruneTrue(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	applyList := []string{"file1", "file2"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true", Prune: "true"}, "file1", kubeClient),
		expectApplyAndReturnSuccess("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file1", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file2", kubeClient),
		expectApplyAndReturnSuccess("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file2", metrics),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "", Info{}, time.Time{}, time.Time{}},
		{"file2", "cmd /repo/file2", "output /repo/file2", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
			DryRun:        false,
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplySuccessPruneClusterResources(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	applyList := []string{"file1"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true", PruneClusterResources: "true"}, "file1", kubeClient),
		expectApplyAndReturnSuccess("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "none", PruneWhitelist: testAllResources}, kubectlClient),
		expectSuccessMetric("file1", metrics),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
			DryRun:        false,
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplySuccessPruneFalse(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	applyList := []string{"file1", "file2"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true", Prune: "false"}, "file1", kubeClient),
		expectApplyAndReturnSuccess("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "none"}, kubectlClient),
		expectSuccessMetric("file1", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file2", kubeClient),
		expectApplyAndReturnSuccess("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file2", metrics),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "", Info{}, time.Time{}, time.Time{}},
		{"file2", "cmd /repo/file2", "output /repo/file2", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
			DryRun:        false,
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplySuccessPruneBlacklist(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// Valid yaml list
	pruneBlacklist := []string{
		"apps/v1/Deployment",
		"core/v1/Pod",
		"core/v1/PersistentVolume",
		"storage.k8s.io/v1/StorageClass",
	}

	applyList := []string{"file1", "file2"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file1", kubeClient),
		expectApplyAndReturnSuccess("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "none", PruneWhitelist: []string{"autoscaling/v1/HorizontalPodAutoscaler"}}, kubectlClient),
		expectSuccessMetric("file1", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true", PruneClusterResources: "true"}, "file2", kubeClient),
		expectApplyAndReturnSuccess("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "none", PruneWhitelist: []string{"autoscaling/v1/HorizontalPodAutoscaler", "core/v1/Namespace"}}, kubectlClient),
		expectSuccessMetric("file2", metrics),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "", Info{}, time.Time{}, time.Time{}},
		{"file2", "cmd /repo/file2", "output /repo/file2", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient:  kubectlClient,
			KubeClient:     kubeClient,
			Metrics:        metrics,
			Clock:          &zeroClock{},
			DryRun:         false,
			PruneBlacklist: pruneBlacklist,
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func TestBatchApplierApplySuccessServerSide(t *testing.T) {
	log.InitLogger("info")
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubeClient := kube.NewMockClientInterface(mockCtrl)
	kubectlClient := kubectl.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// All files succeed
	applyList := []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true", ServerSide: "false"}, "file1", kubeClient),
		expectApplyAndReturnSuccess("/repo/file1", kubectl.ApplyFlags{Namespace: "file1", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file1", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true", ServerSide: "true"}, "file2", kubeClient),
		expectApplyAndReturnSuccess("/repo/file2", kubectl.ApplyFlags{Namespace: "file2", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources, ServerSide: true}, kubectlClient),
		expectSuccessMetric("file2", metrics),
	)
	gomock.InOrder(
		expectNamespaceAnnotationsAndReturn(kube.KAAnnotations{Enabled: "true"}, "file3", kubeClient),
		expectApplyAndReturnSuccess("/repo/file3", kubectl.ApplyFlags{Namespace: "file3", DryRunStrategy: "none", PruneWhitelist: testApplyOptions.NamespacedResources}, kubectlClient),
		expectSuccessMetric("file3", metrics),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd /repo/file1", "output /repo/file1", "", Info{}, time.Time{}, time.Time{}},
		{"file2", "cmd /repo/file2", "output /repo/file2", "", Info{}, time.Time{}, time.Time{}},
		{"file3", "cmd /repo/file3", "output /repo/file3", "", Info{}, time.Time{}, time.Time{}},
	}
	tc := batchTestCase{
		BatchApplier{
			KubectlClient: kubectlClient,
			KubeClient:    kubeClient,
			Metrics:       metrics,
			Clock:         &zeroClock{},
		},
		"/repo",
		applyList,
		testApplyOptions,
		successes,
		[]ApplyAttempt{},
	}
	applyAndAssert(t, tc)
}

func expectApplyAndReturnSuccess(file string, applyFlags kubectl.ApplyFlags, kubectlClient *kubectl.MockClientInterface) *gomock.Call {
	return kubectlClient.EXPECT().Apply(file, applyFlags).Times(1).Return("cmd "+file, "output "+file, nil)
}

func expectApplyAndReturnFailure(file string, applyFlags kubectl.ApplyFlags, kubectlClient *kubectl.MockClientInterface) *gomock.Call {
	return kubectlClient.EXPECT().Apply(file, applyFlags).Times(1).Return("cmd "+file, "output "+file, fmt.Errorf("error "+file))
}

func expectNamespaceAnnotationsAndReturn(ret kube.KAAnnotations, namespace string, kubeClient *kube.MockClientInterface) *gomock.Call {
	return kubeClient.EXPECT().NamespaceAnnotations(namespace).Times(1).Return(ret, nil)
}

func expectSuccessMetric(file string, metrics *metrics.MockPrometheusInterface) *gomock.Call {
	return metrics.EXPECT().UpdateNamespaceSuccess(file, true).Times(1)
}

func expectFailureMetric(file string, metrics *metrics.MockPrometheusInterface) *gomock.Call {
	return metrics.EXPECT().UpdateNamespaceSuccess(file, false).Times(1)
}

func applyAndAssert(t *testing.T, tc batchTestCase) {
	assert := assert.New(t)
	successes, failures := tc.ba.Apply(tc.rootPath, tc.applyList, tc.applyOptions)
	assert.Equal(tc.expectedSuccesses, successes)
	assert.Equal(tc.expectedFailures, failures)
}
