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
	recs = append(recs, g.envRecs(obs, cur)...)
	return recs
}

// gomaxprocsAutoSince is the first Go version with automatic cgroup-aware
// GOMAXPROCS behavior. See https://go.dev/doc/go1.25.
const gomaxprocsAutoSince = "1.25"

// envRecs produces GOMAXPROCS / GOMEMLIMIT / GOGC recommendations.
func (g GoAnalyzer) envRecs(obs ObservedMetrics, cur CurrentSettings) []Recommendation {
	var out []Recommendation

	// GOMAXPROCS
	if r := g.gomaxprocsRec(cur); r != nil {
		out = append(out, *r)
	}
	// GOMEMLIMIT
	if r := g.gomemlimitRec(cur); r != nil {
		out = append(out, *r)
	}
	// GOGC — only suggest if GC pause is unusually high.
	if obs.GCPauseP99Ms > 50.0 && cur.Env["GOGC"] == "" {
		out = append(out, Recommendation{
			Field:       "env.GOGC",
			Current:     "(unset)",
			Recommended: "75",
			Rationale:   fmt.Sprintf("p99 GC pause %.1fms is high; consider lowering GOGC (default 100) to trade memory for shorter pauses.", obs.GCPauseP99Ms),
			Severity:    SeverityInfo,
		})
	}
	return out
}

func (g GoAnalyzer) gomaxprocsRec(cur CurrentSettings) *Recommendation {
	cpuLim := readMillicores(cur.Resources.Limits, corev1.ResourceCPU)
	if cpuLim == 0 {
		return nil
	}
	ceilCores := int64(math.Ceil(float64(cpuLim) / 1000.0))
	if ceilCores < 1 {
		ceilCores = 1
	}
	recommended := fmt.Sprintf("%d", ceilCores)
	current := cur.Env["GOMAXPROCS"]
	autoBuiltIn := goVersionAtLeast(cur.GoVersion, gomaxprocsAutoSince)
	optedOut := containsGODEBUGOpt(cur.Env["GODEBUG"], "containermaxprocs=0")

	switch {
	case autoBuiltIn && !optedOut && current == "":
		// Go 1.25+ handles it. Nothing to recommend.
		return nil
	case autoBuiltIn && optedOut:
		return &Recommendation{
			Field:       "env.GOMAXPROCS",
			Current:     "(unset)",
			Recommended: recommended,
			Rationale:   "GODEBUG=containermaxprocs=0 opts out of Go 1.25+ automatic GOMAXPROCS; either remove the opt-out or set GOMAXPROCS explicitly to ceil(cpu limit).",
			Severity:    SeverityWarning,
		}
	case autoBuiltIn && current != "" && current != recommended:
		return &Recommendation{
			Field:       "env.GOMAXPROCS",
			Current:     current,
			Recommended: recommended,
			Rationale:   fmt.Sprintf("GOMAXPROCS=%s does not match ceil(cpu limit)=%s; Go 1.25+ would compute %s automatically if GOMAXPROCS were unset.", current, recommended, recommended),
			Severity:    SeverityInfo,
		}
	case !autoBuiltIn && current == "":
		return &Recommendation{
			Field:       "env.GOMAXPROCS",
			Current:     "(unset)",
			Recommended: recommended,
			Rationale:   "Go < 1.25 runtime sees host CPU count and will oversubscribe under cpu limit; set GOMAXPROCS or import go.uber.org/automaxprocs.",
			Severity:    SeverityWarning,
		}
	}
	return nil
}

func (g GoAnalyzer) gomemlimitRec(cur CurrentSettings) *Recommendation {
	memLim := readBytes(cur.Resources.Limits, corev1.ResourceMemory)
	if memLim == 0 {
		// fall back to request if limit unset
		memLim = readBytes(cur.Resources.Requests, corev1.ResourceMemory)
		if memLim == 0 {
			return nil
		}
	}
	current := cur.Env["GOMEMLIMIT"]
	if current != "" {
		// User explicitly set it; trust them (iter 1).
		return nil
	}
	wantBytes := int64(float64(memLim) * 0.9)
	wantMiB := int64(wantBytes / (1024 * 1024))
	recommended := fmt.Sprintf("%dMiB", wantMiB)
	return &Recommendation{
		Field:       "env.GOMEMLIMIT",
		Current:     "(unset)",
		Recommended: recommended,
		Rationale:   fmt.Sprintf("GC pacing improves when GOMEMLIMIT ≈ 0.9 × container memory limit (%s here).", recommended),
		Severity:    SeverityWarning,
	}
}

// goVersionAtLeast returns true if got >= want.
// got is e.g. "1.26.0", want is e.g. "1.25".
// Empty or malformed got → false.
func goVersionAtLeast(got, want string) bool {
	gMajor, gMinor, ok := parseSemverMM(got)
	if !ok {
		return false
	}
	wMajor, wMinor, ok := parseSemverMM(want)
	if !ok {
		return false
	}
	if gMajor != wMajor {
		return gMajor > wMajor
	}
	return gMinor >= wMinor
}

func parseSemverMM(v string) (int, int, bool) {
	if v == "" {
		return 0, 0, false
	}
	// strip optional leading "go"
	if len(v) >= 2 && v[:2] == "go" {
		v = v[2:]
	}
	var major, minor int
	n, _ := fmt.Sscanf(v, "%d.%d", &major, &minor)
	if n < 2 {
		return 0, 0, false
	}
	return major, minor, true
}

func containsGODEBUGOpt(godebug, opt string) bool {
	if godebug == "" {
		return false
	}
	// GODEBUG is comma-separated key=val pairs.
	for _, kv := range splitCSV(godebug) {
		if kv == opt {
			return true
		}
	}
	return false
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
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
