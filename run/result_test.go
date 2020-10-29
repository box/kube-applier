package run

/*
import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
		r := Result{LastRun: Info{Start: tc.Start, Finish: tc.Finish}}
		assert.Equal(tc.Expected, r.LastRun.Latency())
	}
}

type totalFilesTestCase struct {
	Successes []ApplyAttempt
	Failures  []ApplyAttempt
	Expected  int
}

var totalFilesTestCases = []totalFilesTestCase{
	// Both nil
	{nil, nil, 0},
	// One empty, one nil
	{[]ApplyAttempt{}, nil, 0},
	// Both empty
	{[]ApplyAttempt{}, []ApplyAttempt{}, 0},
	// Single apply attempt, other nil
	{[]ApplyAttempt{ApplyAttempt{}}, nil, 1},
	// Single apply attempt, other empty
	{[]ApplyAttempt{ApplyAttempt{}}, []ApplyAttempt{}, 1},
	// Both single apply attempt
	{[]ApplyAttempt{ApplyAttempt{}}, []ApplyAttempt{ApplyAttempt{}}, 2},
	// Both multiple apply attempts
	{
		[]ApplyAttempt{ApplyAttempt{}, ApplyAttempt{}, ApplyAttempt{}},
		[]ApplyAttempt{ApplyAttempt{}, ApplyAttempt{}},
		5,
	},
}

func TestResultTotalFiles(t *testing.T) {
	assert := assert.New(t)
	for _, tc := range totalFilesTestCases {
		r := Result{Successes: tc.Successes, Failures: tc.Failures}
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
		r := Result{LastRun: Info{CommitHash: tc.CommitHash, DiffURLFormat: tc.DiffURLFormat}}
		assert.Equal(tc.ExpectedLink, r.LastRun.LastCommitLink())
	}
}

func TestResultPatch(t *testing.T) {
	tNow := time.Now()
	tLater := tNow.Add(time.Second)
	infoA := Info{tNow, tNow, "foo", "bar", "", 0}
	infoB := Info{tLater, tLater, "foo", "bar", "", 0}
	testCases := []struct {
		a        Result
		b        Result
		expected Result
	}{
		{
			Result{},
			Result{infoA, "", nil, nil},
			Result{infoA, "", nil, nil},
		},
		{
			Result{infoA, "", []ApplyAttempt{{"/foo", "foo", "", "", infoA, tNow, tNow}}, nil},
			Result{infoB, "", nil, nil},
			Result{infoB, "", []ApplyAttempt{{"/foo", "foo", "", "", infoA, tNow, tNow}}, nil},
		},
		{
			Result{infoA, "", []ApplyAttempt{{"/foo", "foo", "", "", infoA, tNow, tNow}}, nil},
			Result{infoB, "", []ApplyAttempt{{"/foo", "bar", "", "", infoB, tNow, tNow}}, nil},
			Result{infoB, "", []ApplyAttempt{{"/foo", "bar", "", "", infoB, tNow, tNow}}, nil},
		},
		{
			Result{infoA, "", []ApplyAttempt{{"/foo", "foo", "", "", infoA, tNow, tNow}}, nil},
			Result{infoB, "", []ApplyAttempt{{"/bar", "bar", "", "", infoB, tNow, tNow}}, nil},
			Result{infoB, "", []ApplyAttempt{{"/bar", "bar", "", "", infoB, tNow, tNow}, {"/foo", "foo", "", "", infoA, tNow, tNow}}, nil},
		},
		{
			Result{infoA, "", []ApplyAttempt{{"/foo", "foo", "", "", infoA, tNow, tNow}}, []ApplyAttempt{{"/bar", "bar", "", "", infoA, tNow, tNow}}},
			Result{infoB, "", []ApplyAttempt{{"/bar", "bar", "", "", infoB, tNow, tNow}}, nil},
			Result{infoB, "", []ApplyAttempt{{"/bar", "bar", "", "", infoB, tNow, tNow}, {"/foo", "foo", "", "", infoA, tNow, tNow}}, nil},
		},
		{
			Result{infoA, "", []ApplyAttempt{{"/0", "", "", "", infoA, tNow, tNow}, {"/1", "", "", "", infoA, tNow, tNow}}, []ApplyAttempt{{"/2", "", "", "", infoA, tNow, tNow}, {"/3", "", "", "", infoA, tNow, tNow}}},
			Result{infoB, "", []ApplyAttempt{{"/2", "", "", "", infoB, tNow, tNow}}, []ApplyAttempt{{"/3", "", "", "", infoB, tNow, tNow}}},
			Result{infoB, "", []ApplyAttempt{{"/2", "", "", "", infoB, tNow, tNow}, {"/0", "", "", "", infoA, tNow, tNow}, {"/1", "", "", "", infoA, tNow, tNow}}, []ApplyAttempt{{"/3", "", "", "", infoB, tNow, tNow}}},
		},
		{
			Result{infoA, "", []ApplyAttempt{{"/0", "", "", "", infoA, tNow, tNow}, {"/1", "", "", "", infoA, tNow, tNow}, {"/2", "", "", "", infoA, tNow, tNow}}, []ApplyAttempt{{"/3", "", "", "", infoA, tNow, tNow}, {"/4", "", "", "", infoA, tNow, tNow}, {"/5", "", "", "", infoA, tNow, tNow}}},
			Result{infoB, "", []ApplyAttempt{{"/4", "", "", "", infoA, tNow, tNow}}, []ApplyAttempt{{"/1", "", "", "", infoA, tNow, tNow}}},
			Result{infoB, "", []ApplyAttempt{{"/4", "", "", "", infoA, tNow, tNow}, {"/0", "", "", "", infoA, tNow, tNow}, {"/2", "", "", "", infoA, tNow, tNow}}, []ApplyAttempt{{"/1", "", "", "", infoA, tNow, tNow}, {"/3", "", "", "", infoA, tNow, tNow}, {"/5", "", "", "", infoA, tNow, tNow}}},
		},
		{
			Result{infoA, "", []ApplyAttempt{{"/4", "", "", "", infoA, tNow, tNow}, {"/0", "", "", "", infoA, tNow, tNow}, {"/2", "", "", "", infoA, tNow, tNow}}, []ApplyAttempt{{"/1", "", "", "", infoA, tNow, tNow}, {"/5", "", "", "", infoA, tNow, tNow}, {"/3", "", "", "", infoA, tNow, tNow}}},
			Result{infoB, "", []ApplyAttempt{{"/1", "", "", "", infoA, tNow, tNow}}, []ApplyAttempt{{"/0", "", "", "", infoA, tNow, tNow}}},
			Result{infoB, "", []ApplyAttempt{{"/1", "", "", "", infoA, tNow, tNow}, {"/2", "", "", "", infoA, tNow, tNow}, {"/4", "", "", "", infoA, tNow, tNow}}, []ApplyAttempt{{"/0", "", "", "", infoA, tNow, tNow}, {"/3", "", "", "", infoA, tNow, tNow}, {"/5", "", "", "", infoA, tNow, tNow}}},
		},
	}
	assert := assert.New(t)

	for _, tc := range testCases {
		tc.a.Patch(tc.b)
		assert.Equal(tc.expected, tc.a)
	}
}
*/
