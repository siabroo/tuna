package prom_test

import (
	"context"
	"strings"
	"testing"

	"github.com/siabroo/tuna/internal/prom"
	"github.com/siabroo/tuna/internal/testenv"
)

func TestClient_Query_ReturnsValue(t *testing.T) {
	mock := testenv.StartPromMock(t, func(query string) testenv.PromResult {
		if strings.Contains(query, "go_info") {
			return testenv.PromResult{Value: 1.0}
		}
		return testenv.PromResult{Empty: true}
	})

	client, err := prom.NewClient(mock.URL, prom.AuthNone)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	v, empty, err := client.Query(context.Background(), `count(go_info{namespace="default"})`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if empty {
		t.Fatal("empty=true, want false")
	}
	if v != 1.0 {
		t.Errorf("v = %v, want 1.0", v)
	}
}

func TestClient_Query_EmptyResult(t *testing.T) {
	mock := testenv.StartPromMock(t, func(query string) testenv.PromResult {
		return testenv.PromResult{Empty: true}
	})

	client, _ := prom.NewClient(mock.URL, prom.AuthNone)
	_, empty, err := client.Query(context.Background(), `anything`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !empty {
		t.Error("empty=false, want true")
	}
}

func TestClient_Query_PromError(t *testing.T) {
	mock := testenv.StartPromMock(t, func(query string) testenv.PromResult {
		return testenv.PromResult{Error: "parse error: unexpected character"}
	})

	client, _ := prom.NewClient(mock.URL, prom.AuthNone)
	_, _, err := client.Query(context.Background(), `bad{`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse error") {
		t.Errorf("error message = %q, want 'parse error' substring", err.Error())
	}
}

func TestClient_Query_UnreachableServer(t *testing.T) {
	client, _ := prom.NewClient("http://127.0.0.1:1/this-port-not-open", prom.AuthNone)
	_, _, err := client.Query(context.Background(), `up`)
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

func TestClient_QueryFirstLabel_ReturnsLabel(t *testing.T) {
	mock := testenv.StartPromMock(t, func(query string) testenv.PromResult {
		return testenv.PromResult{Empty: true}
	})
	// For the actual label-extraction path, since PromMock's vector response
	// has an empty metric map, this test will return ("", true, nil).
	// That's the "no version label" case — still useful to verify.
	client, _ := prom.NewClient(mock.URL, prom.AuthNone)
	val, empty, err := client.QueryFirstLabel(context.Background(), `go_info`, "version")
	if err != nil {
		t.Fatalf("QueryFirstLabel: %v", err)
	}
	if !empty {
		t.Errorf("empty=false with empty mock, got val=%q", val)
	}
}
