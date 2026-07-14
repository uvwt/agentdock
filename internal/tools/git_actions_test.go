package tools

import "testing"

func TestAnnotateGitPushResultClassifiesSuccessfulWarning(t *testing.T) {
	result := Result{
		"command_ok": true,
		"output":     "fatal: unable to get credential storage lock in 1000 ms: File exists\nTo https://example.invalid/repo.git\n   624b51e..9f6edef  main -> main\n",
	}

	annotateGitPushResult(result)

	if result["push_succeeded"] != true || result["remote_updated"] != true || result["fatal_but_non_blocking"] != true {
		t.Fatalf("expected successful push with non-blocking fatal warning: %#v", result)
	}
	if result["push_status"] != "pushed" {
		t.Fatalf("unexpected push status: %#v", result)
	}
}

func TestAnnotateGitPushResultClassifiesUpToDate(t *testing.T) {
	result := Result{"command_ok": true, "output": "Everything up-to-date\n"}

	annotateGitPushResult(result)

	if result["push_succeeded"] != true || result["remote_updated"] != false || result["up_to_date"] != true {
		t.Fatalf("expected up-to-date successful push: %#v", result)
	}
	if result["push_status"] != "up_to_date" {
		t.Fatalf("unexpected push status: %#v", result)
	}
}
