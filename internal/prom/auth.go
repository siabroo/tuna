package prom

import (
	"fmt"
	"net/http"
)

// AuthMode selects the transport.
type AuthMode string

const (
	AuthNone       AuthMode = "none"
	AuthGCPIDToken AuthMode = "gcp-id-token"
)

// newAuthTransport returns an http.RoundTripper for the given mode.
// Wraps http.DefaultTransport.
func newAuthTransport(mode AuthMode) (http.RoundTripper, error) {
	switch mode {
	case AuthNone, "":
		return http.DefaultTransport, nil
	case AuthGCPIDToken:
		return &gcpIDTokenTransport{base: http.DefaultTransport}, nil
	default:
		return nil, fmt.Errorf("prom: unknown auth mode %q (supported: none, gcp-id-token)", mode)
	}
}

// gcpIDTokenTransport fetches an OAuth2 ID token from the GCE/GKE
// metadata server on each request and sets the Authorization header.
// In production this would cache the token (with refresh ~5min before
// expiry). Iter 1: fetch per request — simple, slower under load but
// correct.
//
// To keep this PoC dependency-light, we read the metadata-server token
// endpoint directly rather than pulling in google.golang.org/api.
type gcpIDTokenTransport struct {
	base http.RoundTripper
}

const gcpMetadataTokenURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity?audience=https://monitoring.googleapis.com/"

func (g *gcpIDTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tokReq, err := http.NewRequestWithContext(req.Context(), http.MethodGet, gcpMetadataTokenURL, nil)
	if err != nil {
		return nil, err
	}
	tokReq.Header.Set("Metadata-Flavor", "Google")
	tokResp, err := http.DefaultClient.Do(tokReq)
	if err != nil {
		return nil, fmt.Errorf("gcp metadata id-token: %w", err)
	}
	defer tokResp.Body.Close()
	if tokResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gcp metadata id-token: status %d", tokResp.StatusCode)
	}
	buf := make([]byte, 4096)
	n, _ := tokResp.Body.Read(buf)
	if n == 0 {
		return nil, fmt.Errorf("gcp metadata id-token: empty body")
	}
	token := string(buf[:n])
	// Defensive copy so we don't mutate the caller's request.
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "Bearer "+token)
	return g.base.RoundTrip(req2)
}
