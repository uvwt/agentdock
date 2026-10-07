package main

import (
	"reflect"
	"testing"
)

func TestBuildR2RetentionPlanKeepsCurrentAndHighestPreviousStable(t *testing.T) {
	plan, err := buildR2RetentionPlan("v1.0.1", []string{
		"v0.9.1",
		"v1.0.0",
		"v1.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.PreviousStable != "v1.0.0" {
		t.Fatalf("PreviousStable = %q, want v1.0.0", plan.PreviousStable)
	}
	if want := []string{"v0.9.1"}; !reflect.DeepEqual(plan.DeleteTags, want) {
		t.Fatalf("DeleteTags = %v, want %v", plan.DeleteTags, want)
	}
}

func TestBuildR2RetentionPlanCleansOldPrereleaseButPreservesUnknownAndFuture(t *testing.T) {
	plan, err := buildR2RetentionPlan("v1.0.1", []string{
		"legacy-backup",
		"v0.9.2-rc.1",
		"v1.0.0-rc.4",
		"v1.0.0",
		"v1.0.1",
		"v1.1.0-rc.1",
		"v1.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.PreviousStable != "v1.0.0" {
		t.Fatalf("PreviousStable = %q, want v1.0.0", plan.PreviousStable)
	}
	if want := []string{"v0.9.2-rc.1", "v1.0.0-rc.4"}; !reflect.DeepEqual(plan.DeleteTags, want) {
		t.Fatalf("DeleteTags = %v, want %v", plan.DeleteTags, want)
	}
}

func TestBuildR2RetentionPlanRequiresCurrentPrefix(t *testing.T) {
	if _, err := buildR2RetentionPlan("v1.0.1", []string{"v1.0.0"}); err == nil {
		t.Fatal("expected missing current prefix to fail")
	}
}

func TestBuildR2RetentionPlanRejectsPrereleaseCurrent(t *testing.T) {
	if _, err := buildR2RetentionPlan("v1.0.1-rc.1", []string{"v1.0.1-rc.1"}); err == nil {
		t.Fatal("expected prerelease current tag to fail stable retention")
	}
}
