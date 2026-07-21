package device

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uvwt/agentdock/internal/goal"
)

func TestRemoteClientFetchGoal(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "remote", Objective: "fetch me",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/runtime/goals/"+g.ID {
			http.NotFound(w, r)
			return
		}
		loaded, _ := store.Get(g.ID)
		payload := map[string]any{"goal": loaded, "capsule": goal.BuildCapsule(loaded)}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	cli := &RemoteClient{BaseURL: srv.URL}
	got, cap, err := cli.FetchGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != g.ID || cap.GoalID != g.ID {
		t.Fatalf("got=%#v cap=%#v", got, cap)
	}

	local, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	imported, err := ImportCapsule(local, cap, "")
	if err != nil {
		t.Fatal(err)
	}
	if imported.Title != "remote" {
		t.Fatalf("imported=%#v", imported)
	}
}
