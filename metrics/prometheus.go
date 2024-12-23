package metrics

import (
	"github.com/box/kube-applier/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"strconv"
)

// Prometheus implements instrumentation of metrics for kube-applier.
// fileApplyCount is a Counter vector to increment the number of successful and failed apply attempts for each file in the repo.
// runLatency is a Summary vector that keeps track of the duration for apply runs.
type Prometheus struct {
	RunMetrics     <-chan run.Result
	fileApplyCount *prometheus.CounterVec
	runLatency     *prometheus.SummaryVec
}

// GetHandler returns a handler for exposing Prometheus metrics via HTTP.
func (p *Prometheus) GetHandler() http.Handler {
	return promhttp.Handler()
}

// Configure creates and registers the custom metrics for kube-applier, and starts a loop to receive run results.
func (p *Prometheus) Configure() {
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
			// FullRun or QuickRun
			"run_type",
		},
	)

	prometheus.MustRegister(p.fileApplyCount)
	prometheus.MustRegister(p.runLatency)
}

// StartMetricsLoop receives from the RunMetrics channel and calls processResult when a run result comes in.
func (p *Prometheus) StartMetricsLoop() {
	for result := range p.RunMetrics {
		p.processResult(result)
	}
}

// processResult parses a run result for info and updates the metrics (file_apply_count and run_latency_seconds).
func (p *Prometheus) processResult(result run.Result) {
	runSuccess := len(result.Failures) == 0
	runType := result.RunType
	latency := result.Finish.Sub(result.Start).Seconds()
	for _, successFile := range result.Successes {
		p.fileApplyCount.With(prometheus.Labels{"file": successFile.FilePath, "success": "true"}).Inc()
	}
	for _, failureFile := range result.Failures {
		p.fileApplyCount.With(prometheus.Labels{"file": failureFile.FilePath, "success": "false"}).Inc()
	}
	p.runLatency.With(prometheus.Labels{
		"success":  strconv.FormatBool(runSuccess),
		"run_type": string(runType),
	}).Observe(latency)
}
