// sample-go-app is a tiny HTTP server with /metrics + /work endpoints,
// used by tuna e2e tests as a target workload. It exposes the standard
// Go runtime collector (go_info, go_goroutines, go_gc_*).
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// memHold keeps allocated memory alive so the GC cannot reclaim it.
var memHold []byte

func main() {
	cpuPct, _ := strconv.Atoi(os.Getenv("LOAD_CPU_PERCENT"))
	memMB, _ := strconv.Atoi(os.Getenv("LOAD_MEM_MB"))

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	mux.HandleFunc("/work", workHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	log.Printf("sample-go-app listening on :8080 (Go %s, %d CPUs)", runtime.Version(), runtime.NumCPU())
	log.Printf("load config: LOAD_CPU_PERCENT=%d LOAD_MEM_MB=%d", cpuPct, memMB)

	startCPULoad(cpuPct)
	startMemoryHold(memMB)

	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}

// startCPULoad spawns a background goroutine that consumes pct% of one CPU
// core continuously. It works in 100 ms windows: burn for pct ms, then sleep
// for the remainder. pct=0 is a no-op; pct=100 is a tight loop.
func startCPULoad(pct int) {
	if pct <= 0 {
		return
	}
	if pct > 100 {
		pct = 100
	}
	burnDur := time.Duration(pct) * time.Millisecond
	sleepDur := time.Duration(100-pct) * time.Millisecond

	go func() {
		for {
			deadline := time.Now().Add(burnDur)
			for time.Now().Before(deadline) {
				// Tight spin — just poll the clock; no math needed.
				_ = time.Now()
			}
			if sleepDur > 0 {
				time.Sleep(sleepDur)
			}
		}
	}()
}

// startMemoryHold allocates mb megabytes and touches every OS page so the
// kernel actually commits the memory. The slice is kept in a package-level
// variable so the GC never reclaims it.
func startMemoryHold(mb int) {
	if mb <= 0 {
		return
	}
	size := mb * 1024 * 1024
	buf := make([]byte, size)
	for i := 0; i < len(buf); i += 4096 {
		buf[i] = 1
	}
	memHold = buf
}

// workHandler burns CPU for a configurable duration. Used by e2e to
// generate non-trivial usage so Prometheus sees something to analyze.
func workHandler(w http.ResponseWriter, r *http.Request) {
	deadline := time.Now().Add(200 * time.Millisecond)
	x := 0.0
	for time.Now().Before(deadline) {
		x += 1.0 / float64(time.Now().UnixNano()&0xff+1)
	}
	fmt.Fprintf(w, "did work: %f\n", x)
}
