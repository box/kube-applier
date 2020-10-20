package metrics

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/utilitywarehouse/kube-applier/log"
)

var kubectlOutputPattern = regexp.MustCompile(`([\w.\-]+)\/([\w.\-:]+) ([\w-]+).*`)

// PrometheusInterface allows for mocking out the functionality of Prometheus when testing the full process of an apply run.
type PrometheusInterface interface {
	UpdateKubectlExitCodeCount(string, int)
	UpdateNamespaceSuccess(string, bool)
	UpdateRunLatency(float64, bool)
	UpdateResultSummary(map[string]string)
	UpdateLastRunTimestamp(time.Time)
}

// Prometheus implements instrumentation of metrics for kube-applier.
// kubectlExitCodeCount is a Counter vector to increment the number of exit codes for each kubectl execution
// fileApplyCount is a Counter vector to increment the number of successful and failed apply attempts for each file in the repo.
// runLatency is a Summary vector that keeps track of the duration for apply runs.
type Prometheus struct {
	kubectlExitCodeCount *prometheus.CounterVec
	namespaceApplyCount  *prometheus.CounterVec
	runLatency           *prometheus.HistogramVec
	resultSummary        *prometheus.GaugeVec
	lastRunTimestamp     prometheus.Gauge
}

// Init creates and registers the custom metrics for kube-applier.
func (p *Prometheus) Init() {
	p.kubectlExitCodeCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kubectl_exit_code_count",
		Help: "Count of kubectl exit codes",
	},
		[]string{
			// Path of the file that was applied
			"namespace",
			// Exit code
			"exit_code",
		},
	)
	p.namespaceApplyCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "namespace_apply_count",
		Help: "Success metric for every namespace applied",
	},
		[]string{
			// Path of the file that was applied
			"namespace",
			// Result: true if the apply was successful, false otherwise
			"success",
		},
	)
	p.runLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "run_latency_seconds",
		Help: "Latency for completed apply runs",
	},
		[]string{
			// Result: true if the run was successful, false otherwise
			"success",
		},
	)
	p.resultSummary = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "result_summary",
		Help: "Result summary for every manifest",
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
	p.lastRunTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "last_run_timestamp_seconds",
		Help: "Timestamp of the last completed apply run",
	})

	prometheus.MustRegister(p.kubectlExitCodeCount)
	prometheus.MustRegister(p.resultSummary)
	prometheus.MustRegister(p.namespaceApplyCount)
	prometheus.MustRegister(p.runLatency)
	prometheus.MustRegister(p.lastRunTimestamp)
}

// UpdateKubectlExitCodeCount increments for each exit code returned by kubectl
func (p *Prometheus) UpdateKubectlExitCodeCount(file string, code int) {
	p.kubectlExitCodeCount.With(prometheus.Labels{
		"namespace": filepath.Base(file),
		"exit_code": strconv.Itoa(code),
	}).Inc()
}

// UpdateNamespaceSuccess increments the given namespace's Counter for either successful apply attempts or failed apply attempts.
func (p *Prometheus) UpdateNamespaceSuccess(file string, success bool) {
	p.namespaceApplyCount.With(prometheus.Labels{
		"namespace": filepath.Base(file), "success": strconv.FormatBool(success),
	}).Inc()
}

// UpdateRunLatency adds a data point (latency of the most recent run) to the run_latency_seconds Summary metric, with a tag indicating whether or not the run was successful.
func (p *Prometheus) UpdateRunLatency(runLatency float64, success bool) {
	p.runLatency.With(prometheus.Labels{
		"success": strconv.FormatBool(success),
	}).Observe(runLatency)
}

// UpdateResultSummary sets gauges for each deployment
func (p *Prometheus) UpdateResultSummary(failures map[string]string) {
	p.resultSummary.Reset()

	for filePath, output := range failures {
		res := parseKubectlOutput(output)
		for _, r := range res {
			p.resultSummary.With(prometheus.Labels{
				"namespace": filepath.Base(filePath),
				"type":      r.Type,
				"name":      r.Name,
				"action":    r.Action,
			}).Set(1)
		}
	}
}

// UpdateLastRunTimestamp records the last time a run finished
func (p *Prometheus) UpdateLastRunTimestamp(t time.Time) {
	p.lastRunTimestamp.Set(float64(t.UnixNano()) / 1e9)
}

// Result struct containing Type, Name and Action
type Result struct {
	Type, Name, Action string
}

func parseKubectlOutput(output string) []Result {
	output = strings.TrimSpace(output)
	lines := strings.Split(output, "\n")
	var results []Result
	for _, line := range lines {
		m := kubectlOutputPattern.FindAllStringSubmatch(line, -1)
		// Should be only 1 match, and should contain 4 elements (0: whole match, 1: resource-type, 2: name, 3: action
		if len(m) != 1 || len(m[0]) != 4 {
			log.Logger.Warn("Expected format: <resource-type>/<name> <action>", "line", line, "full output", output)
			continue
		}
		results = append(results, Result{
			Type:   m[0][1],
			Name:   m[0][2],
			Action: m[0][3],
		})
	}
	return results
}
