// Package metrics contains global structures for capturing kube-applier
// metrics. The following metrics are implemented:
//
//   - kube_applier_kubectl_exit_code_count{"namespace", "exit_code"}
//   - kube_applier_namespace_apply_count{"namespace"}
//   - kube_applier_run_latency_seconds{"namespace", "success"}
//   - kube_applier_result_summary{"namespace", "type", "name", "action"}
//   - kube_applier_last_run_timestamp_seconds{"namespace"}
//   - kube_applier_run_queue{"namespace", "type"}
//   - kube_applier_run_queue_failures{"namespace", "type"}
//   - kube_applier_application_spec_dry_run{"namespace"}
//   - kube_applier_application_spec_run_interval{"namespace"}
package metrics

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/log"
)

const (
	metricsNamespace = "kube_applier"
)

var (
	// Used to parse kubectl output
	kubectlOutputPattern = regexp.MustCompile(`([\w.\-]+)\/([\w.\-:]+) ([\w-]+).*`)

	// kubectlExitCodeCount is a Counter vector of run exit codes
	kubectlExitCodeCount *prometheus.CounterVec
	// namespaceApplyCount is a Counter vector of runs success status
	namespaceApplyCount *prometheus.CounterVec
	// runLatency is a Histogram vector that keeps track of run durations
	runLatency *prometheus.HistogramVec
	// resultSummary is a Gauge vector that captures information about objects
	// applied during runs
	resultSummary *prometheus.GaugeVec
	// lastRunTimestamp is a Gauge vector of the last run timestamp
	lastRunTimestamp *prometheus.GaugeVec
	// runQueue is a Gauge vector of active run requests
	runQueue *prometheus.GaugeVec
	// runQueueFailures is a Counter vector of failed queue attempts
	runQueueFailures *prometheus.CounterVec
	// applicationSpecDryRun is a Gauge vector that captures an Application's
	// dryRun attribute
	applicationSpecDryRun *prometheus.GaugeVec
	// applicationSpecRunInterval is a Gauge vector that captures an
	// Application's runInterval attribute
	applicationSpecRunInterval *prometheus.GaugeVec
)

func init() {
	kubectlExitCodeCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "kubectl_exit_code_count",
		Help:      "Count of kubectl exit codes",
	},
		[]string{
			// Namespace of the Application applied
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
			// Namespace of the Application applied
			"namespace",
			// Whether the apply was successful or not
			"success",
		},
	)
	runLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Name:      "run_latency_seconds",
		Help:      "Latency for completed apply runs",
	},
		[]string{
			// Namespace of the Application applied
			"namespace",
			// Whether the apply was successful or not
			"success",
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
	lastRunTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "last_run_timestamp_seconds",
		Help:      "Timestamp of the last completed apply run",
	},
		[]string{
			// Namespace of the Application applied
			"namespace",
		},
	)
	runQueue = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "run_queue",
		Help:      "Number of run requests currently queued",
	},
		[]string{
			// Namespace of the Application applied
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
			// Namespace of the Application applied
			"namespace",
			// Type of the run requested
			"type",
		},
	)
	applicationSpecDryRun = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: "application_spec",
		Name:      "dry_run",
		Help:      "The value of dryRun in the Application spec",
	},
		[]string{
			// Namespace of the Application
			"namespace",
		},
	)
	applicationSpecRunInterval = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: "application_spec",
		Name:      "run_interval",
		Help:      "The value of runInterval in the Application spec",
	},
		[]string{
			// Namespace of the Application
			"namespace",
		},
	)
}

// AddRunRequestQueueFailure increments the counter of failed queue attempts
func AddRunRequestQueueFailure(t string, app *kubeapplierv1alpha1.Application) {
	runQueueFailures.With(prometheus.Labels{
		"namespace": app.Namespace,
		"type":      t,
	}).Inc()
}

// ReconcileFromApplicationList ensures that the last_run_timestamp and
// application_spec metrics correctly represent the state in the cluster
func ReconcileFromApplicationList(apps []kubeapplierv1alpha1.Application) {
	lastRunTimestamp.Reset()
	applicationSpecDryRun.Reset()
	applicationSpecRunInterval.Reset()
	for _, app := range apps {
		var dryRun float64
		if app.Spec.DryRun {
			dryRun = 1
		}
		applicationSpecDryRun.With(prometheus.Labels{
			"namespace": app.Namespace,
		}).Set(dryRun)
		applicationSpecRunInterval.With(prometheus.Labels{
			"namespace": app.Namespace,
		}).Set(float64(app.Spec.RunInterval))
		if app.Status.LastRun == nil {
			continue
		}
		lastRunTimestamp.With(prometheus.Labels{
			"namespace": app.Namespace,
		}).Set(float64(app.Status.LastRun.Finished.Unix()))
	}
}

// UpdateFromLastRun takes information from an Application's LastRun status and
// updates all the relevant metrics
func UpdateFromLastRun(app *kubeapplierv1alpha1.Application) {
	success := strconv.FormatBool(app.Status.LastRun.Success)
	namespaceApplyCount.With(prometheus.Labels{
		"namespace": app.Namespace,
		"success":   success,
	}).Inc()
	runLatency.With(prometheus.Labels{
		"namespace": app.Namespace,
		"success":   success,
	}).Observe(app.Status.LastRun.Finished.Sub(app.Status.LastRun.Started.Time).Seconds())
	lastRunTimestamp.With(prometheus.Labels{
		"namespace": app.Namespace,
	}).Set(float64(app.Status.LastRun.Finished.Unix()))
}

// UpdateKubectlExitCodeCount increments for each exit code returned by kubectl
func UpdateKubectlExitCodeCount(namespace string, code int) {
	kubectlExitCodeCount.With(prometheus.Labels{
		"namespace": namespace,
		"exit_code": strconv.Itoa(code),
	}).Inc()
}

// UpdateResultSummary sets gauges for resources applied during the last run of
// each Application
func UpdateResultSummary(apps []kubeapplierv1alpha1.Application) {
	resultSummary.Reset()

	for _, app := range apps {
		if app.Status.LastRun == nil {
			continue
		}
		res := parseKubectlOutput(app.Status.LastRun.Output)
		for _, r := range res {
			resultSummary.With(prometheus.Labels{
				"namespace": app.Namespace,
				"type":      r.Type,
				"name":      r.Name,
				"action":    r.Action,
			}).Set(1)
		}
	}
}

// UpdateRunRequest modifies the gauge of currently queued run requests
func UpdateRunRequest(t string, app *kubeapplierv1alpha1.Application, diff float64) {
	runQueue.With(prometheus.Labels{
		"namespace": app.Namespace,
		"type":      t,
	}).Add(diff)
}

// Reset deletes all metrics. This is exported for use in integration tests.
func Reset() {
	kubectlExitCodeCount.Reset()
	namespaceApplyCount.Reset()
	runLatency.Reset()
	resultSummary.Reset()
	lastRunTimestamp.Reset()
	runQueue.Reset()
	runQueueFailures.Reset()
	applicationSpecDryRun.Reset()
	applicationSpecRunInterval.Reset()
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
			log.Logger.Warn("Expected format: <resource-type>/<name> <action>", "line", line, "full output", output)
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
