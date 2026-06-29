package testenv

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	tunav1alpha1 "github.com/siabroo/tuna/api/v1alpha1"
)

func TestStart_RegistersCRD(t *testing.T) {
	env := Start(t)
	// Verify the CRD is installed by looking up the kind via a dynamic RESTMapper.
	httpClient, err := rest.HTTPClientFor(env.Config)
	if err != nil {
		t.Fatalf("HTTPClientFor: %v", err)
	}
	rm, err := apiutil.NewDynamicRESTMapper(env.Config, httpClient)
	if err != nil {
		t.Fatalf("RESTMapper: %v", err)
	}
	if _, err := rm.RESTMapping(tunav1alpha1.GroupVersion.WithKind("WorkloadRecommendation").GroupKind()); err != nil {
		t.Fatalf("WorkloadRecommendation kind not mapped: %v", err)
	}
}

func TestStartPromMock_AnswersQuery(t *testing.T) {
	mock := StartPromMock(t, func(query string) PromResult {
		if strings.Contains(query, "go_info") {
			return PromResult{Value: 1.0}
		}
		return PromResult{Empty: true}
	})

	form := url.Values{"query": {"go_info"}}
	resp, err := http.PostForm(mock.URL+"/api/v1/query", form)
	if err != nil {
		t.Fatalf("PostForm: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
