package plugin

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableProvenanceChangesDigestAndReviewToken(t *testing.T) {
	write := func(root, revision string) {
		t.Helper()
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		writeJSONFile(t, filepath.Join(root, "plugin.json"), map[string]any{
			"$schema": pluginSchemaURI,
			"name":    "provenance-demo",
			"version": "1.0.0",
			"provenance": map[string]any{
				"origin":      "https://github.com/example/plugins",
				"revision":    revision,
				"subdir":      "plugins/demo",
				"vendor_note": map[string]any{"preserved": true},
			},
		})
	}

	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	write(first, "rev-a")
	write(second, "rev-b")

	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	firstReview := manager.Validate(first)
	secondReview := manager.Validate(second)
	if !firstReview.Valid || !secondReview.Valid {
		t.Fatalf("reviews invalid: first=%#v second=%#v", firstReview, secondReview)
	}
	if firstReview.Provenance == nil || firstReview.Provenance.Revision != "rev-a" {
		t.Fatalf("first provenance = %#v", firstReview.Provenance)
	}
	if firstReview.PackageDigest == secondReview.PackageDigest {
		t.Fatal("provenance change did not change package digest")
	}
	if firstReview.ReviewToken == secondReview.ReviewToken {
		t.Fatal("provenance change did not change review token")
	}
}

func TestPortableManifestRejectsInvalidProvenance(t *testing.T) {
	tests := []struct {
		name       string
		provenance map[string]any
		want       string
	}{
		{
			name:       "missing origin",
			provenance: map[string]any{"revision": "rev-a", "vendor_note": true},
			want:       "provenance.origin is required",
		},
		{
			name:       "escaping subdir",
			provenance: map[string]any{"origin": "https://example.com/repo", "subdir": "../escape"},
			want:       "provenance.subdir",
		},
		{
			name:       "HTTP userinfo",
			provenance: map[string]any{"origin": "https://token@example.com/plugins"},
			want:       "must not contain userinfo",
		},
		{
			name:       "HTTP query",
			provenance: map[string]any{"origin": "https://example.com/plugins?token=secret"},
			want:       "must not contain query parameters",
		},
		{
			name:       "control character",
			provenance: map[string]any{"origin": "https://example.com/plugins", "revision": "abc\ndef"},
			want:       "must not contain control characters",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeJSONFile(t, filepath.Join(root, "plugin.json"), map[string]any{
				"$schema":    pluginSchemaURI,
				"name":       "bad-provenance",
				"version":    "1.0.0",
				"provenance": test.provenance,
			})
			if _, err := LoadPackage(root); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadPackage() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestPortableZIPRejectsTraversalAndSymlink(t *testing.T) {
	tests := []struct {
		name string
		add  func(*zip.Writer) error
		want string
	}{
		{
			name: "traversal",
			add: func(writer *zip.Writer) error {
				entry, err := writer.Create("../escape")
				if err != nil {
					return err
				}
				_, err = entry.Write([]byte("escape"))
				return err
			},
			want: "not local",
		},
		{
			name: "symlink",
			add: func(writer *zip.Writer) error {
				header := &zip.FileHeader{Name: "linked"}
				header.SetMode(os.ModeSymlink | 0o777)
				entry, err := writer.CreateHeader(header)
				if err != nil {
					return err
				}
				_, err = entry.Write([]byte("target"))
				return err
			},
			want: "symlink",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "plugin.zip")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			if err := test.add(writer); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}

			destination := t.TempDir()
			if err := extractPluginZip(archive, destination); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("extractPluginZip() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSnapshotRuntimePackageRejectsSourceOutsidePluginStorage(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), ".agentdock"))
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if _, err := manager.Store().SnapshotRuntimePackage(outside); err == nil || !strings.Contains(err.Error(), "inside installed Plugin storage") {
		t.Fatalf("SnapshotRuntimePackage(%q) error = %v", outside, err)
	}
}
