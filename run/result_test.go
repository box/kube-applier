package run_test

import (
	"github.com/box/kube-applier/run"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type latencyTestCase struct {
	Start    time.Time
	Finish   time.Time
	Expected string
}

var latencyTestCases = []latencyTestCase{
	// Zero
	{time.Unix(0, 0), time.Unix(0, 0), "0.000 sec"},
	// Integer
	{time.Unix(0, 0), time.Unix(5, 0), "5.000 sec"},
	// Simple float
	{time.Unix(0, 0), time.Unix(2, 500000000), "2.500 sec"},
	// Complex float - round down
	{time.Unix(0, 0), time.Unix(2, 137454234), "2.137 sec"},
	// Complex float - round up
	{time.Unix(0, 0), time.Unix(2, 137554234), "2.138 sec"},
}

func TestResultLatency(t *testing.T) {
	assert := assert.New(t)
	for _, tc := range latencyTestCases {
		r := run.Result{Start: tc.Start, Finish: tc.Finish}
		assert.Equal(tc.Expected, r.Latency())
	}
}

type totalFilesTestCase struct {
	Successes []run.ApplyAttempt
	Failures  []run.ApplyAttempt
	Expected  int
}

var totalFilesTestCases = []totalFilesTestCase{
	// Both nil
	{nil, nil, 0},
	// One empty, one nil
	{[]run.ApplyAttempt{}, nil, 0},
	// Both empty
	{[]run.ApplyAttempt{}, []run.ApplyAttempt{}, 0},
	// Single apply attempt, other nil
	{[]run.ApplyAttempt{run.ApplyAttempt{}}, nil, 1},
	// Single apply attempt, other empty
	{[]run.ApplyAttempt{run.ApplyAttempt{}}, []run.ApplyAttempt{}, 1},
	// Both single apply attempt
	{[]run.ApplyAttempt{run.ApplyAttempt{}}, []run.ApplyAttempt{run.ApplyAttempt{}}, 2},
	// Both multiple apply attempts
	{
		[]run.ApplyAttempt{run.ApplyAttempt{}, run.ApplyAttempt{}, run.ApplyAttempt{}},
		[]run.ApplyAttempt{run.ApplyAttempt{}, run.ApplyAttempt{}},
		5,
	},
}

func TestResultTotalFiles(t *testing.T) {
	assert := assert.New(t)
	for _, tc := range totalFilesTestCases {
		r := run.Result{Successes: tc.Successes, Failures: tc.Failures}
		assert.Equal(tc.Expected, r.TotalFiles())
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
		r := run.Result{DiffURLFormat: tc.DiffURLFormat, CommitHash: tc.CommitHash}
		assert.Equal(tc.ExpectedLink, r.LastCommitLink())
	}
}
