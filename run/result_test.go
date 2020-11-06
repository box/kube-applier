package run

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
)

type formattingTestCases struct {
	Start                   time.Time
	Finish                  time.Time
	ExpectedLatency         string
	ExpectedFormattedFinish string
	ExpectedFinished        bool
}

var formattingTestCasess = []formattingTestCases{
	// Unfinished
	{time.Time{}, time.Time{}, "0.000 sec", "0001-01-01 00:00:00 +0000 UTC", false},
	// Zero
	{time.Unix(0, 0).UTC(), time.Unix(0, 0).UTC(), "0.000 sec", "1970-01-01 00:00:00 +0000 UTC", true},
	// Integer
	{time.Unix(0, 0).UTC(), time.Unix(5, 0).UTC(), "5.000 sec", "1970-01-01 00:00:05 +0000 UTC", true},
	// Simple float
	{time.Unix(0, 0).UTC(), time.Unix(2, 500000000).UTC(), "2.500 sec", "1970-01-01 00:00:02 +0000 UTC", true},
	// Complex float - round down
	{time.Unix(0, 0).UTC(), time.Unix(2, 137454234).UTC(), "2.137 sec", "1970-01-01 00:00:02 +0000 UTC", true},
	// Complex float - round up
	{time.Unix(0, 0).UTC(), time.Unix(2, 137554234).UTC(), "2.138 sec", "1970-01-01 00:00:02 +0000 UTC", true},
}

func TestResultFormattedTime(t *testing.T) {
	assert := assert.New(t)
	for _, tc := range formattingTestCasess {
		r := Result{
			LastRun: kubeapplierv1alpha1.ApplicationStatusRunInfo{
				Started:  metav1.NewTime(tc.Start),
				Finished: metav1.NewTime(tc.Finish),
			},
		}
		assert.Equal(tc.ExpectedFormattedFinish, r.FormattedTime(r.LastRun.Finished))
	}
}

func TestResultLatency(t *testing.T) {
	assert := assert.New(t)
	for _, tc := range formattingTestCasess {
		r := Result{
			LastRun: kubeapplierv1alpha1.ApplicationStatusRunInfo{
				Started:  metav1.NewTime(tc.Start),
				Finished: metav1.NewTime(tc.Finish),
			},
		}
		assert.Equal(tc.ExpectedLatency, r.Latency(r.LastRun.Started, r.LastRun.Finished))
	}
}

func TestResultFinished(t *testing.T) {
	assert := assert.New(t)
	for _, tc := range formattingTestCasess {
		r := Result{
			LastRun: kubeapplierv1alpha1.ApplicationStatusRunInfo{
				Started:  metav1.NewTime(tc.Start),
				Finished: metav1.NewTime(tc.Finish),
			},
		}
		assert.Equal(tc.ExpectedFinished, r.Finished())
	}
}

type totalFilesTestCase struct {
	Applications []kubeapplierv1alpha1.Application
	Failures     []kubeapplierv1alpha1.Application
	Successes    []kubeapplierv1alpha1.Application
}

var totalFilesTestCases = []totalFilesTestCase{
	{nil, nil, nil},
	{
		[]kubeapplierv1alpha1.Application{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "app-a"},
				Status: kubeapplierv1alpha1.ApplicationStatus{
					LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
						Success: true,
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "app-b"},
				Status: kubeapplierv1alpha1.ApplicationStatus{
					LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
						Success: false,
					},
				},
			},
		},
		[]kubeapplierv1alpha1.Application{
			kubeapplierv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "app-b"},
				Status: kubeapplierv1alpha1.ApplicationStatus{
					LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
						Success: false,
					},
				},
			},
		},
		[]kubeapplierv1alpha1.Application{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "app-a"},
				Status: kubeapplierv1alpha1.ApplicationStatus{
					LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
						Success: true,
					},
				},
			},
		},
	},
}

func TestResultSuccessesAndFailures(t *testing.T) {
	assert := assert.New(t)
	for _, tc := range totalFilesTestCases {
		r := Result{Applications: tc.Applications}
		assert.Equal(tc.Successes, r.Successes())
		assert.Equal(tc.Failures, r.Failures())
	}
}

type lastCommitLinkTestCase struct {
	DiffURLFormat string
	CommitHash    string
	ExpectedLink  string
}

var lastCommitLinkTestCases = []lastCommitLinkTestCase{
	// All empty
	{"", "", ""},
	// Empty URL, non-empty hash
	{"", "hash", ""},
	// URL missing %s, empty hash
	{"https://badurl.com/", "", ""},
	// URL missing %s, non-empty hash
	{"https://badurl.com/", "hash", ""},
	// %s at end of URL, empty hash
	{"https://goodurl.com/%s/", "", ""},
	// %s at end of URL, non-empty hash
	{"https://goodurl.com/%s", "hash", "https://goodurl.com/hash"},
	// %s in middle of URL, empty hash
	{"https://goodurl.com/commit/%s/show", "", ""},
	// %s in middle of URL, non-empty hash
	{"https://goodurl.com/commit/%s/show", "hash", "https://goodurl.com/commit/hash/show"},
}

func TestResultLastCommitLink(t *testing.T) {
	assert := assert.New(t)
	for _, tc := range lastCommitLinkTestCases {
		r := Result{LastRun: kubeapplierv1alpha1.ApplicationStatusRunInfo{Commit: tc.CommitHash}, DiffURLFormat: tc.DiffURLFormat}
		assert.Equal(tc.ExpectedLink, r.CommitLink(r.LastRun.Commit))
	}
}

func TestResultAppliedDuringLastRun(t *testing.T) {
	assert := assert.New(t)

	runA := kubeapplierv1alpha1.ApplicationStatusRunInfo{Commit: "foo", Finished: metav1.Unix(2, 0), Started: metav1.Unix(1, 0), Type: "abc"}
	runB := kubeapplierv1alpha1.ApplicationStatusRunInfo{Commit: "bar", Finished: metav1.Unix(2, 0), Started: metav1.Unix(1, 0), Type: "abc"}
	runC := kubeapplierv1alpha1.ApplicationStatusRunInfo{Commit: "foo", Finished: metav1.Unix(2, 0), Started: metav1.Unix(1, 0), Type: "def"}
	runD := kubeapplierv1alpha1.ApplicationStatusRunInfo{Commit: "foo", Finished: metav1.Unix(3, 0), Started: metav1.Unix(1, 0), Type: "abc"}
	runE := kubeapplierv1alpha1.ApplicationStatusRunInfo{Commit: "foo", Finished: metav1.Unix(2, 0), Started: metav1.Unix(2, 0), Type: "abc"}
	runF := kubeapplierv1alpha1.ApplicationStatusRunInfo{Commit: "foo", Finished: metav1.Unix(2, 1), Started: metav1.Unix(1, 1), Type: "abc"}

	appA := kubeapplierv1alpha1.Application{Status: kubeapplierv1alpha1.ApplicationStatus{LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{Info: runA}}}
	appB := kubeapplierv1alpha1.Application{Status: kubeapplierv1alpha1.ApplicationStatus{LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{Info: runB}}}
	appC := kubeapplierv1alpha1.Application{Status: kubeapplierv1alpha1.ApplicationStatus{LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{Info: runC}}}
	appD := kubeapplierv1alpha1.Application{Status: kubeapplierv1alpha1.ApplicationStatus{LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{Info: runD}}}
	appE := kubeapplierv1alpha1.Application{Status: kubeapplierv1alpha1.ApplicationStatus{LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{Info: runE}}}
	appF := kubeapplierv1alpha1.Application{Status: kubeapplierv1alpha1.ApplicationStatus{LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{Info: runF}}}

	r := Result{LastRun: runA}

	testCases := []struct {
		App      kubeapplierv1alpha1.Application
		Expected bool
	}{
		{appA, true},
		{appB, false},
		{appC, false},
		{appD, false},
		{appE, false},
		{appF, true},
	}
	for _, tc := range testCases {
		assert.Equal(tc.Expected, r.AppliedDuringLastRun(tc.App))
	}
}
