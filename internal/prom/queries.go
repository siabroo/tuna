package prom

import (
	"fmt"
	"time"
)

// rateWindow is the inner window used inside rate() / quantile_over_time.
// Smaller = more responsive to recent changes; larger = smoother.
const rateWindow = 5 * time.Minute

// QueryCPUPercentile builds a PromQL query for the cpu-millicore
// percentile over the analysis window. Returns a value in
// millicores (1 core = 1000m).
func QueryCPUPercentile(namespace, podRegex, container string, percentile float64, window time.Duration) string {
	return fmt.Sprintf(
		`quantile_over_time(%g, rate(container_cpu_usage_seconds_total{namespace=%q,pod=~%q,container=%q}[%s])[%s:%s]) * 1000`,
		percentile, namespace, podRegex, container,
		durationToPromString(rateWindow),
		durationToPromString(window),
		durationToPromString(rateWindow),
	)
}

// QueryMemoryWorkingSetPercentile builds a PromQL query for the memory
// working-set percentile in bytes.
func QueryMemoryWorkingSetPercentile(namespace, podRegex, container string, percentile float64, window time.Duration) string {
	return fmt.Sprintf(
		`quantile_over_time(%g, container_memory_working_set_bytes{namespace=%q,pod=~%q,container=%q}[%s:%s])`,
		percentile, namespace, podRegex, container,
		durationToPromString(window),
		durationToPromString(rateWindow),
	)
}

// QueryGCPauseP99 builds a query for the p99 Go GC pause in
// milliseconds, over the given window.
func QueryGCPauseP99(namespace, podRegex string, window time.Duration) string {
	// go_gc_duration_seconds is a summary; its 0.99 quantile is exposed
	// directly via the {quantile="0.99"} label.
	return fmt.Sprintf(
		`max_over_time(go_gc_duration_seconds{namespace=%q,pod=~%q,quantile="0.99"}[%s]) * 1000`,
		namespace, podRegex, durationToPromString(window),
	)
}

// QueryGoVersion returns a query whose label set contains the Go
// version of the running pods. The operator reads this from the
// metric label, not the value.
func QueryGoVersion(namespace, podRegex string) string {
	return fmt.Sprintf(`go_info{namespace=%q,pod=~%q}`, namespace, podRegex)
}

// QueryGoInfoCount probes for the existence of any go_info series.
func QueryGoInfoCount(namespace, podRegex string) string {
	return fmt.Sprintf(`count(go_info{namespace=%q,pod=~%q})`, namespace, podRegex)
}

// durationToPromString returns the Prometheus-friendly representation.
func durationToPromString(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
