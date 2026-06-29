// Package prom provides a minimal PromQL HTTP API client with
// pluggable auth (none, gcp-id-token in iter 1). Just enough for
// instant-vector queries that the operator needs.
package prom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a thin wrapper around the Prometheus HTTP API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient constructs a client. The transport wraps http.DefaultTransport
// with the auth layer matching the given mode.
func NewClient(baseURL string, mode AuthMode) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("prom: empty baseURL")
	}
	rt, err := newAuthTransport(mode)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second, Transport: rt},
	}, nil
}

// Query executes an instant PromQL query.
// Returns (value, empty, error):
//   - empty=true means the query was valid but returned no series.
//   - On Prom error status, returns a non-nil error containing Prom's
//     error message.
//   - On HTTP error (connection refused, 5xx), returns a non-nil error.
//   - For non-scalar/vector results, takes the first value found.
func (c *Client) Query(ctx context.Context, q string) (float64, bool, error) {
	u := c.baseURL + "/api/v1/query"
	form := url.Values{"query": {q}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, false, fmt.Errorf("prom query http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, false, fmt.Errorf("prom read body: %w", err)
	}
	if resp.StatusCode >= 400 && resp.StatusCode != 422 {
		return 0, false, fmt.Errorf("prom http %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Status    string `json:"status"`
		Error     string `json:"error"`
		ErrorType string `json:"errorType"`
		Data      struct {
			ResultType string            `json:"resultType"`
			Result     []json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, false, fmt.Errorf("prom decode: %w (body: %s)", err, string(body))
	}
	if parsed.Status != "success" {
		return 0, false, fmt.Errorf("prom %s: %s", parsed.ErrorType, parsed.Error)
	}
	if len(parsed.Data.Result) == 0 {
		return 0, true, nil
	}
	// Each result is {metric: {...}, value: [ts, "string-value"]}.
	var first struct {
		Value []any `json:"value"`
	}
	if err := json.Unmarshal(parsed.Data.Result[0], &first); err != nil {
		return 0, false, fmt.Errorf("prom decode result[0]: %w", err)
	}
	if len(first.Value) < 2 {
		return 0, false, fmt.Errorf("prom result.value malformed: %v", first.Value)
	}
	s, ok := first.Value[1].(string)
	if !ok {
		return 0, false, fmt.Errorf("prom result.value[1] is %T not string", first.Value[1])
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false, fmt.Errorf("prom parse value: %w", err)
	}
	return v, false, nil
}

// QueryFirstLabel runs the query and returns the value of the named
// label on the first result series. Returns ("", true, nil) when
// the query succeeded but returned zero series. Returns
// ("", false, err) on any other error.
func (c *Client) QueryFirstLabel(ctx context.Context, query, labelName string) (string, bool, error) {
	u := c.baseURL + "/api/v1/query"
	form := url.Values{"query": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("prom query http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, fmt.Errorf("prom read body: %w", err)
	}
	if resp.StatusCode >= 400 && resp.StatusCode != 422 {
		return "", false, fmt.Errorf("prom http %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []map[string]any `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false, fmt.Errorf("prom decode: %w", err)
	}
	if parsed.Status != "success" {
		return "", false, fmt.Errorf("prom: %s", parsed.Error)
	}
	if len(parsed.Data.Result) == 0 {
		return "", true, nil
	}
	metric, ok := parsed.Data.Result[0]["metric"].(map[string]any)
	if !ok {
		return "", false, nil
	}
	val, ok := metric[labelName].(string)
	if !ok {
		return "", false, nil
	}
	return val, false, nil
}
