package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"filippo.io/age"
)

const (
	privateNotesPlainDir      = "notes"
	privateNotesEncryptedDir  = "encrypted"
	privateNotesKeyDir        = ".keys"
	privateNotesIdentityFile  = "private-notes-age-identity.txt"
	privateNotesRecipientFile = "recipients.txt"
)

type privateNoteSummary struct {
	Path           string   `json:"path"`
	EncryptedPath  string   `json:"encrypted_path"`
	Title          string   `json:"title,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	ContainsSecret bool     `json:"contains_secret"`
}

func (r *Runtime) privateNotesSearch(ctx context.Context, args map[string]any) (Result, error) {
	query := strings.TrimSpace(stringArg(args, "query", ""))
	if query == "" {
		return nil, toolError("MISSING_QUERY", "query is required", "validation")
	}
	root, err := r.privateNotesRoot()
	if err != nil {
		return nil, err
	}
	maxResults := intArg(args, "max_results", 8)
	if maxResults <= 0 {
		maxResults = 8
	}
	terms := privateNoteTerms(query)
	var results []map[string]any
	walkRoot := filepath.Join(root, privateNotesPlainDir)
	_ = filepath.WalkDir(walkRoot, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		contentBytes, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		content := string(contentBytes)
		score := privateNoteScore(rel, content, terms)
		if score <= 0 {
			return nil
		}
		summary := privateNoteExtractSummary(rel, content)
		results = append(results, map[string]any{
			"path":            rel,
			"encrypted_path":  privateNoteEncryptedRel(rel),
			"title":           summary.Title,
			"summary":         summary.Summary,
			"tags":            summary.Tags,
			"contains_secret": summary.ContainsSecret,
			"score":           score,
			"snippet":         redactPrivateNoteSnippet(privateNoteSnippet(content, terms)),
		})
		return nil
	})
	sort.Slice(results, func(i, j int) bool {
		si, _ := results[i]["score"].(int)
		sj, _ := results[j]["score"].(int)
		if si == sj {
			return fmt.Sprint(results[i]["path"]) < fmt.Sprint(results[j]["path"])
		}
		return si > sj
	})
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return Result{"ok": true, "query": query, "root": root, "results": results, "count": len(results), "redacted": true, "policy": "search returns redacted snippets only; use private_notes_read to read full plaintext"}, nil
}

func (r *Runtime) privateNotesRead(ctx context.Context, args map[string]any) (Result, error) {
	root, err := r.privateNotesRoot()
	if err != nil {
		return nil, err
	}
	rel, err := privateNoteNormalizePath(stringArg(args, "path", ""), stringArg(args, "category", ""), stringArg(args, "title", ""))
	if err != nil {
		return nil, err
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, toolErrorDetails("PRIVATE_NOTE_READ_FAILED", err.Error(), "filesystem", map[string]any{"path": rel})
	}
	maxBytes := intArg(args, "max_bytes", 256000)
	body := string(content)
	truncated := false
	if maxBytes > 0 && len(body) > maxBytes {
		body = body[:maxBytes]
		truncated = true
	}
	summary := privateNoteExtractSummary(rel, string(content))
	return Result{"ok": true, "root": root, "path": rel, "encrypted_path": privateNoteEncryptedRel(rel), "content": body, "truncated": truncated, "contains_secret": summary.ContainsSecret, "policy": "private_notes_read returns plaintext by design"}, nil
}

func (r *Runtime) privateNotesWrite(ctx context.Context, args map[string]any) (Result, error) {
	if !boolArg(args, "confirmed", false) {
		return nil, toolError("CONFIRMATION_REQUIRED", "private_notes_write requires confirmed=true", "validation")
	}
	content := stringArg(args, "content", "")
	if strings.TrimSpace(content) == "" {
		return nil, toolError("MISSING_CONTENT", "content is required", "validation")
	}
	root, err := r.privateNotesRoot()
	if err != nil {
		return nil, err
	}
	if err := initPrivateNotesTree(root); err != nil {
		return nil, err
	}
	rel, err := privateNoteNormalizePath(stringArg(args, "path", ""), stringArg(args, "category", ""), stringArg(args, "title", ""))
	if err != nil {
		return nil, err
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err == nil && !boolArg(args, "overwrite", false) {
		return nil, toolErrorDetails("PRIVATE_NOTE_EXISTS", "private note already exists; pass overwrite=true", "validation", map[string]any{"path": rel})
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, err
	}
	finalContent := privateNoteWithFrontmatter(rel, content, args)
	if err := os.WriteFile(abs, []byte(finalContent), 0o600); err != nil {
		return nil, err
	}
	if err := os.Chmod(abs, 0o600); err != nil {
		return nil, err
	}
	encRel, err := encryptPrivateNote(root, rel)
	if err != nil {
		_ = os.Remove(abs)
		return nil, err
	}
	return Result{"ok": true, "root": root, "path": rel, "encrypted_path": encRel, "written": true, "encrypted": true, "algorithm": "age/X25519", "policy": "age encrypted backup is mandatory and cannot be skipped"}, nil
}

func (r *Runtime) privateNotesStatus(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "check")))
	root, err := r.privateNotesRoot()
	if err != nil {
		return nil, err
	}
	switch action {
	case "list":
		items, err := listPrivateNotes(root)
		if err != nil {
			return nil, err
		}
		return Result{"ok": true, "action": "list", "root": root, "notes": items, "count": len(items)}, nil
	case "check", "status":
		items, err := listPrivateNotes(root)
		if err != nil {
			return nil, err
		}
		missing := []string{}
		for _, item := range items {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(item.EncryptedPath))); err != nil {
				missing = append(missing, item.EncryptedPath)
			}
		}
		return Result{"ok": len(missing) == 0, "action": "check", "root": root, "notes_count": len(items), "missing_encrypted": missing, "policy": "notes/ is plaintext and ignored by git; encrypted/ stores mandatory .md.age backups"}, nil
	default:
		return nil, toolErrorDetails("INVALID_PRIVATE_NOTES_ACTION", "unsupported private_notes_status action", "validation", map[string]any{"action": action, "allowed": []string{"check", "list"}})
	}
}

func (r *Runtime) privateNotesMaintain(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "sync-encrypted")))
	root, err := r.privateNotesRoot()
	if err != nil {
		return nil, err
	}
	switch action {
	case "init", "init-encryption":
		if err := initPrivateNotesTree(root); err != nil {
			return nil, err
		}
		identity, created, err := privateNotesEnsureAgeIdentity(root)
		if err != nil {
			return nil, err
		}
		return Result{"ok": true, "action": "init", "root": root, "recipient": identity.Recipient().String(), "identity_created": created, "algorithm": "age/X25519"}, nil
	case "sync", "encrypt", "encrypt-all", "sync-encrypted", "migrate-enc-to-age":
		if err := initPrivateNotesTree(root); err != nil {
			return nil, err
		}
		count, err := syncPrivateNotesEncrypted(root)
		if err != nil {
			return nil, err
		}
		removed := 0
		if action == "migrate-enc-to-age" {
			removed, err = removeLegacyPrivateNoteBackups(root)
			if err != nil {
				return nil, err
			}
		}
		return Result{"ok": true, "action": action, "root": root, "encrypted_count": count, "legacy_removed_count": removed, "algorithm": "age/X25519"}, nil
	default:
		return nil, toolErrorDetails("INVALID_PRIVATE_NOTES_ACTION", "unsupported private_notes_maintain action", "validation", map[string]any{"action": action, "allowed": []string{"init", "sync-encrypted", "encrypt-all", "migrate-enc-to-age"}})
	}
}

func (r *Runtime) privateNotesRoot() (string, error) {
	return filepath.Join(r.cfg.AgentDockHome, "private-notes"), nil
}

func initPrivateNotesTree(root string) error {
	for _, dir := range []string{"notes/services", "notes/accounts", "notes/recovery", "notes/networking", "encrypted/services", "encrypted/accounts", "encrypted/recovery", "encrypted/networking", "templates", "scripts", privateNotesKeyDir} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o700); err != nil {
			return err
		}
	}
	files := map[string]string{
		"README.md": `# private-notes

private-notes 是用户个人私密资料库。人和工具只维护 notes/ 明文主目录，encrypted/ 由工具自动生成 age 加密备份。

- notes/：本地明文，允许保存私密凭据、恢复码和私密 runbook；不提交 Git。
- encrypted/：notes/ 的强制 age 加密备份，文件后缀为 .md.age，可提交 Git。
- private_notes_search：只返回脱敏片段。
- private_notes_read：明确读取私密笔记时返回明文。
`,
		"RULES.md": `# private-notes 规则

1. 只把明文私密资料写入 notes/。
2. private_notes_write 必须同步生成 encrypted 目录下的 .md.age age 加密备份；不允许跳过。
3. notes/ 和 .keys/ 不得提交 Git。
4. encrypted/ 可以提交 Git，但只能存 age 加密产物。
5. RecallDock 只记录索引和路径，不记录 secret 明文。
`,
		".gitignore": "notes/\n.keys/\n*.tmp\n*.bak\n*.enc\n.DS_Store\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func privateNoteNormalizePath(raw, category, title string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		cat := privateNoteSafeSegment(category)
		if cat == "" {
			cat = "services"
		}
		slug := notesSlug(title)
		if slug == "" {
			return "", toolError("MISSING_PATH", "path or title is required", "validation")
		}
		raw = filepath.ToSlash(filepath.Join(privateNotesPlainDir, cat, slug+".md"))
	}
	raw = strings.TrimPrefix(filepath.ToSlash(raw), "/")
	if !strings.HasPrefix(raw, privateNotesPlainDir+"/") {
		raw = privateNotesPlainDir + "/" + raw
	}
	raw = filepath.ToSlash(filepath.Clean(raw))
	if raw == "." || strings.HasPrefix(raw, "../") || strings.Contains(raw, "/../") || filepath.IsAbs(raw) || !strings.HasSuffix(raw, ".md") {
		return "", toolErrorDetails("INVALID_PRIVATE_NOTE_PATH", "private note path must be a safe .md path under notes/", "validation", map[string]any{"path": raw})
	}
	for _, part := range strings.Split(raw, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return "", toolErrorDetails("INVALID_PRIVATE_NOTE_PATH", "hidden or empty path segments are not allowed", "validation", map[string]any{"path": raw})
		}
	}
	return raw, nil
}

func privateNoteSafeSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if r == '-' || r == '_' || r == ' ' || r == '/' {
			if b.Len() > 0 {
				b.WriteByte('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func privateNoteWithFrontmatter(rel, content string, args map[string]any) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "---\n") {
		return ensureTrailingNewline(trimmed)
	}
	title := strings.TrimSpace(stringArg(args, "title", ""))
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(rel), ".md")
	}
	summary := strings.TrimSpace(stringArg(args, "summary", ""))
	tags := normalizedMemoryCardTags(stringSliceArg(args, "tags"))
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + yamlSingleLine(title) + "\n")
	b.WriteString("type: private-note\n")
	b.WriteString("contains_secret: ")
	if hasPrivateNoteSecretMarker(content) {
		b.WriteString("true\n")
	} else {
		b.WriteString("false\n")
	}
	if summary != "" {
		b.WriteString("summary: " + yamlSingleLine(summary) + "\n")
	}
	if len(tags) > 0 {
		b.WriteString("tags: " + strings.Join(tags, ",") + "\n")
	}
	b.WriteString("created_at: " + time.Now().Format(time.RFC3339) + "\n")
	b.WriteString("---\n\n")
	b.WriteString(trimmed)
	b.WriteString("\n")
	return b.String()
}

func privateNotesEnsureAgeIdentity(root string) (*age.X25519Identity, bool, error) {
	identityPath := filepath.Join(root, privateNotesKeyDir, privateNotesIdentityFile)
	if data, err := os.ReadFile(identityPath); err == nil {
		identity, err := age.ParseX25519Identity(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, false, toolError("PRIVATE_NOTES_AGE_IDENTITY_INVALID", "private notes age identity is invalid", "configuration")
		}
		if err := privateNotesWriteRecipientFile(root, identity.Recipient().String()); err != nil {
			return nil, false, err
		}
		return identity, false, nil
	}
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(identityPath), 0o700); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(identityPath, []byte(identity.String()+"\n"), 0o600); err != nil {
		return nil, false, err
	}
	if err := privateNotesWriteRecipientFile(root, identity.Recipient().String()); err != nil {
		return nil, false, err
	}
	return identity, true, nil
}

func privateNotesWriteRecipientFile(root, recipient string) error {
	recipientPath := filepath.Join(root, privateNotesKeyDir, privateNotesRecipientFile)
	if err := os.MkdirAll(filepath.Dir(recipientPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(recipientPath, []byte(recipient+"\n"), 0o600)
}

func privateNotesAgeRecipients(root string) ([]age.Recipient, error) {
	raw := []string{}
	if value := strings.TrimSpace(os.Getenv("AGENTDOCK_PRIVATE_NOTES_AGE_RECIPIENT")); value != "" {
		raw = append(raw, privateNotesRecipientLines(value)...)
	}
	if file := strings.TrimSpace(os.Getenv("AGENTDOCK_PRIVATE_NOTES_AGE_RECIPIENTS_FILE")); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		raw = append(raw, privateNotesRecipientLines(string(data))...)
	}
	if len(raw) == 0 {
		data, err := os.ReadFile(filepath.Join(root, privateNotesKeyDir, privateNotesRecipientFile))
		if err == nil {
			raw = append(raw, privateNotesRecipientLines(string(data))...)
		}
	}
	if len(raw) == 0 {
		return nil, toolError("PRIVATE_NOTES_AGE_RECIPIENT_MISSING", "private notes age recipient is missing; run private_notes_maintain action=init-encryption first", "configuration")
	}
	recipients := make([]age.Recipient, 0, len(raw))
	for _, value := range raw {
		recipient, err := age.ParseX25519Recipient(value)
		if err != nil {
			return nil, toolErrorDetails("PRIVATE_NOTES_AGE_RECIPIENT_INVALID", err.Error(), "configuration", map[string]any{"recipient": "<redacted>"})
		}
		recipients = append(recipients, recipient)
	}
	return recipients, nil
}

func privateNotesRecipientLines(value string) []string {
	lines := []string{}
	for _, line := range strings.FieldsFunc(value, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' || r == ';' }) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return uniqueStrings(lines)
}

func encryptPrivateNote(root, rel string) (string, error) {
	recipients, err := privateNotesAgeRecipients(root)
	if err != nil {
		return "", err
	}
	plainPath := filepath.Join(root, filepath.FromSlash(rel))
	plain, err := os.ReadFile(plainPath)
	if err != nil {
		return "", err
	}
	var encrypted bytes.Buffer
	writer, err := age.Encrypt(&encrypted, recipients...)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(writer, bytes.NewReader(plain)); err != nil {
		_ = writer.Close()
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	encRel := privateNoteEncryptedRel(rel)
	encPath := filepath.Join(root, filepath.FromSlash(encRel))
	if err := os.MkdirAll(filepath.Dir(encPath), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(encPath, encrypted.Bytes(), 0o600); err != nil {
		return "", err
	}
	return encRel, nil
}

func privateNoteEncryptedRel(rel string) string {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), privateNotesPlainDir+"/")
	return filepath.ToSlash(filepath.Join(privateNotesEncryptedDir, rel+".age"))
}

func syncPrivateNotesEncrypted(root string) (int, error) {
	count := 0
	walkRoot := filepath.Join(root, privateNotesPlainDir)
	if _, err := os.Stat(walkRoot); os.IsNotExist(err) {
		return 0, nil
	}
	err := filepath.WalkDir(walkRoot, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return walkErr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if _, err := encryptPrivateNote(root, filepath.ToSlash(rel)); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func removeLegacyPrivateNoteBackups(root string) (int, error) {
	removed := 0
	legacyRoot := filepath.Join(root, privateNotesEncryptedDir)
	if _, err := os.Stat(legacyRoot); os.IsNotExist(err) {
		return 0, nil
	}
	err := filepath.WalkDir(legacyRoot, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".enc") {
			return walkErr
		}
		if err := os.Remove(p); err != nil {
			return err
		}
		removed++
		return nil
	})
	return removed, err
}

func listPrivateNotes(root string) ([]privateNoteSummary, error) {
	items := []privateNoteSummary{}
	walkRoot := filepath.Join(root, privateNotesPlainDir)
	if _, err := os.Stat(walkRoot); os.IsNotExist(err) {
		return items, nil
	}
	err := filepath.WalkDir(walkRoot, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return walkErr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		content, _ := os.ReadFile(p)
		summary := privateNoteExtractSummary(filepath.ToSlash(rel), string(content))
		items = append(items, summary)
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, err
}

func privateNoteExtractSummary(rel, content string) privateNoteSummary {
	item := privateNoteSummary{Path: rel, EncryptedPath: privateNoteEncryptedRel(rel), ContainsSecret: hasPrivateNoteSecretMarker(content)}
	if title := regexp.MustCompile(`(?m)^title:\s*(.+)$`).FindStringSubmatch(content); len(title) == 2 {
		item.Title = strings.Trim(strings.TrimSpace(title[1]), `"'`)
	}
	if item.Title == "" {
		for _, line := range strings.Split(content, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "# ") {
				item.Title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "# "))
				break
			}
		}
	}
	if item.Title == "" {
		item.Title = strings.TrimSuffix(filepath.Base(rel), ".md")
	}
	if m := regexp.MustCompile(`(?m)^summary:\s*(.+)$`).FindStringSubmatch(content); len(m) == 2 {
		item.Summary = strings.Trim(strings.TrimSpace(m[1]), `"'`)
	}
	if m := regexp.MustCompile(`(?m)^tags:\s*(.+)$`).FindStringSubmatch(content); len(m) == 2 {
		for _, tag := range strings.Split(m[1], ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				item.Tags = append(item.Tags, tag)
			}
		}
	}
	return item
}

func privateNoteTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') })
	return uniqueStrings(fields)
}

func privateNoteScore(rel, content string, terms []string) int {
	value := strings.ToLower(rel + "\n" + content)
	score := 0
	for _, term := range terms {
		if term == "" {
			continue
		}
		if strings.Contains(strings.ToLower(rel), term) {
			score += 20
		}
		if strings.Contains(value, term) {
			score += 10
		}
	}
	return score
}

func privateNoteSnippet(content string, terms []string) string {
	lower := strings.ToLower(content)
	idx := -1
	for _, term := range terms {
		if term == "" {
			continue
		}
		if i := strings.Index(lower, strings.ToLower(term)); i >= 0 && (idx < 0 || i < idx) {
			idx = i
		}
	}
	if idx < 0 {
		return firstRunes(strings.TrimSpace(content), 220)
	}
	start := idx - 80
	if start < 0 {
		start = 0
	}
	end := idx + 180
	if end > len(content) {
		end = len(content)
	}
	return strings.TrimSpace(content[start:end])
}

func hasPrivateNoteSecretMarker(value string) bool {
	lower := strings.ToLower(value)
	markers := []string{"token", "password", "passwd", "api_key", "apikey", "secret", "authorization: bearer", "private key", "recovery code"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func redactPrivateNoteSnippet(value string) string {
	out := value
	patterns := []struct{ re, repl string }{
		{`(?i)(authorization:\s*bearer\s+)[^\s]+`, `$1<redacted>`},
		{`(?i)(token|api[_-]?key|secret|password|passwd|pwd)\s*[:=]\s*["']?[^"'\s]+["']?`, `$1=<redacted>`},
		{`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`, `<redacted private key>`},
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p.re)
		out = re.ReplaceAllString(out, p.repl)
	}
	out = strings.TrimSpace(out)
	return string(bytes.TrimSpace([]byte(out)))
}
