package prom_test

import (
	"strings"
	"testing"
	"time"

	"github.com/siabroo/tuna/internal/prom"
)

func TestQueryCPUPercentile(t *testing.T) {
	q := prom.QueryCPUPercentile("default", `^(svc-1)$`, "app", 0.95, 24*time.Hour)
	wants := []string{`container_cpu_usage_seconds_total`, `default`, `svc-1`, `app`, `[5m]`, `0.95`}
	for _, w := range wants {
		if !strings.Contains(q, w) {
			t.Errorf("CPU query missing %q; got: %s", w, q)
		}
	}
}

func TestQueryMemoryWorkingSetPercentile(t *testing.T) {
	q := prom.QueryMemoryWorkingSetPercentile("default", `^(svc-1)$`, "app", 0.95, 24*time.Hour)
	wants := []string{`container_memory_working_set_bytes`, `app`, `0.95`}
	for _, w := range wants {
		if !strings.Contains(q, w) {
			t.Errorf("memory query missing %q; got: %s", w, q)
		}
	}
}

func TestQueryGCPauseP99(t *testing.T) {
	q := prom.QueryGCPauseP99("default", `^(svc-1)$`, 1*time.Hour)
	if !strings.Contains(q, "go_gc_duration_seconds") {
		t.Errorf("missing go_gc_duration_seconds; got: %s", q)
	}
	if !strings.Contains(q, "0.99") {
		t.Errorf("missing 0.99 quantile; got: %s", q)
	}
}

func TestQueryGoVersion(t *testing.T) {
	q := prom.QueryGoVersion("default", `^(svc-1)$`)
	if !strings.Contains(q, "go_info") {
		t.Errorf("missing go_info; got: %s", q)
	}
	if !strings.Contains(q, "default") {
		t.Errorf("missing namespace; got: %s", q)
	}
}

func TestQueryGoInfoCount(t *testing.T) {
	q := prom.QueryGoInfoCount("default", `^(svc-1)$`)
	if !strings.Contains(q, "count") {
		t.Errorf("missing count(); got: %s", q)
	}
	if !strings.Contains(q, "go_info") {
		t.Errorf("missing go_info; got: %s", q)
	}
}
