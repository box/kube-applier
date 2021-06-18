// Package metrics contains global structures for capturing kube-applier
// metrics. The following metrics are implemented:
//
//   - kube_applier_git_last_sync_timestamp
//   - kube_applier_git_sync_count{"success"}
//   - kube_applier_kubectl_exit_code_count{"namespace", "exit_code"}
//   - kube_applier_namespace_apply_count{"namespace"}
//   - kube_applier_run_latency_seconds{"namespace", "success"}
//   - kube_applier_result_summary{"namespace", "type", "name", "action"}
//   - kube_applier_last_run_timestamp_seconds{"namespace"}
//   - kube_applier_run_queue{"namespace", "type"}
//   - kube_applier_run_queue_failures{"namespace", "type"}
//   - kube_applier_waybill_spec_auto_apply{"namespace"}
//   - kube_applier_waybill_spec_dry_run{"namespace"}
//   - kube_applier_waybill_spec_run_interval{"namespace"}
package metrics

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/utils/pointer"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/log"
)

const (
	metricsNamespace = "kube_applier"
)

var (
	// Used to parse kubectl output
	kubectlOutputPattern = regexp.MustCompile(`([\w.\-]+)\/([\w.\-:]+) ([\w-]+).*`)

	// gitLastSyncTimestamp is a Gauge that captures the timestamp of the last
	// successful git sync
	gitLastSyncTimestamp prometheus.Gauge
	// gitSyncCount is a Counter vector of git sync operations
	gitSyncCount *prometheus.CounterVec
	// kubectlExitCodeCount is a Counter vector of run exit codes
	kubectlExitCodeCount *prometheus.CounterVec
	// namespaceApplyCount is a Counter vector of runs success status
	namespaceApplyCount *prometheus.CounterVec
	// runLatency is a Histogram vector that keeps track of run durations
	runLatency *prometheus.HistogramVec
	// resultSummary is a Gauge vector that captures information about objects
	// applied during runs
	resultSummary *prometheus.GaugeVec
	// lastRunSuccess is a Gauge vector of whether the last run was
	// successful or not
	lastRunSuccess *prometheus.GaugeVec
	// lastRunTimestamp is a Gauge vector of the last run timestamp
	lastRunTimestamp *prometheus.GaugeVec
	// runQueue is a Gauge vector of active run requests
	runQueue *prometheus.GaugeVec
	// runQueueFailures is a Counter vector of failed queue attempts
	runQueueFailures *prometheus.CounterVec
	// waybillSpecAutoApply is a Gauge vector that captures a Waybill's
	// autoApply attribute
	waybillSpecAutoApply *prometheus.GaugeVec
	// waybillSpecDryRun is a Gauge vector that captures a Waybill's dryRun
	// attribute
	waybillSpecDryRun *prometheus.GaugeVec
	// waybillSpecRunInterval is a Gauge vector that captures a Waybill's
	// runInterval attribute
	waybillSpecRunInterval *prometheus.GaugeVec
)

func init() {
	gitLastSyncTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "git_last_sync_timestamp",
		Help:      "Timestamp of the last successful git sync",
	})
	gitSyncCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "git_sync_count",
		Help:      "Count of git sync operations",
	},
		[]string{
			// Whether the apply was successful or not
			"success",
		},
	)
	kubectlExitCodeCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "kubectl_exit_code_count",
		Help:      "Count of kubectl exit codes",
	},
		[]string{
			// Namespace of the Waybill applied
			"namespace",
			// Exit code
			"exit_code",
		},
	)
	namespaceApplyCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "namespace_apply_count",
		Help:      "Success metric for every namespace applied",
	},
		[]string{
			// Namespace of the Waybill applied
			"namespace",
			// Whether the apply was successful or not
			"success",
		},
	)
	runLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Name:      "run_latency_seconds",
		Help:      "Latency for completed apply runs",
		Buckets:   []float64{1, 5, 10, 30, 60, 90, 120, 150, 300, 600},
	},
		[]string{
			// Namespace of the Waybill applied
			"namespace",
			// Whether the apply was successful or not
			"success",
			// Whether the apply was a dry run
			"dryrun",
		},
	)
	resultSummary = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "result_summary",
		Help:      "Result summary for every manifest",
	},
		[]string{
			// The object namespace
			"namespace",
			// The object type
			"type",
			// The object name
			"name",
			// The applied action
			"action",
		},
	)
	lastRunSuccess = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "last_run_success",
		Help:      "Was the last run for this namespace successful?",
	},
		[]string{
			// Namespace of the Waybill applied
			"namespace",
		},
	)
	lastRunTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "last_run_timestamp_seconds",
		Help:      "Timestamp of the last completed apply run",
	},
		[]string{
			// Namespace of the Waybill applied
			"namespace",
		},
	)
	runQueue = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "run_queue",
		Help:      "Number of run requests currently queued",
	},
		[]string{
			// Namespace of the Waybill applied
			"namespace",
			// Type of the run requested
			"type",
		},
	)
	runQueueFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "run_queue_failures",
		Help:      "Number of run requests queue failures",
	},
		[]string{
			// Namespace of the Waybill applied
			"namespace",
			// Type of the run requested
			"type",
		},
	)
	waybillSpecAutoApply = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: "waybill_spec",
		Name:      "auto_apply",
		Help:      "The value of auto apply in the Waybill spec",
	},
		[]string{
			// Namespace of the Waybill
			"namespace",
		},
	)
	waybillSpecDryRun = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: "waybill_spec",
		Name:      "dry_run",
		Help:      "The value of dryRun in the Waybill spec",
	},
		[]string{
			// Namespace of the Waybill
			"namespace",
		},
	)
	waybillSpecRunInterval = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: "waybill_spec",
		Name:      "run_interval",
		Help:      "The value of runInterval in the Waybill spec",
	},
		[]string{
			// Namespace of the Waybill
			"namespace",
		},
	)
}

// AddRunRequestQueueFailure increments the counter of failed queue attempts
func AddRunRequestQueueFailure(t string, waybill *kubeapplierv1alpha1.Waybill) {
	runQueueFailures.With(prometheus.Labels{
		"namespace": waybill.Namespace,
		"type":      t,
	}).Inc()
}

// ReconcileFromWaybillList ensures that the last_run_success, last_run_timestamp
// and waybill_spec metrics correctly represent the state in the cluster
func ReconcileFromWaybillList(waybills []kubeapplierv1alpha1.Waybill) {
	lastRunSuccess.Reset()
	lastRunTimestamp.Reset()
	waybillSpecAutoApply.Reset()
	waybillSpecDryRun.Reset()
	waybillSpecRunInterval.Reset()
	for _, wb := range waybills {
		var autoApply, dryRun float64
		if pointer.BoolPtrDerefOr(wb.Spec.AutoApply, true) {
			autoApply = 1
		}
		if wb.Spec.DryRun {
			dryRun = 1
		}
		waybillSpecAutoApply.With(prometheus.Labels{
			"namespace": wb.Namespace,
		}).Set(autoApply)
		waybillSpecDryRun.With(prometheus.Labels{
			"namespace": wb.Namespace,
		}).Set(dryRun)
		waybillSpecRunInterval.With(prometheus.Labels{
			"namespace": wb.Namespace,
		}).Set(float64(wb.Spec.RunInterval))
		if wb.Status.LastRun == nil {
			continue
		}
		setLastRunSuccess(wb.Namespace, wb.Status.LastRun.Success)
		lastRunTimestamp.With(prometheus.Labels{
			"namespace": wb.Namespace,
		}).Set(float64(wb.Status.LastRun.Finished.Unix()))
		// Initialise the following vectors, if they don't exist. By simply
		// fetching an instance of a Counter from the CounterVec, it is being
		// initialised, if it doesn't exist. This ensures that counters start
		// with a zero value if they have not been updated before.
		for _, b := range []string{"true", "false"} {
			namespaceApplyCount.With(prometheus.Labels{"namespace": wb.Namespace, "success": b})
			runLatency.With(prometheus.Labels{"namespace": wb.Namespace, "success": b, "dryrun": "true"})
			runLatency.With(prometheus.Labels{"namespace": wb.Namespace, "success": b, "dryrun": "false"})
		}
		kubectlExitCodeCount.With(prometheus.Labels{"namespace": wb.Namespace, "exit_code": "0"})
		kubectlExitCodeCount.With(prometheus.Labels{"namespace": wb.Namespace, "exit_code": "1"})
	}
}

// RecordGitSync records a git repository sync attempt by updating all the
// relevant metrics
func RecordGitSync(success bool) {
	if success {
		gitLastSyncTimestamp.Set(float64(time.Now().Unix()))
	}
	gitSyncCount.With(prometheus.Labels{
		"success": strconv.FormatBool(success),
	}).Inc()
}

// UpdateFromLastRun takes information from a Waybill's LastRun status and
// updates all the relevant metrics
func UpdateFromLastRun(waybill *kubeapplierv1alpha1.Waybill) {
	success := strconv.FormatBool(waybill.Status.LastRun.Success)
	dryRun := strconv.FormatBool(waybill.Spec.DryRun)
	namespaceApplyCount.With(prometheus.Labels{
		"namespace": waybill.Namespace,
		"success":   success,
	}).Inc()
	runLatency.With(prometheus.Labels{
		"namespace": waybill.Namespace,
		"success":   success,
		"dryrun":    dryRun,
	}).Observe(waybill.Status.LastRun.Finished.Sub(waybill.Status.LastRun.Started.Time).Seconds())
	lastRunTimestamp.With(prometheus.Labels{
		"namespace": waybill.Namespace,
	}).Set(float64(waybill.Status.LastRun.Finished.Unix()))
	setLastRunSuccess(waybill.Namespace, waybill.Status.LastRun.Success)
}

// UpdateKubectlExitCodeCount increments for each exit code returned by kubectl
func UpdateKubectlExitCodeCount(namespace string, code int) {
	kubectlExitCodeCount.With(prometheus.Labels{
		"namespace": namespace,
		"exit_code": strconv.Itoa(code),
	}).Inc()
}

// UpdateResultSummary sets gauges for resources applied during the last run of
// each Waybill
func UpdateResultSummary(waybills []kubeapplierv1alpha1.Waybill) {
	resultSummary.Reset()

	for _, wb := range waybills {
		if wb.Status.LastRun == nil {
			continue
		}
		res := parseKubectlOutput(wb.Status.LastRun.Output)
		for _, r := range res {
			resultSummary.With(prometheus.Labels{
				"namespace": wb.Namespace,
				"type":      r.Type,
				"name":      r.Name,
				"action":    r.Action,
			}).Set(1)
		}
	}
}

// UpdateRunRequest modifies the gauge of currently queued run requests
func UpdateRunRequest(t string, waybill *kubeapplierv1alpha1.Waybill, diff float64) {
	runQueue.With(prometheus.Labels{
		"namespace": waybill.Namespace,
		"type":      t,
	}).Add(diff)
}

// Reset deletes all metrics. This is exported for use in integration tests.
func Reset() {
	gitSyncCount.Reset()
	kubectlExitCodeCount.Reset()
	namespaceApplyCount.Reset()
	runLatency.Reset()
	resultSummary.Reset()
	lastRunSuccess.Reset()
	lastRunTimestamp.Reset()
	runQueue.Reset()
	runQueueFailures.Reset()
	waybillSpecAutoApply.Reset()
	waybillSpecDryRun.Reset()
	waybillSpecRunInterval.Reset()
}

func setLastRunSuccess(namespace string, success bool) {
	lrs := float64(0)
	if success {
		lrs = 1
	}
	lastRunSuccess.With(prometheus.Labels{
		"namespace": namespace,
	}).Set(lrs)
}

type applyObjectResult struct {
	Type, Name, Action string
}

func parseKubectlOutput(output string) []applyObjectResult {
	output = strings.TrimSpace(output)
	lines := strings.Split(output, "\n")
	var results []applyObjectResult
	for _, line := range lines {
		m := kubectlOutputPattern.FindAllStringSubmatch(line, -1)
		// Should be only 1 match, and should contain 4 elements (0: whole match, 1: resource-type, 2: name, 3: action
		if len(m) != 1 || len(m[0]) != 4 {
			log.Logger("metrics").Debug("Could not parse output, expected format: <resource-type>/<name> <action>", "line", line, "full output", output)
			continue
		}
		results = append(results, applyObjectResult{
			Type:   m[0][1],
			Name:   m[0][2],
			Action: m[0][3],
		})
	}
	return results
}
