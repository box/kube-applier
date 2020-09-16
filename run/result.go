package run

import (
	"fmt"
	"strings"
	"time"
)

// Result stores the data from a single run of the apply loop.
// The functions associated with Result convert raw data into the desired formats for insertion into the status page template.
type Result struct {
	Start         time.Time
	Finish        time.Time
	CommitHash    string
	FullCommit    string
	Successes     []ApplyAttempt
	Failures      []ApplyAttempt
	DiffURLFormat string
	Type          Type
}

// FormattedStart returns the Start time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (r *Result) FormattedStart() string {
	return r.Start.Truncate(time.Second).String()
}

// FormattedFinish returns the Finish time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (r *Result) FormattedFinish() string {
	return r.Finish.Truncate(time.Second).String()
}

// Latency returns the latency for the run in seconds, truncated to 3 decimal places.
func (r *Result) Latency() string {
	return fmt.Sprintf("%.3f sec", r.Finish.Sub(r.Start).Seconds())
}

// TotalFiles returns the total count of apply attempts, both successes and failures.
func (r *Result) TotalFiles() int {
	return len(r.Successes) + len(r.Failures)
}

// Finished returns true if the Result is from a finished apply run.
func (r *Result) Finished() bool {
	return !r.Finish.IsZero()
}

// LastCommitLink returns a URL for the most recent commit if the envar $DIFF_URL_FORMAT is specified, otherwise it returns empty string.
func (r *Result) LastCommitLink() string {
	if r.CommitHash == "" || r.DiffURLFormat == "" || !strings.Contains(r.DiffURLFormat, "%s") {
		return ""
	}
	return fmt.Sprintf(r.DiffURLFormat, r.CommitHash)
}

// Patch updates the Result's attributes from the provided Result.
func (r *Result) Patch(result Result) {
	r.Start = result.Start
	r.Finish = result.Finish
	r.CommitHash = result.CommitHash
	r.FullCommit = result.FullCommit
	r.DiffURLFormat = result.DiffURLFormat
	r.Type = result.Type
	updateApplyAttemptSlice(&r.Successes, &r.Failures, result.Successes)
	updateApplyAttemptSlice(&r.Failures, &r.Successes, result.Failures)
}

func updateApplyAttemptSlice(to, from *[]ApplyAttempt, r []ApplyAttempt) {
	toAppend := []ApplyAttempt{}
	for _, ra := range r {
		found := false
		for i, ta := range *to {
			if ta.FilePath == ra.FilePath {
				(*to)[i] = ra
				found = true
				break
			}
		}
		if !found {
			toAppend = append(toAppend, ra)
		}
		for i, fa := range *from {
			if fa.FilePath == ra.FilePath {
				for j := i; j < len(*from)-1; j++ {
					(*from)[j] = (*from)[j+1]
				}
				*from = (*from)[:len(*from)-1]
				break
			}
		}
	}
	*to = append(*to, toAppend...)
}
