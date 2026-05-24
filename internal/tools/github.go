package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (r *Runtime) configureGitHubToken(args map[string]any) (Result, error) {
	p, err := r.ws.ResolveExisting(stringArg(args, "env_file", ".env"))
	if err != nil {
		return nil, err
	}
	values, err := parseEnvFile(p.Abs)
	if err != nil {
		return nil, err
	}
	token := firstNonEmpty(values, "GITHUB_TOKEN", "GH_TOKEN", "GITHUB_PAT", "TOKEN")
	if token == "" {
		return Result{"ok": false, "token_found": false, "expected_one_of": []string{"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_PAT", "TOKEN"}}, nil
	}
	username := stringArg(args, "username", "")
	if username == "" {
		username = firstNonEmpty(values, "GITHUB_USERNAME", "GITHUB_USER")
	}
	if username == "" {
		username = "x-access-token"
	}
	if err := writeGitCredential(r.ws.Root(), username, token); err != nil {
		return nil, err
	}
	return Result{"ok": true, "token_found": true, "username": username, "credential_helper": "store", "password_stored": true, "home": r.ws.Root()}, nil
}

func (r *Runtime) checkGitHubRepoAccess(args map[string]any) (Result, error) {
	repo := normalizeGitHubRepo(stringArg(args, "repo", stringArg(args, "repository", "")))
	if repo == "" || !strings.Contains(repo, "/") {
		return nil, toolError("INVALID_ARGUMENT", "repo is required as owner/name or GitHub URL", "validation")
	}
	token, username, err := githubCredential(r.ws.Root())
	if err != nil {
		return Result{"ok": false, "credential_found": false, "diagnostic": map[string]any{"kind": "git_auth_missing", "suggestion": "run configure_github_token first"}}, nil
	}
	client := &http.Client{Timeout: time.Duration(intArg(args, "timeout_ms", 15000)) * time.Millisecond}
	result := Result{"ok": true, "credential_found": true, "username": username, "repo": repo}
	login, scopes, authStatus, authMessage := githubGet(client, token, "https://api.github.com/user")
	result["auth_status"] = authStatus
	result["auth_login"] = login
	result["oauth_scopes"] = scopes
	if authMessage != "" {
		result["auth_message"] = authMessage
	}
	_, _, repoStatus, repoMessage := githubGet(client, token, "https://api.github.com/repos/"+repo)
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

func (r *Runtime) githubCreateRepo(args map[string]any) (Result, error) {
	cred, username, err := githubCredential(r.ws.Root())
	if err != nil {
		return Result{"ok": false, "credential_found": false, "diagnostic": map[string]any{"kind": "git_auth_missing", "suggestion": "run configure_github_token first"}}, nil
	}
	name := stringArg(args, "name", "")
	owner := stringArg(args, "owner", "")
	if repo := normalizeGitHubRepo(stringArg(args, "repo", "")); repo != "" {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) == 2 {
			owner = parts[0]
			name = parts[1]
		} else if name == "" {
			name = repo
		}
	}
	if name == "" || strings.Contains(name, "/") {
		return nil, toolError("INVALID_ARGUMENT", "name is required and must not contain '/'", "validation")
	}
	client := &http.Client{Timeout: time.Duration(intArg(args, "timeout_ms", 15000)) * time.Millisecond}
	login, scopes, authStatus, authMessage := githubGet(client, cred, "https://api.github.com/user")
	if authStatus >= 400 || authStatus == 0 {
		return Result{"ok": false, "credential_found": true, "username": username, "auth_status": authStatus, "auth_login": login, "oauth_scopes": scopes, "auth_message": authMessage, "diagnostic": diagnoseGitHubStatus(authStatus, authMessage)}, nil
	}
	endpoint := "https://api.github.com/user/repos"
	if owner != "" && login != "" && owner != login {
		endpoint = "https://api.github.com/orgs/" + owner + "/repos"
	}
	payload := map[string]any{"name": name, "private": boolArg(args, "private", true), "description": stringArg(args, "description", ""), "auto_init": boolArg(args, "auto_init", false)}
	status, body, message := githubPostJSON(client, cred, endpoint, payload)
	result := Result{"ok": status == 201, "credential_found": true, "username": username, "auth_login": login, "oauth_scopes": scopes, "status": status, "created": status == 201}
	if message != "" {
		result["message"] = message
	}
	for _, key := range []string{"full_name", "html_url", "clone_url", "ssh_url", "default_branch"} {
		if value, ok := body[key].(string); ok && value != "" {
			result[key] = redactSecrets(value, nil)
		}
	}
	if status != 201 {
		result["diagnostic"] = diagnoseGitHubCreateStatus(status, message)
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

func parseEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimPrefix(s, "export ")
		parts := strings.SplitN(s, "=", 2)
		if len(parts) != 2 {
			continue
		}
		values[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), "'\"")
	}
	return values, nil
}

func firstNonEmpty(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := values[key]; value != "" {
			return value
		}
	}
	return ""
}

func writeGitCredential(home, username, token string) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	// 只确保 credential.helper=store 存在，不覆盖已有 .gitconfig。
	// 背景：这个工作区可能同时保存 user、core、url rewrite 等项目配置；
	// 如果每次配置 token 都重写整份 .gitconfig，会把用户已有 Git 配置清掉。
	if err := ensureGitCredentialHelper(filepath.Join(home, ".gitconfig")); err != nil {
		return err
	}
	credPath := filepath.Join(home, ".git-credentials")
	lines := []string{}
	if data, err := os.ReadFile(credPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" || strings.Contains(line, "github.com") {
				continue
			}
			lines = append(lines, line)
		}
	}
	u := &url.URL{Scheme: "https", Host: "github.com", User: url.UserPassword(username, token)}
	lines = append(lines, u.String())
	if err := os.WriteFile(credPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(credPath, 0o600)
}

func ensureGitCredentialHelper(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(data)
	if strings.Contains(text, "helper = store") || strings.Contains(text, "helper=store") {
		return nil
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += "[credential]\n\thelper = store\n"
	return os.WriteFile(configPath, []byte(text), 0o644)
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

func githubGet(client *http.Client, token, endpoint string) (login, scopes string, status int, message string) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", "", 0, err.Error()
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "coding-tools-mcp")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, err.Error()
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if value, ok := body["login"].(string); ok {
		login = value
	}
	if value, ok := body["message"].(string); ok {
		message = value
	}
	return login, resp.Header.Get("X-OAuth-Scopes"), resp.StatusCode, message
}

func githubPostJSON(client *http.Client, token, endpoint string, payload map[string]any) (int, map[string]any, string) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err.Error()
	}
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(data))
	if err != nil {
		return 0, nil, err.Error()
	}
	req.Header.Set("Auth"+"orization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "coding-tools-mcp")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err.Error()
	}
	defer resp.Body.Close()
	body := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	message := ""
	if value, ok := body["message"].(string); ok {
		message = value
	}
	return resp.StatusCode, body, message
}

func diagnoseGitHubStatus(status int, message string) map[string]any {
	switch status {
	case 401:
		return map[string]any{"kind": "github_token_invalid", "suggestion": "regenerate the token and run configure_github_token again"}
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

func diagnoseGitHubCreateStatus(status int, message string) map[string]any {
	switch status {
	case 201:
		return map[string]any{"kind": "github_repo_created"}
	case 401:
		return map[string]any{"kind": "github_token_invalid", "suggestion": "regenerate the token and run configure_github_token again"}
	case 403:
		return map[string]any{"kind": "github_create_repo_denied", "suggestion": "grant permission to create repositories for the user or organization"}
	case 422:
		return map[string]any{"kind": "github_repo_create_validation_failed", "message": message, "suggestion": "check whether the repository already exists or the name is invalid"}
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
		return map[string]any{"kind": "git_auth_missing", "suggestion": "configure a GitHub credential or run configure_github_token"}
	case strings.Contains(lower, "write access to repository not granted") || strings.Contains(lower, "the requested url returned error: 403"):
		return map[string]any{"kind": "github_token_permission_denied", "suggestion": "for fine-grained PATs, grant repository Contents permission and ensure the repository is selected"}
	case strings.Contains(lower, "authentication failed"):
		return map[string]any{"kind": "github_authentication_failed", "suggestion": "verify the token value and rerun configure_github_token"}
	case strings.Contains(lower, "repository not found"):
		return map[string]any{"kind": "github_repo_not_visible", "suggestion": "check repository URL and token repository access"}
	}
	return nil
}
