package webserver

import (
	"fmt"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
)

// Result stores the data from a single run of the apply loop.
// The functions associated with Result convert raw data into the desired formats for insertion into the status page template.
type Result struct {
	*sync.Mutex
	Waybills      []kubeapplierv1alpha1.Waybill
	DiffURLFormat string
}

// Successes returns all the Waybills that applied successfully.
func (r *Result) Successes() []kubeapplierv1alpha1.Waybill {
	var ret []kubeapplierv1alpha1.Waybill
	r.Lock()
	defer r.Unlock()
	for _, wb := range r.Waybills {
		if wb.Status.LastRun != nil && wb.Status.LastRun.Success {
			ret = append(ret, wb)
		}
	}
	return ret
}

// Failures returns all the Waybills that failed applying.
func (r *Result) Failures() []kubeapplierv1alpha1.Waybill {
	var ret []kubeapplierv1alpha1.Waybill
	r.Lock()
	defer r.Unlock()
	for _, wb := range r.Waybills {
		if wb.Status.LastRun != nil && !wb.Status.LastRun.Success {
			ret = append(ret, wb)
		}
	}
	return ret
}

// FormattedTime returns the Time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (r *Result) FormattedTime(t metav1.Time) string {
	return t.Time.Truncate(time.Second).String()
}

// Latency returns the latency between the two Times in seconds.
func (r *Result) Latency(t1, t2 metav1.Time) string {
	return fmt.Sprintf("%.0f sec", t2.Time.Sub(t1.Time).Seconds())
}

// CommitLink returns a URL for the commit most recently applied or it returns
// an empty string if it cannot construct the URL.
func (r *Result) CommitLink(commit string) string {
	if commit == "" || r.DiffURLFormat == "" || !strings.Contains(r.DiffURLFormat, "%s") {
		return ""
	}
	return fmt.Sprintf(r.DiffURLFormat, commit)
}

// Finished returns true if the Result is from a finished apply run.
func (r *Result) Finished() bool {
	r.Lock()
	defer r.Unlock()
	return len(r.Waybills) > 0
}

// Status returns a human-readable string that describes the Waybill in terms
// of its autoApply and dryRun attributes.
func (r *Result) Status(wb *kubeapplierv1alpha1.Waybill) string {
	ret := []string{}
	if !pointer.BoolPtrDerefOr(wb.Spec.AutoApply, true) {
		ret = append(ret, "auto-apply disabled")
	}
	if wb.Spec.DryRun {
		ret = append(ret, "dry-run")
	}
	if len(ret) == 0 {
		return ""
	}
	return fmt.Sprintf("(%s)", strings.Join(ret, ", "))
}

// AppliedRecently checks whether the provided Waybill was applied in the last
// 15 minutes.
func (r *Result) AppliedRecently(waybill kubeapplierv1alpha1.Waybill) bool {
	return waybill.Status.LastRun != nil &&
		time.Since(waybill.Status.LastRun.Started.Time) < time.Minute*15
}
