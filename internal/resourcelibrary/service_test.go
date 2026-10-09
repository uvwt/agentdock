package resourcelibrary

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/plugin"
	skills "github.com/uvwt/agentdock/internal/skill"
	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func TestService_导出受管Skill且响应不含本地路径(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo-skill")
	writeFile(t, filepath.Join(root, "SKILL.md"), skillDocument("demo-skill", "Use the portable instructions."))
	writeFile(t, filepath.Join(root, "references", "guide.md"), "See SKILL.md.\n")
	service := testService(t, Deps{Lookup: func(kind, name string) (string, error) {
		if kind != KindSkill || name != "demo-skill" {
			t.Fatalf("lookup %s %s", kind, name)
		}
		return root, nil
	}})

	result, err := service.Handle(context.Background(), "POST", []byte(`{"action":"export_prepare","kind":"skill","name":"demo-skill"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result["stream_upload_available"] != false || result["transfer"] != "local_grant_only" {
		t.Fatalf("export transfer = %#v", result)
	}
	if strings.Contains(result["archive_digest"].(string), "/") {
		t.Fatalf("digest leaked a path: %#v", result["archive_digest"])
	}
	for _, file := range result["files"].([]FileEntry) {
		if filepath.IsAbs(file.Path) || strings.Contains(file.Path, "demo-skill/") && strings.Contains(file.Path, t.TempDir()) {
			t.Fatalf("file path = %s", file.Path)
		}
	}
	capabilities, err := service.Handle(context.Background(), "GET", nil)
	if err != nil || capabilities["supported"] != true || capabilities["stream_upload_available"] != false {
		t.Fatalf("capabilities = %#v %v", capabilities, err)
	}
}

func TestService_下载地址失配和摘要失配不会进入安装(t *testing.T) {
	var fetches int
	service := testService(t, Deps{
		Identity: testIdentity,
		Fetch: func(context.Context, string, string, string, string, int64) (string, error) {
			fetches++
			return "sha256:abcd", nil
		},
		SkillInstall: func(context.Context, string, string) (SkillCommit, error) {
			t.Fatal("install was called")
			return SkillCommit{}, nil
		},
	})

	_, err := service.Handle(context.Background(), "POST", []byte(`{
		"action":"install_prepare","kind":"skill","name":"demo-skill","node_id":"node-1",
		"download_url":"https://127.0.0.1/pkg.zip","archive_digest":"sha256:abcd","package_digest":"sha256:1111"
	}`))
	if err == nil || fetches != 0 {
		t.Fatalf("local URL error = %v fetches = %d", err, fetches)
	}

	_, err = service.Handle(context.Background(), "POST", []byte(`{
		"action":"install_prepare","kind":"skill","name":"demo-skill","node_id":"node-1",
		"download_url":"https://evil.example/pkg.zip","archive_digest":"sha256:abcd","package_digest":"sha256:1111"
	}`))
	if err == nil || fetches != 0 {
		t.Fatalf("other origin error = %v fetches = %d", err, fetches)
	}

	_, err = service.Handle(context.Background(), "POST", []byte(`{
		"action":"install_prepare","kind":"skill","name":"other-node","node_id":"node-other",
		"download_url":"https://1.1.1.1/pkg.zip","archive_digest":"sha256:abcd","package_digest":"sha256:1111"
	}`))
	var libraryErr *Error
	if err == nil || !asLibraryError(err, &libraryErr) || libraryErr.Code != "NODE_MISMATCH" || fetches != 0 {
		t.Fatalf("node mismatch = %#v fetches = %d", err, fetches)
	}
}

func TestService_挑战过期重复提交和包被替换都不能安装(t *testing.T) {
	archive, archiveDigest, contentDigest := exportSkillFixture(t, "demo-skill", "First body.")
	var installs int
	current := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	service := testService(t, Deps{
		Now:      func() time.Time { return current },
		Identity: testIdentity,
		Fetch:    fetchFile(archive, archiveDigest),
		SkillValidate: func(_ context.Context, source, digest string) (SkillPreview, error) {
			return validateSkillArchive(t, source, digest, "demo-skill", contentDigest)
		},
		SkillInstall: func(context.Context, string, string) (SkillCommit, error) {
			installs++
			return SkillCommit{Name: "demo-skill", ContentDigest: contentDigest, Changed: true}, nil
		},
	})

	prepared := prepareSkill(t, service, archiveDigest, contentDigest)
	challenge := prepared["challenge"].(string)
	current = current.Add(3 * time.Minute)
	if _, err := commitSkill(service, challenge, archiveDigest, contentDigest, ""); err == nil || !errorCode(err, "CHALLENGE_EXPIRED") {
		t.Fatalf("expired commit = %v", err)
	}
	if installs != 0 {
		t.Fatalf("expired challenge installed %d times", installs)
	}

	current = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	prepared = prepareSkill(t, service, archiveDigest, contentDigest)
	challenge = prepared["challenge"].(string)
	staged := service.pending[challenge]
	if err := os.WriteFile(staged.ArchivePath, []byte("replaced"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := commitSkill(service, challenge, archiveDigest, contentDigest, ""); err == nil || !errorCode(err, "ARCHIVE_CHANGED") {
		t.Fatalf("replaced archive commit = %v", err)
	}
	if installs != 0 {
		t.Fatalf("replaced archive installed %d times", installs)
	}

	prepared = prepareSkill(t, service, archiveDigest, contentDigest)
	challenge = prepared["challenge"].(string)
	if _, err := commitSkill(service, challenge, archiveDigest, contentDigest, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := commitSkill(service, challenge, archiveDigest, contentDigest, ""); err == nil || !errorCode(err, "CHALLENGE_INVALID") {
		t.Fatalf("replayed commit = %v", err)
	}
	if installs != 1 {
		t.Fatalf("installs = %d, want 1", installs)
	}
}

func TestService_审核令牌不匹配不会调用原生安装(t *testing.T) {
	home := t.TempDir()
	manager := newPluginManager(t, home)
	archive, archiveDigest, packageDigest, reviewToken := pluginArchive(t, "1.0.0", "First body.")
	var updates int
	service := testService(t, Deps{
		Identity: testIdentity,
		Fetch:    fetchFile(archive, archiveDigest),
		PluginValidate: func(_ context.Context, source string) (PluginPreview, error) {
			return validatePluginArchive(t, manager, source)
		},
		PluginInstalled: func(string) (bool, error) { return true, nil },
		PluginUpdate: func(_ context.Context, source, token string) (PluginCommit, error) {
			updates++
			if token != reviewToken {
				t.Fatalf("token = %s", token)
			}
			result, err := manager.UpdateReviewedSource(context.Background(), source, token, nil)
			if err != nil {
				return PluginCommit{}, err
			}
			return PluginCommit{Name: result.Name, Version: result.Version, PackageDigest: result.PackageDigest, Changed: result.Changed}, nil
		},
	})
	installPluginVersion(t, manager, "1.0.0", "Already installed.")

	prepared := preparePlugin(t, service, archiveDigest, packageDigest)
	if prepared["operation"] != OperationUpdate {
		t.Fatalf("operation = %#v", prepared["operation"])
	}
	challenge := prepared["challenge"].(string)
	if _, err := commitPlugin(service, challenge, archiveDigest, packageDigest, "review-v1:sha256:deadbeef"); err == nil || !errorCode(err, "REVIEW_TOKEN_MISMATCH") {
		t.Fatalf("wrong token = %v", err)
	}
	if updates != 0 {
		t.Fatalf("wrong token called update %d times", updates)
	}
	if _, err := commitPlugin(service, challenge, archiveDigest, packageDigest, reviewToken); err == nil || !strings.Contains(err.Error(), "different package digest") {
		t.Fatalf("version conflict = %v", err)
	}
	if updates != 1 {
		t.Fatalf("updates = %d", updates)
	}
	installed, err := manager.Inspect("demo-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Version != "1.0.0" || installed.PackageDigest == packageDigest {
		t.Fatalf("installed plugin changed: %#v", installed.State)
	}
}

func TestService_摘要不匹配拒绝下载结果(t *testing.T) {
	archive, archiveDigest, _ := exportSkillFixture(t, "demo-skill", "Body.")
	service := testService(t, Deps{
		Identity: testIdentity,
		Fetch:    fetchFile(archive, archiveDigest),
		SkillValidate: func(context.Context, string, string) (SkillPreview, error) {
			t.Fatal("validator ran after digest mismatch")
			return SkillPreview{}, nil
		},
	})
	_, err := service.Handle(context.Background(), "POST", []byte(`{
		"action":"install_prepare","kind":"skill","name":"demo-skill","node_id":"node-1",
		"download_url":"https://1.1.1.1/packages/demo-skill.zip",
		"archive_digest":"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"package_digest":"sha256:1111111111111111111111111111111111111111111111111111111111111111"
	}`))
	if err == nil || !errorCode(err, "DIGEST_MISMATCH") {
		t.Fatalf("digest mismatch = %v", err)
	}
}

func testService(t *testing.T, deps Deps) *Service {
	t.Helper()
	service, err := New(t.TempDir(), deps)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func testIdentity() (DeviceIdentity, error) {
	return DeviceIdentity{Endpoint: "https://1.1.1.1", NodeID: "node-1", DeviceToken: "device-token"}, nil
}

func fetchFile(source, digest string) func(context.Context, string, string, string, string, int64) (string, error) {
	return func(_ context.Context, _, _, _ string, destination string, limit int64) (string, error) {
		data, err := os.ReadFile(source)
		if err != nil {
			return "", err
		}
		if int64(len(data)) > limit {
			return "", failed("PACKAGE_INVALID", "validation", "package exceeds limit")
		}
		if err := os.WriteFile(destination, data, 0o600); err != nil {
			return "", err
		}
		return digest, nil
	}
}

func exportSkillFixture(t *testing.T, name, body string) (string, string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	writeFile(t, filepath.Join(root, "SKILL.md"), skillDocument(name, body))
	destination := filepath.Join(t.TempDir(), name+".zip")
	exported, err := ExportDirectory(root, KindSkill, destination)
	if err != nil {
		t.Fatal(err)
	}
	return destination, exported.ArchiveDigest, exported.ContentDigest
}

func skillDocument(name, body string) string {
	return "---\nname: " + name + "\ndescription: Portable demo skill.\n---\n\n# Demo\n\n" + body + "\n"
}

func prepareSkill(t *testing.T, service *Service, archiveDigest, contentDigest string) map[string]any {
	t.Helper()
	body := `{
		"action":"install_prepare","kind":"skill","name":"demo-skill","node_id":"node-1",
		"download_url":"https://1.1.1.1/packages/demo-skill.zip",
		"archive_digest":"` + archiveDigest + `",
		"package_digest":"` + contentDigest + `"
	}`
	result, err := service.Handle(context.Background(), "POST", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if result["valid"] != true {
		t.Fatalf("prepare = %#v", result)
	}
	return result
}

func commitSkill(service *Service, challenge, archiveDigest, contentDigest, reviewToken string) (map[string]any, error) {
	body := `{
		"action":"install_commit","kind":"skill","name":"demo-skill","node_id":"node-1",
		"operation":"install","challenge":"` + challenge + `",
		"archive_digest":"` + archiveDigest + `","package_digest":"` + contentDigest + `"
	}`
	if reviewToken != "" {
		body = strings.TrimSuffix(body, "\n\t}") + `,"review_token":"` + reviewToken + `"}`
	}
	return service.Handle(context.Background(), "POST", []byte(body))
}

func validateSkillArchive(t *testing.T, source, digest, name, contentDigest string) (SkillPreview, error) {
	t.Helper()
	state, err := skillstate.New(filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := skills.New(state)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Validate(context.Background(), skills.ValidateRequest{Source: source, DigestSHA256: digest})
	if err != nil {
		return SkillPreview{}, err
	}
	if result.ContentDigest != contentDigest {
		t.Fatalf("content digest = %s, want %s", result.ContentDigest, contentDigest)
	}
	issues := make([]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		issues = append(issues, issue.Message)
	}
	return SkillPreview{
		Valid: result.Valid, Name: result.Document.Name, SourceDigest: result.SourceDigest,
		ContentDigest: result.ContentDigest, Issues: issues,
	}, nil
}

func newPluginManager(t *testing.T, home string) *plugin.Manager {
	t.Helper()
	manager, err := plugin.NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func installPluginVersion(t *testing.T, manager *plugin.Manager, version, body string) {
	t.Helper()
	root := writePluginDir(t, version, body)
	review := manager.ValidateSource(context.Background(), root)
	if !review.Valid {
		t.Fatalf("review = %#v", review)
	}
	result, err := manager.InstallReviewedSource(context.Background(), root, true, review.ReviewToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.FinalizeActivation(result.Name); err != nil {
		t.Fatal(err)
	}
}

func pluginArchive(t *testing.T, version, body string) (string, string, string, string) {
	t.Helper()
	root := writePluginDir(t, version, body)
	destination := filepath.Join(t.TempDir(), "plugin.zip")
	exported, err := ExportDirectory(root, KindPlugin, destination)
	if err != nil {
		t.Fatal(err)
	}
	manager := newPluginManager(t, t.TempDir())
	review := manager.ValidateSource(context.Background(), destination)
	if !review.Valid || review.PackageDigest != exported.ContentDigest {
		t.Fatalf("review = %#v export digest %s", review, exported.ContentDigest)
	}
	return destination, exported.ArchiveDigest, review.PackageDigest, review.ReviewToken
}

func writePluginDir(t *testing.T, version, body string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "plugin-"+version)
	writeFile(t, filepath.Join(root, "plugin.json"), `{"name":"demo-plugin","version":"`+version+`","description":"Demo plugin"}`)
	writeFile(t, filepath.Join(root, "skills", "demo-skill", "SKILL.md"), skillDocument("demo-skill", body))
	return root
}

func validatePluginArchive(t *testing.T, manager *plugin.Manager, source string) (PluginPreview, error) {
	t.Helper()
	review := manager.ValidateSource(context.Background(), source)
	return PluginPreview{
		Valid: review.Valid, Name: review.Name, Version: review.Version, PackageDigest: review.PackageDigest,
		ReviewToken: review.ReviewToken, Warnings: review.Warnings, Issues: review.Issues, Format: review.Format,
	}, nil
}

func preparePlugin(t *testing.T, service *Service, archiveDigest, packageDigest string) map[string]any {
	t.Helper()
	body := `{
		"action":"install_prepare","kind":"plugin","name":"demo-plugin","node_id":"node-1",
		"download_url":"https://1.1.1.1/packages/demo-plugin.zip",
		"archive_digest":"` + archiveDigest + `",
		"package_digest":"` + packageDigest + `"
	}`
	result, err := service.Handle(context.Background(), "POST", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func commitPlugin(service *Service, challenge, archiveDigest, packageDigest, reviewToken string) (map[string]any, error) {
	body := `{
		"action":"install_commit","kind":"plugin","name":"demo-plugin","node_id":"node-1",
		"operation":"update","challenge":"` + challenge + `",
		"archive_digest":"` + archiveDigest + `","package_digest":"` + packageDigest + `",
		"review_token":"` + reviewToken + `"
	}`
	return service.Handle(context.Background(), "POST", []byte(body))
}

func errorCode(err error, code string) bool {
	libraryErr, ok := err.(*Error)
	return ok && libraryErr.Code == code
}

func asLibraryError(err error, target **Error) bool {
	libraryErr, ok := err.(*Error)
	if !ok {
		return false
	}
	*target = libraryErr
	return true
}
