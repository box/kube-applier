package run_test

import (
	"fmt"
	"github.com/box/kube-applier/kube"
	"github.com/box/kube-applier/metrics"
	"github.com/box/kube-applier/run"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"testing"
)

type batchTestCase struct {
	kubeClient kube.ClientInterface
	metrics    metrics.PrometheusInterface
	applyList  []string

	expectedSuccesses []run.ApplyAttempt
	expectedFailures  []run.ApplyAttempt
}

func TestBatchApplierApply(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubeClient := kube.NewMockClientInterface(mockCtrl)
	metrics := metrics.NewMockPrometheusInterface(mockCtrl)

	// Empty apply list
	tc := batchTestCase{kubeClient, metrics, []string{}, []run.ApplyAttempt{}, []run.ApplyAttempt{}}
	expectCheckVersionAndReturnNil(kubeClient)
	applyAndAssert(t, tc)

	// All files succeed
	applyList := []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectCheckVersionAndReturnNil(kubeClient),
		expectApplyAndReturnSuccess("file1", kubeClient),
		expectSuccessMetric("file1", metrics),
		expectApplyAndReturnSuccess("file2", kubeClient),
		expectSuccessMetric("file2", metrics),
		expectApplyAndReturnSuccess("file3", kubeClient),
		expectSuccessMetric("file3", metrics),
	)
	successes := []run.ApplyAttempt{
		{"file1", "cmd file1", "output file1", ""},
		{"file2", "cmd file2", "output file2", ""},
		{"file3", "cmd file3", "output file3", ""},
	}
	tc = batchTestCase{kubeClient, metrics, applyList, successes, []run.ApplyAttempt{}}
	applyAndAssert(t, tc)

	// All files fail
	applyList = []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectCheckVersionAndReturnNil(kubeClient),
		expectApplyAndReturnFailure("file1", kubeClient),
		expectFailureMetric("file1", metrics),
		expectApplyAndReturnFailure("file2", kubeClient),
		expectFailureMetric("file2", metrics),
		expectApplyAndReturnFailure("file3", kubeClient),
		expectFailureMetric("file3", metrics),
	)
	failures := []run.ApplyAttempt{
		{"file1", "cmd file1", "output file1", "error file1"},
		{"file2", "cmd file2", "output file2", "error file2"},
		{"file3", "cmd file3", "output file3", "error file3"},
	}
	tc = batchTestCase{kubeClient, metrics, applyList, []run.ApplyAttempt{}, failures}
	applyAndAssert(t, tc)

	// Some successes, some failures
	applyList = []string{"file1", "file2", "file3", "file4"}
	gomock.InOrder(
		expectCheckVersionAndReturnNil(kubeClient),
		expectApplyAndReturnSuccess("file1", kubeClient),
		expectSuccessMetric("file1", metrics),
		expectApplyAndReturnFailure("file2", kubeClient),
		expectFailureMetric("file2", metrics),
		expectApplyAndReturnSuccess("file3", kubeClient),
		expectSuccessMetric("file3", metrics),
		expectApplyAndReturnFailure("file4", kubeClient),
		expectFailureMetric("file4", metrics),
	)
	successes = []run.ApplyAttempt{
		{"file1", "cmd file1", "output file1", ""},
		{"file3", "cmd file3", "output file3", ""},
	}
	failures = []run.ApplyAttempt{
		{"file2", "cmd file2", "output file2", "error file2"},
		{"file4", "cmd file4", "output file4", "error file4"},
	}
	tc = batchTestCase{kubeClient, metrics, applyList, successes, failures}
	applyAndAssert(t, tc)
}

func expectCheckVersionAndReturnNil(kubeClient *kube.MockClientInterface) *gomock.Call {
	return kubeClient.EXPECT().CheckVersion().Times(1).Return(nil)
}

func expectApplyAndReturnSuccess(file string, kubeClient *kube.MockClientInterface) *gomock.Call {
	return kubeClient.EXPECT().Apply(file).Times(1).Return("cmd "+file, "output "+file, nil)
}

func expectApplyAndReturnFailure(file string, kubeClient *kube.MockClientInterface) *gomock.Call {
	return kubeClient.EXPECT().Apply(file).Times(1).Return("cmd "+file, "output "+file, fmt.Errorf("error "+file))
}

func expectSuccessMetric(file string, metrics *metrics.MockPrometheusInterface) *gomock.Call {
	return metrics.EXPECT().UpdateFileSuccess(file, true).Times(1)
}

func expectFailureMetric(file string, metrics *metrics.MockPrometheusInterface) *gomock.Call {
	return metrics.EXPECT().UpdateFileSuccess(file, false).Times(1)
}

func applyAndAssert(t *testing.T, tc batchTestCase) {
	assert := assert.New(t)
	ba := run.BatchApplier{tc.kubeClient, tc.metrics}
	successes, failures := ba.Apply(tc.applyList)
	assert.Equal(tc.expectedSuccesses, successes)
	assert.Equal(tc.expectedFailures, failures)
}
