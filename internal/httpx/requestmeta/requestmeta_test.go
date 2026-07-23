package requestmeta

import (
	"context"
	"testing"
)

func TestWithBaseURLRoundTrip(t *testing.T) {
	ctx := context.Background()
	wrapped := WithBaseURL(ctx, "https://agentdock.example/base")
	if got := BaseURL(wrapped); got != "https://agentdock.example/base" {
		t.Fatalf("BaseURL() = %q", got)
	}
}

func TestWithBaseURLEmptyKeepsOriginalContext(t *testing.T) {
	ctx := context.Background()
	wrapped := WithBaseURL(ctx, "")
	if wrapped != ctx {
		t.Fatal("WithBaseURL() wrapped context for empty value")
	}
	if got := BaseURL(wrapped); got != "" {
		t.Fatalf("BaseURL() = %q, want empty", got)
	}
}

func TestWithBaseURLNearestValueWins(t *testing.T) {
	ctx := WithBaseURL(context.Background(), "https://first.example")
	ctx = WithBaseURL(ctx, "https://second.example")
	if got := BaseURL(ctx); got != "https://second.example" {
		t.Fatalf("BaseURL() = %q", got)
	}
}
