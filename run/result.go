package run

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// Result stores the data from a single run of the apply loop.
// The functions associated with Result convert raw data into the desired formats for insertion into the status page template.
type Result struct {
	LastRun   Info
	RootPath  string
	Successes []ApplyAttempt
	Failures  []ApplyAttempt
}

// Info stores information about an apply run.
type Info struct {
	Start         time.Time
	Finish        time.Time
	CommitHash    string
	FullCommit    string
	DiffURLFormat string
	Type          Type
}

// FormattedStart returns the Start time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (i Info) FormattedStart() string {
	return i.Start.Truncate(time.Second).String()
}

// FormattedFinish returns the Finish time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (i Info) FormattedFinish() string {
	return i.Finish.Truncate(time.Second).String()
}

// Latency returns the latency for the run in seconds, truncated to 3 decimal places.
func (i Info) Latency() string {
	return fmt.Sprintf("%.3f sec", i.Finish.Sub(i.Start).Seconds())
}

// Finished returns true if the Result is from a finished apply run.
func (i Info) Finished() bool {
	return !i.Finish.IsZero()
}

// Equal checks whether the Info struct is equal to another.
func (i Info) Equal(info Info) bool {
	return reflect.DeepEqual(i, info)
}

// LastCommitLink returns a URL for the most recent commit if the envar $DIFF_URL_FORMAT is specified, otherwise it returns empty string.
func (i Info) LastCommitLink() string {
	if i.CommitHash == "" || i.DiffURLFormat == "" || !strings.Contains(i.DiffURLFormat, "%s") {
		return ""
	}
	return fmt.Sprintf(i.DiffURLFormat, i.CommitHash)
}

// TotalFiles returns the total count of apply attempts, both successes and failures.
func (r *Result) TotalFiles() int {
	return len(r.Successes) + len(r.Failures)
}

// Patch updates the Result's attributes from the provided Result.
func (r *Result) Patch(result Result) {
	r.LastRun = result.LastRun
	r.RootPath = result.RootPath
	updateApplyAttemptSlice(&r.Successes, &r.Failures, result.Successes)
	updateApplyAttemptSlice(&r.Failures, &r.Successes, result.Failures)
}

func updateApplyAttemptSlice(to, from *[]ApplyAttempt, r []ApplyAttempt) {
	for _, ra := range r {
		for i, ta := range *to {
			if ta.FilePath == ra.FilePath {
				for j := i; j < len(*to)-1; j++ {
					(*to)[j] = (*to)[j+1]
				}
				*to = (*to)[:len(*to)-1]
				break
			}
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
	sortApplyAttemptSlice(*to)
	*to = append(r, *to...)
}
