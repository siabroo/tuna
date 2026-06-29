// Package analyzer defines the Analyzer interface and the internal
// types it operates on. Analyzers are workload-type plugins;
// iter 1 has only GoAnalyzer.
package analyzer

import (
	corev1 "k8s.io/api/core/v1"
)

// Severity levels for recommendations. Matches CRD enum exactly.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// ObservedMetrics is the analyzer-side input (already digested from
// raw PromQL results into clean numbers).
type ObservedMetrics struct {
	Window string // e.g. "24h"

	CPUMillicoresP50 int64
	CPUMillicoresP95 int64
	CPUMillicoresP99 int64

	MemoryWorkingSetP95Bytes int64
	MemoryWorkingSetP99Bytes int64

	GCPauseP99Ms     float64 // 0 means no data
	GCPauseP99Window string  // optional, "1h" by default
}

// CurrentSettings is the analyzer-side view of the target Deployment's
// container.
type CurrentSettings struct {
	Resources corev1.ResourceRequirements
	Env       map[string]string // GOMAXPROCS, GOMEMLIMIT, GOGC keys
	GoVersion string            // best-effort, "" if unknown
}

// Recommendation is one piece of analyzer output.
type Recommendation struct {
	Field       string
	Current     string
	Recommended string
	Rationale   string
	Severity    string
}

// Analyzer is the plugin contract for a workload type.
type Analyzer interface {
	// Name is the workload kind, lower-case ("go", "nodejs", "jvm").
	Name() string
	// DetectionProbe returns the PromQL query whose nonzero result
	// signals "this workload type is present". Caller supplies the
	// label scoping (pod=~"..." namespace=..) by substitution; the
	// probe template uses the placeholder %s where appropriate.
	DetectionProbe() string
	// Analyze produces recommendations given observed metrics and
	// current settings. Must be pure (no side effects, no I/O).
	Analyze(observed ObservedMetrics, current CurrentSettings) []Recommendation
}
