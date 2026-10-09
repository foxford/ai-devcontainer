package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func TestRuntimeReadinessLifecycle(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		done := make(chan error)
		if err := waitRuntime(context.Background(), done, func(context.Context) bool { return true }, time.Second); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("exited", func(t *testing.T) {
		done := make(chan error, 1)
		done <- errors.New("fixture exit")
		if err := waitRuntime(context.Background(), done, func(context.Context) bool { t.Fatal("probe after child exit"); return true }, time.Second); err == nil || !strings.Contains(err.Error(), "fixture exit") {
			t.Fatal(err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		if err := waitRuntime(context.Background(), make(chan error), func(context.Context) bool { return false }, 10*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := waitRuntime(ctx, make(chan error), func(context.Context) bool { return false }, time.Second); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestBannerFindsNearestWorktreeAndRunsProjectHook(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, ".git"), "gitdir: /unneeded-for-banner\n")
	dir := filepath.Join(root, "src", "nested")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if checkoutRoot(dir) != root {
		t.Fatal("worktree .git file not detected")
	}
	var out bytes.Buffer
	if err := Banner(dir, "fixture", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ADC · fixture") || !strings.Contains(out.String(), "adc tools --help") {
		t.Fatal(out.String())
	}
	// The shell extension is exercised by the Linux Docker integration test.
}

func TestTranscriptArchiveRestoreRotationAndDaily(t *testing.T) {
	home := testutil.Home(t)
	dir := filepath.Join(home, ".claude")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	session := filepath.Join(dir, "projects", "repo with spaces", "session.jsonl")
	writeFixture(t, session, "original transcript\n")
	archive, err := snapshotTranscripts(dir, 2)
	if err != nil || archive == "" {
		t.Fatalf("%s: %v", archive, err)
	}
	writeFixture(t, session, "new transcript\n")
	result, err := restoreTranscripts(dir, archive)
	if err != nil {
		t.Fatal(err)
	}
	if readFixture(t, filepath.Join(result, "projects", "repo with spaces", "session.jsonl")) != "original transcript\n" || readFixture(t, session) != "new transcript\n" {
		t.Fatal("restore overwrote current sessions or lost original")
	}
	for index := 0; index < 2; index++ {
		if _, err := snapshotTranscripts(dir, 2); err != nil {
			t.Fatal(err)
		}
	}
	archives, err := transcriptArchives(filepath.Join(dir, "transcript-backups"))
	if err != nil || len(archives) != 2 {
		t.Fatalf("rotation: %d %v", len(archives), err)
	}
	if err := ClaudeSnapshot([]string{"daily"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	after, err := transcriptArchives(filepath.Join(dir, "transcript-backups"))
	if err != nil || len(after) != 2 || after[0].path != archives[0].path {
		t.Fatal("daily duplicated a recent archive")
	}
	settings := filepath.Join(dir, "settings.json")
	writeFixture(t, settings, `{"cleanupPeriodDays":7,"personal":"keep"}`)
	if err := ClaudeSnapshot([]string{"retention"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if readFixture(t, settings) != `{"cleanupPeriodDays":7,"personal":"keep"}` {
		t.Fatal("retention changed explicit setting")
	}
	writeFixture(t, settings, `{"personal":"keep"}`)
	if err := ClaudeSnapshot([]string{"retention"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFixture(t, settings), "3650") || !strings.Contains(readFixture(t, settings), "keep") {
		t.Fatal("retention lost other settings")
	}
}

func TestTranscriptRestoreRejectsTraversalAndLinks(t *testing.T) {
	for _, header := range []*tar.Header{{Name: "projects/../../escape", Typeflag: tar.TypeReg, Size: 1}, {Name: "projects/link", Typeflag: tar.TypeSymlink, Linkname: "../../escape"}, {Name: "outside", Typeflag: tar.TypeReg, Size: 1}} {
		t.Run(header.Name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "bad.tar.gz")
			var data bytes.Buffer
			gz := gzip.NewWriter(&data)
			archive := tar.NewWriter(gz)
			if err := archive.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if header.Size > 0 {
				if _, err := archive.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "claude")
			if _, err := restoreTranscripts(dir, path); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatal("partial restore left behind")
			}
			if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
				t.Fatal("archive escaped destination")
			}
		})
	}
}

func TestGraphPathLabelsAndNativeHTTP(t *testing.T) {
	data := []byte(`{"nodes":[{"community":0,"source_file":"packages/sdk/src/a.ts"},{"community":0,"source_file":"packages/sdk/src/b.ts"},{"community":1,"source_file":"packages/config/package.json"},{"community":2,"source_file":"packages/a/a.ts"},{"community":2,"source_file":"packages/b/b.ts"},{"community":3}]}`)
	labels, err := graphLabels(data)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"0": "sdk/src", "1": "config", "2": "a · b", "3": "Community 3"} {
		if labels[key] != want {
			t.Fatalf("%s = %q, want %q", key, labels[key], want)
		}
	}
	root := t.TempDir()
	dir := filepath.Join(root, "graphify-out")
	writeFixture(t, filepath.Join(dir, "graph.json"), string(data))
	var args []string
	err = GraphLabels(root, func(cwd, name string, argv ...string) error {
		if cwd != root || name != "graphify" {
			t.Fatal("wrong CLI scope")
		}
		args = argv
		return nil
	}, io.Discard)
	if err != nil || strings.Join(args, " ") == "" || len(args) < 2 || args[0] != "export" || args[1] != "html" {
		t.Fatalf("export: %v %v", args, err)
	}
	page := strings.Repeat("large graph content\n", 100000)
	writeFixture(t, filepath.Join(dir, "graph.html"), page)
	server := httptest.NewServer(graphHandler(dir))
	defer server.Close()
	response, err := http.Get(server.URL + "/graph.html")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || string(body) != page {
		t.Fatal("large HTML truncated or failed")
	}
	writeFixture(t, filepath.Join(dir, ".private"), "must not serve")
	response, err = http.Get(server.URL + "/.private")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("served private output")
	}
}

func TestGraphServerRefusesLinksOutsideOutput(t *testing.T) {
	requireSymlinks(t)
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private.txt")
	writeFixture(t, outside, "private")
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(graphHandler(root))
	defer server.Close()
	response, err := http.Get(server.URL + "/link.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("served a file outside graphify-out")
	}
}
