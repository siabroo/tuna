// sample-go-app is a tiny HTTP server with /metrics + /work endpoints,
// used by tuna e2e tests as a target workload. It exposes the standard
// Go runtime collector (go_info, go_goroutines, go_gc_*).
package main

import (
	"fmt"
	"log"
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
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
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
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
