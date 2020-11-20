package webserver

import (
	"fmt"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
)

// Result stores the data from a single run of the apply loop.
// The functions associated with Result convert raw data into the desired formats for insertion into the status page template.
type Result struct {
	*sync.Mutex
	Applications  []kubeapplierv1alpha1.Application
	DiffURLFormat string
}

// Successes returns all the Applications that applied successfully.
func (r Result) Successes() []kubeapplierv1alpha1.Application {
	var ret []kubeapplierv1alpha1.Application
	r.Lock()
	defer r.Unlock()
	for _, app := range r.Applications {
		if app.Status.LastRun != nil && app.Status.LastRun.Success {
			ret = append(ret, app)
		}
	}
	return ret
}

// Failures returns all the Applications that failed applying.
func (r Result) Failures() []kubeapplierv1alpha1.Application {
	var ret []kubeapplierv1alpha1.Application
	r.Lock()
	defer r.Unlock()
	for _, app := range r.Applications {
		if app.Status.LastRun != nil && !app.Status.LastRun.Success {
			ret = append(ret, app)
		}
	}
	return ret
}

// FormattedTime returns the Time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (r Result) FormattedTime(t metav1.Time) string {
	return t.Time.Truncate(time.Second).String()
}

// Latency returns the latency between the two Times in seconds, truncated to 3
// decimal places.
func (r Result) Latency(t1, t2 metav1.Time) string {
	return fmt.Sprintf("%.3f sec", t2.Time.Sub(t1.Time).Seconds())
}

// CommitLink returns a URL for the commit most recently applied or it returns
// an empty string if it cannot construct the URL.
func (r Result) CommitLink(commit string) string {
	if commit == "" || r.DiffURLFormat == "" || !strings.Contains(r.DiffURLFormat, "%s") {
		return ""
	}
	return fmt.Sprintf(r.DiffURLFormat, commit)
}

// Finished returns true if the Result is from a finished apply run.
func (r Result) Finished() bool {
	r.Lock()
	defer r.Unlock()
	return len(r.Applications) > 0
}

// AppliedRecently checks whether the provided Application was applied in the
// last 15 minutes.
func (r Result) AppliedRecently(app kubeapplierv1alpha1.Application) bool {
	return app.Status.LastRun != nil &&
		time.Since(app.Status.LastRun.Started.Time) < time.Minute*15
}
