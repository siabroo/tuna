package controller_test

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/siabroo/tuna/internal/controller"
)

func drain(ch <-chan string, timeout time.Duration) []string {
	var out []string
	deadline := time.After(timeout)
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-deadline:
			return out
		}
	}
}

func TestDedupRecorder_SuppressesDuplicates(t *testing.T) {
	fake := record.NewFakeRecorder(10)
	d := controller.NewDedupRecorder(fake, 100*time.Millisecond)

	pod := &corev1.Pod{}
	d.Event(pod, corev1.EventTypeNormal, "AnalysisCompleted", "1 rec")
	d.Event(pod, corev1.EventTypeNormal, "AnalysisCompleted", "1 rec") // dup → suppressed
	d.Event(pod, corev1.EventTypeNormal, "AnalysisCompleted", "2 recs") // different msg → emitted

	got := drain(fake.Events, 50*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %v", len(got), got)
	}
	if !strings.Contains(got[0], "1 rec") || !strings.Contains(got[1], "2 recs") {
		t.Errorf("unexpected event contents: %v", got)
	}

	// After dedup window expires, the same message should be allowed again.
	time.Sleep(120 * time.Millisecond)
	d.Event(pod, corev1.EventTypeNormal, "AnalysisCompleted", "1 rec")
	got = drain(fake.Events, 50*time.Millisecond)
	if len(got) != 1 {
		t.Errorf("after window: got %d events, want 1", len(got))
	}
}
