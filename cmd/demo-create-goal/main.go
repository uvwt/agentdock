package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/uvwt/agentdock/internal/goal"
)

func main() {
	home, _ := os.UserHomeDir()
	store, err := goal.New(filepath.Join(home, ".agentdock", "goals"))
	if err != nil {
		panic(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title:     "L3 測試：示範長任務",
		Objective: "驗證 Goal Mode 無人值守閉環（示範用）。成功條件：有 manual evidence 即可完成。",
		Mode:      goal.ModeGuarded,
		SuccessCriteria: []goal.SuccessCriterionInput{
			{ID: "demo", Type: goal.CriterionManual, Expression: "demo_ok"},
		},
		Constraints: []goal.Constraint{
			{Type: goal.ConstraintProhibition, Value: "no_git_push"},
		},
		Milestones: []goal.MilestoneInput{
			{ID: "m1", Title: "建立與喚醒"},
			{ID: "m2", Title: "提交與驗證"},
		},
	})
	if err != nil {
		panic(err)
	}
	g, err = store.RequestReasoning(g.ID,
		"這是測試 Goal：請先 goal_manage get 讀取 capsule，再 commit_turn 決定下一步。",
		"首次 L3 聯調",
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(g.ID)
	fmt.Println(g.Status)
	fmt.Println("http://127.0.0.1:8765/goal/" + g.ID)
}
