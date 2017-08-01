package metrics

import (
	"fmt"
	"github.com/box/kube-applier/run"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

type testCase struct {
	successes        []run.ApplyAttempt
	failures         []run.ApplyAttempt
	runType          run.RunType
	expectedPatterns []string
}

// TestPrometheusProcessResult tests the processResult() function to ensure that the metrics page is updated properly.
// With each "test case", we construct a fake run.Result and call processResult.
// We then make a request to the metrics page handler and parse its raw output.
// We then use regexp patterns to check that the raw output has the expected state for each metric.
// Note that filenames are reused in order to ensure that the metrics update iteratively.
func TestPrometheusProcessResult(t *testing.T) {
	runMetrics := make(chan run.Result, 5)
	p := &Prometheus{RunMetrics: runMetrics}
	p.Configure()

	testCases := []testCase{
		// Case 1: No successes, no failures, full run
		{
			[]run.ApplyAttempt{},
			[]run.ApplyAttempt{},
			run.FullRun,
			[]string{
				// Expect count 1 for latency metric with run_type=fullRun, success=true
				makeLatencyPattern(run.FullRun, true, 1),
			},
		},
		// Case 2: Successes, no failures, full run
		{
			[]run.ApplyAttempt{{FilePath: "file1"}, {FilePath: "file2"}},
			[]run.ApplyAttempt{},
			run.FullRun,
			[]string{
				// Expect count 2 for latency metric with run_type=fullRun, success=true
				makeLatencyPattern(run.FullRun, true, 2),
				// Expect count 1 for file1 with success=true
				makeFilePattern("file1", true, 1),
				// Expect count 1 for file2 with success=true
				makeFilePattern("file2", true, 1),
			},
		},
		// Case 3: Successes, failures, full run
		{
			[]run.ApplyAttempt{{FilePath: "file1"}, {FilePath: "file3"}},
			[]run.ApplyAttempt{{FilePath: "file2"}},
			run.FullRun,
			[]string{
				// Expect count 1 for latency metric with run_type=fullRun, success=false
				makeLatencyPattern(run.FullRun, false, 1),
				// Expect count 2 for file1 with success=true
				makeFilePattern("file1", true, 2),
				// Expect count 1 for file3 with success=true
				makeFilePattern("file3", true, 1),
				// Expect count 1 for file2 with success=false
				makeFilePattern("file2", false, 1),

				// Ensure that previous metrics remain unchanged.
				// Expect count 2 for latency metric with run_type=fullRun, success=true
				makeLatencyPattern(run.FullRun, true, 2),
				// Expect count 1 for file2 with success=true
				makeFilePattern("file2", true, 1),
			},
		},
		// Case 4: Successes, failures, quick run
		{
			[]run.ApplyAttempt{{FilePath: "file1"}, {FilePath: "file3"}},
			[]run.ApplyAttempt{{FilePath: "file2"}},
			run.QuickRun,
			[]string{
				// Expect count 1 for latency metric with run_type=quickRun, success=false
				makeLatencyPattern(run.QuickRun, false, 1),
				// Expect count 3 for file1 with success=true
				makeFilePattern("file1", true, 3),
				// Expect count 2 for file3 with success=true
				makeFilePattern("file3", true, 2),
				// Expect count 2 for file2 with success=false
				makeFilePattern("file2", false, 2),

				// Ensure that previous metrics remain unchanged.
				// Expect count 2 for latency metric with run_type=fullRun, success=true
				makeLatencyPattern(run.FullRun, true, 2),
				// Expect count 1 for latency metric with run_type=fullRun, success=false
				makeLatencyPattern(run.FullRun, false, 1),
				// Expect count 1 for file2 with success=true
				makeFilePattern("file2", true, 1),
			},
		},
	}

	for _, tc := range testCases {
		processAndCheckOutput(t, p, tc)
	}
}

// Request content body from the handler.
func requestContentBody(handler http.Handler) string {
	req, _ := http.NewRequest("GET", "", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w.Body.String()
}

// Build a regex pattern for file_apply_count metric.
func makeFilePattern(filename string, success bool, count int) string {
	return fmt.Sprintf(
		"\\bfile_apply_count\\{file\\=\"%v\",success\\=\"%v\"\\} %v\\b",
		filename, success, count)
}

// Build a regex pattern for run_latency_seconds_count metric.
func makeLatencyPattern(runType run.RunType, success bool, count int) string {
	return fmt.Sprintf(
		"\\brun_latency_seconds_count\\{run_type\\=\"%v\",success\\=\"%v\"\\} %v\\b",
		runType, success, count)
}

// Process the test case and check that the metrics output contains the expected patterns.
func processAndCheckOutput(t *testing.T, p *Prometheus, tc testCase) {
	assert := assert.New(t)
	result := run.Result{Successes: tc.successes, Failures: tc.failures, RunType: tc.runType}
	p.processResult(result)
	metricsRaw := requestContentBody(p.GetHandler())
	for _, pattern := range tc.expectedPatterns {
		assert.True(regexp.MatchString(pattern, metricsRaw))
	}
}
