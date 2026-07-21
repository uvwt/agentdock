package reasoner

import (
	"context"
	"testing"
)

func TestRouteArchitecturePrefersChatGPTWeb(t *testing.T) {
	r := &Router{AllowChatGPTWeb: true, AllowAPI: true, AllowLocal: true}
	dec, err := r.Route(Request{Class: TaskArchitecture})
	if err != nil || dec.Primary != BackendChatGPTWeb {
		t.Fatalf("dec=%#v err=%v", dec, err)
	}
}

func TestRouteLogSummaryLocal(t *testing.T) {
	r := &Router{AllowLocal: true, AllowAPI: true}
	dec, err := r.Route(Request{Class: TaskLogSummary})
	if err != nil || dec.Primary != BackendLocal {
		t.Fatalf("dec=%#v err=%v", dec, err)
	}
}

func TestBudgetPrefersLocalWhenTight(t *testing.T) {
	b := &Budget{LimitUSD: 0.005}
	r := &Router{AllowAPI: true, AllowLocal: true, Budget: b}
	// API estimate 0.01 > remaining 0.005, Route should still return something;
	// Run with charge should succeed on local for log summary (0 cost).
	used, err := r.Run(context.Background(), Request{Class: TaskLogSummary}, func(ctx context.Context, backend Backend, req Request) error {
		if backend != BackendLocal {
			t.Fatalf("expected local, got %s", backend)
		}
		return nil
	})
	if err != nil || used != BackendLocal {
		t.Fatalf("used=%s err=%v", used, err)
	}
}

func TestNoBackendError(t *testing.T) {
	r := &Router{}
	if _, err := r.Route(Request{Class: TaskRoutineFix}); err == nil {
		t.Fatal("expected error")
	}
}

func TestClassifyHeuristic(t *testing.T) {
	if ClassifyHeuristic("summarize these logs") != TaskLogSummary {
		t.Fatal("log")
	}
	if ClassifyHeuristic("fix the login bug") != TaskRoutineFix {
		t.Fatal("fix")
	}
	if ClassifyHeuristic("architecture decision for pipeline") != TaskArchitecture {
		t.Fatal("arch")
	}
}
