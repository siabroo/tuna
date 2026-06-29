package testenv

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// PromResult is one canned PromQL response.
// Empty=true means "no series matched" (valid, vector with zero entries).
type PromResult struct {
	Value float64
	Empty bool
	// Error sets the Prometheus error status. Empty = success.
	Error string
}

// QueryHandler is the function the test provides to answer queries.
// Receives the raw PromQL string from the `query` form parameter.
type QueryHandler func(query string) PromResult

// PromMock is a mock Prometheus HTTP server.
type PromMock struct {
	Server *httptest.Server
	// URL exposes the base URL the operator should be pointed at.
	URL string
}

// StartPromMock starts a fake Prometheus HTTP API server. Registers
// t.Cleanup to tear it down.
func StartPromMock(t *testing.T, fn QueryHandler) *PromMock {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		query := r.FormValue("query")
		res := fn(query)
		respondPromQL(w, res)
	})
	mux.HandleFunc("/api/v1/query_range", func(w http.ResponseWriter, r *http.Request) {
		query := r.FormValue("query")
		res := fn(query)
		respondPromQL(w, res)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &PromMock{Server: srv, URL: srv.URL}
}

func respondPromQL(w http.ResponseWriter, res PromResult) {
	w.Header().Set("Content-Type", "application/json")
	if res.Error != "" {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "error",
			"errorType": "execution",
			"error":     res.Error,
		})
		return
	}
	var result []map[string]any
	if !res.Empty {
		ts := float64(time.Now().Unix())
		result = []map[string]any{{
			"metric": map[string]string{},
			"value":  []any{ts, strconv.FormatFloat(res.Value, 'f', -1, 64)},
		}}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "vector",
			"result":     result,
		},
	})
	_ = fmt.Sprint // keep fmt linked
}
