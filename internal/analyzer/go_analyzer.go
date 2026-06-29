package analyzer

import (
	"fmt"
	"math"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// GoAnalyzer analyzes Go workloads. Detection: presence of `go_info`
// Prometheus series. Recommendations: resources + GOMAXPROCS +
// GOMEMLIMIT + GOGC per spec §6.
type GoAnalyzer struct {
	// SuppressTolerance is the relative-delta below which a
	// recommendation is suppressed. Defaults to 0.10 (10%).
	SuppressTolerance float64
}

func (GoAnalyzer) Name() string { return "go" }

// DetectionProbe — caller substitutes the pod selector. Result > 0 → matched.
func (GoAnalyzer) DetectionProbe() string {
	return `count(go_info{%s})`
}

// Analyze produces recommendations. See spec §6 for formulas.
func (g GoAnalyzer) Analyze(obs ObservedMetrics, cur CurrentSettings) []Recommendation {
	tol := g.SuppressTolerance
	if tol == 0 {
		tol = 0.10
	}
	var recs []Recommendation
	recs = append(recs, g.resourceRecs(obs, cur, tol)...)
	// env recs added in Task 8
	return recs
}

func (g GoAnalyzer) resourceRecs(obs ObservedMetrics, cur CurrentSettings, tol float64) []Recommendation {
	var out []Recommendation

	// requests.cpu — target p95, floor 50m.
	if obs.CPUMillicoresP95 > 0 {
		curCPUReq := readMillicores(cur.Resources.Requests, corev1.ResourceCPU)
		want := obs.CPUMillicoresP95
		if want < 50 {
			want = 50
		}
		if !SuppressIfClose(float64(curCPUReq), float64(want), tol) {
			rationale := fmt.Sprintf("p95 CPU usage %dm exceeds requests %dm; scheduler will mispack the pod.", obs.CPUMillicoresP95, curCPUReq)
			if want == 50 && obs.CPUMillicoresP95 < 50 {
				rationale = fmt.Sprintf("p95 CPU usage %dm is well below requests %dm; floor 50m applied.", obs.CPUMillicoresP95, curCPUReq)
			}
			out = append(out, Recommendation{
				Field:       "resources.requests.cpu",
				Current:     formatMillicores(curCPUReq),
				Recommended: formatMillicores(want),
				Rationale:   rationale,
				Severity:    SeverityWarning,
			})
		}
	}

	// limits.cpu — target p99 * 1.25, floor 100m, severity info.
	if obs.CPUMillicoresP99 > 0 {
		curCPULim := readMillicores(cur.Resources.Limits, corev1.ResourceCPU)
		want := int64(math.Ceil(float64(obs.CPUMillicoresP99) * 1.25))
		if want < 100 {
			want = 100
		}
		if !SuppressIfClose(float64(curCPULim), float64(want), tol) {
			out = append(out, Recommendation{
				Field:       "resources.limits.cpu",
				Current:     formatMillicores(curCPULim),
				Recommended: formatMillicores(want),
				Rationale:   fmt.Sprintf("p99 CPU usage %dm needs 25%% headroom over current limit %dm.", obs.CPUMillicoresP99, curCPULim),
				Severity:    SeverityInfo,
			})
		}
	}

	// requests.memory — target p95 working set.
	if obs.MemoryWorkingSetP95Bytes > 0 {
		curMemReq := readBytes(cur.Resources.Requests, corev1.ResourceMemory)
		want := obs.MemoryWorkingSetP95Bytes
		if !SuppressIfClose(float64(curMemReq), float64(want), tol) {
			out = append(out, Recommendation{
				Field:       "resources.requests.memory",
				Current:     formatMiB(curMemReq),
				Recommended: formatMiB(want),
				Rationale:   fmt.Sprintf("p95 memory working set %s exceeds requests %s.", formatMiB(want), formatMiB(curMemReq)),
				Severity:    SeverityWarning,
			})
		}
	}

	// limits.memory — target p99 * 1.20. Memory limits are safety-critical
	// (OOM kills), so suppression is intentionally not applied here; emit
	// a rec whenever current doesn't match the formula.
	if obs.MemoryWorkingSetP99Bytes > 0 {
		curMemLim := readBytes(cur.Resources.Limits, corev1.ResourceMemory)
		want := int64(float64(obs.MemoryWorkingSetP99Bytes) * 1.20)
		wantMiB := formatMiB(want)
		curMiB := formatMiB(curMemLim)
		if curMiB != wantMiB {
			out = append(out, Recommendation{
				Field:       "resources.limits.memory",
				Current:     curMiB,
				Recommended: wantMiB,
				Rationale:   fmt.Sprintf("p99 memory working set %s needs 20%% headroom; current limit %s is close.", formatMiB(obs.MemoryWorkingSetP99Bytes), curMiB),
				Severity:    SeverityWarning,
			})
		}
	}

	return out
}

// readMillicores extracts the value of resource r from list in millicores.
// Returns 0 if unset.
func readMillicores(list corev1.ResourceList, r corev1.ResourceName) int64 {
	q, ok := list[r]
	if !ok {
		return 0
	}
	return q.MilliValue()
}

// readBytes extracts the value of resource r from list in bytes.
func readBytes(list corev1.ResourceList, r corev1.ResourceName) int64 {
	q, ok := list[r]
	if !ok {
		return 0
	}
	v, ok := q.AsInt64()
	if !ok {
		return 0
	}
	return v
}

func formatMillicores(m int64) string {
	if m == 0 {
		return "(unset)"
	}
	return resource.NewMilliQuantity(m, resource.DecimalSI).String()
}

// formatMiB renders bytes as N"Mi" with rounding to the nearest MiB.
// 380Mi = 398458880 bytes.
func formatMiB(b int64) string {
	if b == 0 {
		return "(unset)"
	}
	mib := int64(math.Round(float64(b) / (1024 * 1024)))
	return fmt.Sprintf("%dMi", mib)
}
