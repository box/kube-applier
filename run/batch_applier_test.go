package run

import (
	"fmt"
	"github.com/box/kube-applier/kube"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"testing"
)

type batchTestCase struct {
	kubeClient kube.ClientInterface
	applyList  []string

	expectedSuccesses []ApplyAttempt
	expectedFailures  []ApplyAttempt
}

func TestBatchApplierApply(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	kubeClient := kube.NewMockClientInterface(mockCtrl)
	runCount := 0

	// Empty apply list
	tc := batchTestCase{kubeClient, []string{}, []ApplyAttempt{}, []ApplyAttempt{}}
	expectCheckVersionAndReturnNil(kubeClient)
	applyAndAssert(t, runCount, tc)
	runCount++

	// All files succeed
	applyList := []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectCheckVersionAndReturnNil(kubeClient),
		expectApplyAndReturnSuccess("file1", kubeClient),
		expectApplyAndReturnSuccess("file2", kubeClient),
		expectApplyAndReturnSuccess("file3", kubeClient),
	)
	successes := []ApplyAttempt{
		{"file1", "cmd file1", "output file1", ""},
		{"file2", "cmd file2", "output file2", ""},
		{"file3", "cmd file3", "output file3", ""},
	}
	tc = batchTestCase{kubeClient, applyList, successes, []ApplyAttempt{}}
	applyAndAssert(t, runCount, tc)
	runCount++

	// All files fail
	applyList = []string{"file1", "file2", "file3"}
	gomock.InOrder(
		expectCheckVersionAndReturnNil(kubeClient),
		expectApplyAndReturnFailure("file1", kubeClient),
		expectApplyAndReturnFailure("file2", kubeClient),
		expectApplyAndReturnFailure("file3", kubeClient),
	)
	failures := []ApplyAttempt{
		{"file1", "cmd file1", "output file1", "error file1"},
		{"file2", "cmd file2", "output file2", "error file2"},
		{"file3", "cmd file3", "output file3", "error file3"},
	}
	tc = batchTestCase{kubeClient, applyList, []ApplyAttempt{}, failures}
	applyAndAssert(t, runCount, tc)
	runCount++

	// Some successes, some failures
	applyList = []string{"file1", "file2", "file3", "file4"}
	gomock.InOrder(
		expectCheckVersionAndReturnNil(kubeClient),
		expectApplyAndReturnSuccess("file1", kubeClient),
		expectApplyAndReturnFailure("file2", kubeClient),
		expectApplyAndReturnSuccess("file3", kubeClient),
		expectApplyAndReturnFailure("file4", kubeClient),
	)
	successes = []ApplyAttempt{
		{"file1", "cmd file1", "output file1", ""},
		{"file3", "cmd file3", "output file3", ""},
	}
	failures = []ApplyAttempt{
		{"file2", "cmd file2", "output file2", "error file2"},
		{"file4", "cmd file4", "output file4", "error file4"},
	}
	tc = batchTestCase{kubeClient, applyList, successes, failures}
	applyAndAssert(t, runCount, tc)
	runCount++
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

func applyAndAssert(t *testing.T, runCount int, tc batchTestCase) {
	assert := assert.New(t)
	ba := BatchApplier{tc.kubeClient}
	successes, failures := ba.Apply(runCount, tc.applyList)
	assert.Equal(tc.expectedSuccesses, successes)
	assert.Equal(tc.expectedFailures, failures)
}
