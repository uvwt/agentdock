package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (r *Runtime) gitHubRepoAccess(ctx context.Context, args map[string]any) (Result, error) {
	repo := normalizeGitHubRepo(stringArg(args, "repo", ""))
	if repo == "" || !strings.Contains(repo, "/") {
		return nil, toolError("INVALID_ARGUMENT", "repo is required as owner/name or GitHub URL", "validation")
	}
	token, username, err := githubCredential(r.ws.Root())
	if err != nil {
		return Result{"ok": false, "credential_found": false, "diagnostic": map[string]any{"kind": "git_auth_missing", "suggestion": "configure a GitHub credential first"}}, nil
	}
	client := &http.Client{Timeout: time.Duration(intArg(args, "timeout_ms", 15000)) * time.Millisecond}
	result := Result{"ok": true, "credential_found": true, "username": username, "repo": repo}
	login, scopes, authStatus, authMessage := githubGet(ctx, client, token, "https://api.github.com/user")
	result["auth_status"] = authStatus
	result["auth_login"] = login
	result["oauth_scopes"] = scopes
	if authMessage != "" {
		result["auth_message"] = authMessage
	}
	_, _, repoStatus, repoMessage := githubGet(ctx, client, token, "https://api.github.com/repos/"+repo)
	result["repo_status"] = repoStatus
	if repoStatus == 200 {
		result["repo_access"] = true
		result["diagnostic"] = map[string]any{"kind": "github_repo_access_ok", "suggestion": "git clone should be permitted if network access is available"}
	} else {
		result["repo_access"] = false
		result["repo_message"] = repoMessage
		result["diagnostic"] = diagnoseGitHubStatus(repoStatus, repoMessage)
	}
	return result, nil
}

func normalizeGitHubRepo(raw string) string {
	repo := strings.TrimSpace(raw)
	repo = strings.TrimPrefix(repo, "https://github.com/")
	repo = strings.TrimPrefix(repo, "http://github.com/")
	repo = strings.TrimSuffix(repo, ".git")
	return strings.Trim(repo, "/")
}

func githubCredential(home string) (token, username string, err error) {
	data, err := os.ReadFile(filepath.Join(home, ".git-credentials"))
	if err != nil {
		return "", "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "github.com") {
			continue
		}
		u, parseErr := url.Parse(strings.TrimSpace(line))
		if parseErr != nil || u.User == nil {
			continue
		}
		pass, _ := u.User.Password()
		return pass, u.User.Username(), nil
	}
	return "", "", fmt.Errorf("github credential not found")
}

func githubGet(ctx context.Context, client *http.Client, token, endpoint string) (login, scopes string, status int, message string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", 0, err.Error()
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "agentdock")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, err.Error()
	}
	defer resp.Body.Close()
	data, err := readBoundedBody(resp.Body, 1<<20)
	if err != nil {
		return "", resp.Header.Get("X-OAuth-Scopes"), resp.StatusCode, "read GitHub response: " + err.Error()
	}
	if len(data) == 0 {
		return "", resp.Header.Get("X-OAuth-Scopes"), resp.StatusCode, ""
	}
	var body struct {
		Login   string `json:"login"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return "", resp.Header.Get("X-OAuth-Scopes"), resp.StatusCode, "decode GitHub response: " + err.Error()
	}
	return body.Login, resp.Header.Get("X-OAuth-Scopes"), resp.StatusCode, body.Message
}

func diagnoseGitHubStatus(status int, message string) map[string]any {
	switch status {
	case 401:
		return map[string]any{"kind": "github_token_invalid", "suggestion": "regenerate the GitHub credential"}
	case 403:
		return map[string]any{"kind": "github_token_permission_denied", "suggestion": "for fine-grained PATs, grant the selected repository Contents: Read-only or Read and write; organization approval may also be required"}
	case 404:
		return map[string]any{"kind": "github_repo_not_visible", "suggestion": "check owner/name and ensure the token is allowed to access this repository"}
	case 0:
		return map[string]any{"kind": "github_network_error", "message": message, "suggestion": "check outbound network access to api.github.com"}
	default:
		return map[string]any{"kind": "github_unexpected_status", "status": status, "message": message}
	}
}

func diagnoseGitOutput(output string) map[string]any {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "could not read username") && strings.Contains(lower, "terminal prompts disabled"):
		return map[string]any{"kind": "git_auth_missing", "suggestion": "configure a GitHub credential"}
	case strings.Contains(lower, "write access to repository not granted") || strings.Contains(lower, "the requested url returned error: 403"):
		return map[string]any{"kind": "github_token_permission_denied", "suggestion": "for fine-grained PATs, grant repository Contents permission and ensure the repository is selected"}
	case strings.Contains(lower, "authentication failed"):
		return map[string]any{"kind": "github_authentication_failed", "suggestion": "verify the GitHub credential"}
	case strings.Contains(lower, "repository not found"):
		return map[string]any{"kind": "github_repo_not_visible", "suggestion": "check repository URL and token repository access"}
	}
	return nil
}
