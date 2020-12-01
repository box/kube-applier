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
	kubectlOutputPattern = regexp.MustCompile(`([\w.\-]+)\/([\w.\-:]+) ([\w-]+).*`)
)

// Prometheus implements instrumentation of metrics for kube-applier
type Prometheus struct {
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
}

// Init creates and registers the custom metrics for kube-applier.
func (p *Prometheus) Init() {
	p.kubectlExitCodeCount = promauto.NewCounterVec(prometheus.CounterOpts{
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
	p.namespaceApplyCount = promauto.NewCounterVec(prometheus.CounterOpts{
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
	p.runLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
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
	p.resultSummary = promauto.NewGaugeVec(prometheus.GaugeOpts{
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
	p.lastRunTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "last_run_timestamp_seconds",
		Help:      "Timestamp of the last completed apply run",
	},
		[]string{
			// Namespace of the Application applied
			"namespace",
		},
	)
}

// UpdateFromLastRun takes information from an Application's LastRun status and
// updates all the relevant metrics
func (p *Prometheus) UpdateFromLastRun(app *kubeapplierv1alpha1.Application) {
	success := strconv.FormatBool(app.Status.LastRun.Success)
	p.namespaceApplyCount.With(prometheus.Labels{
		"namespace": app.Namespace,
		"success":   success,
	}).Inc()
	p.runLatency.With(prometheus.Labels{
		"namespace": app.Namespace,
		"success":   success,
	}).Observe(app.Status.LastRun.Finished.Sub(app.Status.LastRun.Started.Time).Seconds())
	p.lastRunTimestamp.With(prometheus.Labels{
		"namespace": app.Namespace,
	}).Set(float64(app.Status.LastRun.Finished.Unix()))
}

// UpdateKubectlExitCodeCount increments for each exit code returned by kubectl
func (p *Prometheus) UpdateKubectlExitCodeCount(namespace string, code int) {
	p.kubectlExitCodeCount.With(prometheus.Labels{
		"namespace": namespace,
		"exit_code": strconv.Itoa(code),
	}).Inc()
}

// UpdateResultSummary sets gauges for resources applied during the last run of
// each Application
func (p *Prometheus) UpdateResultSummary(apps []kubeapplierv1alpha1.Application) {
	p.resultSummary.Reset()

	for _, app := range apps {
		if app.Status.LastRun == nil {
			continue
		}
		res := parseKubectlOutput(app.Status.LastRun.Output)
		for _, r := range res {
			p.resultSummary.With(prometheus.Labels{
				"namespace": app.Namespace,
				"type":      r.Type,
				"name":      r.Name,
				"action":    r.Action,
			}).Set(1)
		}
	}
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
