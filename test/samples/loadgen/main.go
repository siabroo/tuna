// Package main is a configurable load generator used as a tuna e2e
// fixture. It exports Go runtime metrics (go_info, go_gc_*, go_memstats_*)
// via /metrics and exposes a small HTTP API to control CPU + memory
// consumption at runtime. Initial load is set via LOAD_CPU_PERCENT and
// LOAD_MEM_MB env vars.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// cpuTargetPercent is the % of one CPU core to keep busy.
// 0 = idle, 100 = peg one core. Read atomically by the load loop.
var cpuTargetPercent atomic.Int32

// memHold guards the held memory slice.
var (
	memMu   sync.Mutex
	memHold []byte
)

type loadConfig struct {
	CPUPercent int `json:"cpu_percent"`
	MemMB      int `json:"mem_mb"`
}

func main() {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	// Initial load from env.
	if cpu, err := strconv.Atoi(os.Getenv("LOAD_CPU_PERCENT")); err == nil && cpu > 0 {
		setCPU(cpu)
	}
	if mem, err := strconv.Atoi(os.Getenv("LOAD_MEM_MB")); err == nil && mem > 0 {
		setMemory(mem)
	}

	// Background CPU loop runs forever; reads target atomically.
	go cpuLoadLoop()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/load", loadHandler)
	mux.HandleFunc("/load/cpu", loadCPUHandler)
	mux.HandleFunc("/load/mem", loadMemHandler)

	log.Printf("loadgen listening on :8080 (Go %s, %d CPUs); initial cpu=%d%% mem=%dMB",
		runtime.Version(), runtime.NumCPU(),
		cpuTargetPercent.Load(), heldMB())

	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}

// cpuLoadLoop continuously bursts CPU for N ms then sleeps (100-N) ms
// where N is the current cpuTargetPercent (clamped 0-100). The 100ms
// window keeps the scheduler responsive.
func cpuLoadLoop() {
	for {
		pct := int(cpuTargetPercent.Load())
		if pct <= 0 {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if pct > 100 {
			pct = 100
		}
		burnUntil := time.Now().Add(time.Duration(pct) * time.Millisecond)
		for time.Now().Before(burnUntil) {
			// tight loop; the runtime preempts long-running goroutines
		}
		if pct < 100 {
			time.Sleep(time.Duration(100-pct) * time.Millisecond)
		}
	}
}

func setCPU(pct int) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	cpuTargetPercent.Store(int32(pct))
}

func setMemory(mb int) {
	memMu.Lock()
	defer memMu.Unlock()
	if mb <= 0 {
		memHold = nil
		runtime.GC() // hint
		return
	}
	buf := make([]byte, mb*1024*1024)
	// touch every 4KB page so OS commits pages (not just reserves vm)
	for i := 0; i < len(buf); i += 4096 {
		buf[i] = 1
	}
	memHold = buf
}

func heldMB() int {
	memMu.Lock()
	defer memMu.Unlock()
	return len(memHold) / (1024 * 1024)
}

// loadHandler handles GET (read state) and POST (set both fields).
func loadHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := loadConfig{CPUPercent: int(cpuTargetPercent.Load()), MemMB: heldMB()}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	case http.MethodPost, http.MethodPut:
		var cfg loadConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		setCPU(cfg.CPUPercent)
		setMemory(cfg.MemMB)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		setCPU(0)
		setMemory(0)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func loadCPUHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "use POST/PUT ?percent=N", http.StatusMethodNotAllowed)
		return
	}
	pct, err := strconv.Atoi(r.URL.Query().Get("percent"))
	if err != nil {
		http.Error(w, "bad percent: "+err.Error(), http.StatusBadRequest)
		return
	}
	setCPU(pct)
	w.WriteHeader(http.StatusNoContent)
}

func loadMemHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "use POST/PUT ?mb=N", http.StatusMethodNotAllowed)
		return
	}
	mb, err := strconv.Atoi(r.URL.Query().Get("mb"))
	if err != nil {
		http.Error(w, "bad mb: "+err.Error(), http.StatusBadRequest)
		return
	}
	setMemory(mb)
	w.WriteHeader(http.StatusNoContent)
}
