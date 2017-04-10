package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"net/http"
	"strconv"
)

// PrometheusInterface allows for mocking out the functionality of Prometheus when testing the full process of an apply run.
type PrometheusInterface interface {
	UpdateFileSuccess(string, bool)
	UpdateRunLatency(float64, bool)
}

// Prometheus implements instrumentation of metrics for kube-applier.
// fileApplyCount is a Counter vector to increment the number of successful and failed apply attempts for each file in the repo.
// runLatency is a Summary vector that keeps track of the duration for apply runs.
type Prometheus struct {
	fileApplyCount *prometheus.CounterVec
	runLatency     *prometheus.SummaryVec
}

// GetHandler returns a handler for exposing Prometheus metrics via HTTP.
func (p *Prometheus) GetHandler() http.Handler {
	return prometheus.UninstrumentedHandler()
}

// Init creates and registers the custom metrics for kube-applier.
func (p *Prometheus) Init() {
	p.fileApplyCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "file_apply_count",
		Help: "Success metric for every file applied",
	},
		[]string{
			// Path of the file that was applied
			"file",
			// Result: true if the apply was successful, false otherwise
			"success",
		},
	)
	p.runLatency = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name: "run_latency_seconds",
		Help: "Latency for completed apply runs",
	},
		[]string{
			// Result: true if the run was successful, false otherwise
			"success",
		},
	)

	prometheus.MustRegister(p.fileApplyCount)
	prometheus.MustRegister(p.runLatency)
}

// UpdateFileSuccess increments the given file's Counter for either successful apply attempts or failed apply attempts.
func (p *Prometheus) UpdateFileSuccess(file string, success bool) {
	p.fileApplyCount.With(prometheus.Labels{
		"file": file, "success": strconv.FormatBool(success),
	}).Inc()
}

// UpdateRunLatency adds a data point (latency of the most recent run) to the run_latency_seconds Summary metric, with a tag indicating whether or not the run was successful.
func (p *Prometheus) UpdateRunLatency(runLatency float64, success bool) {
	p.runLatency.With(prometheus.Labels{
		"success": strconv.FormatBool(success),
	}).Observe(runLatency)
}
