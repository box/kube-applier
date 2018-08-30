package metrics

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/utilitywarehouse/kube-applier/log"
)

//go:generate mockgen -package=metrics -destination=mock_prometheus.go -source prometheus.go
// PrometheusInterface allows for mocking out the functionality of Prometheus when testing the full process of an apply run.
type PrometheusInterface interface {
	UpdateNamespaceSuccess(string, bool)
	UpdateRunLatency(float64, bool)
	UpdateFailedResultSummary(failures map[string]string)
}

// Prometheus implements instrumentation of metrics for kube-applier.
// fileApplyCount is a Counter vector to increment the number of successful and failed apply attempts for each file in the repo.
// runLatency is a Summary vector that keeps track of the duration for apply runs.
type Prometheus struct {
	namespaceApplyCount *prometheus.CounterVec
	runLatency          *prometheus.HistogramVec
	resultSummary       *prometheus.GaugeVec
}

// Init creates and registers the custom metrics for kube-applier.
func (p *Prometheus) Init() {
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
		},
	)

	prometheus.MustRegister(p.resultSummary)
	prometheus.MustRegister(p.namespaceApplyCount)
	prometheus.MustRegister(p.runLatency)
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

// UpdateResultSummary sets failure gauges
func (p *Prometheus) UpdateFailedResultSummary(failures map[string]string) {
	p.resultSummary.Reset()

	for filePath, output := range failures {
		outputParts := strings.Split(output, " ")
		if len(outputParts) != 3 {
			log.Logger.Warn(fmt.Sprintf("unable to pass output: %s", output))
			continue
		}

		p.resultSummary.With(prometheus.Labels{
			"namespace": filePath,
			"type":      outputParts[0],
			"name":      strings.TrimSuffix(strings.TrimPrefix(outputParts[1], "\""), "\""),
		}).Set(1)
	}
}
