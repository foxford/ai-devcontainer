package shared

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func updatesGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=ADC Test", "GIT_COMMITTER_NAME=ADC Test", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, data)
	}
	return strings.TrimSpace(string(data))
}

func updatesWrite(t *testing.T, root, path, data string) {
	t.Helper()
	if err := AtomicWrite(filepath.Join(root, filepath.FromSlash(path)), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func updateFixture(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	upstream := filepath.Join(root, "upstream")
	if err := os.MkdirAll(upstream, 0700); err != nil {
		t.Fatal(err)
	}
	updatesWrite(t, upstream, "delivery/catalog.json", `{"version":1,"base":{"instructions":["instructions/base.md"],"skills":"skills","mcp":{"source":"mcp/servers.json","servers":[]}},"capabilities":{"graph":{"bundle":"bundles/graph/bundle.json"}}}`)
	updatesWrite(t, upstream, "delivery/instructions/base.md", "base\n")
	updatesWrite(t, upstream, "delivery/skills/humanizer/SKILL.md", "---\nname: humanizer\nversion: 1.0.0\n---\nOld skill\n")
	updatesWrite(t, upstream, "delivery/skills/humanizer/meta.json", `{"kind":"skill","name":"humanizer","version":"1.0.0"}`)
	updatesWrite(t, upstream, "delivery/skills/humanizer/references/catalog.md", "Original reference\n")
	updatesWrite(t, upstream, "delivery/mcp/servers.json", `{"mcpServers":{"browser":{"command":"old-browser"}}}`)
	updatesWrite(t, upstream, "delivery/mcp/browser.meta.json", `{"kind":"mcp","name":"browser","version":"1.0.0"}`)
	updatesWrite(t, upstream, "delivery/bundles/graph/bundle.json", `{"version":1,"name":"graph","description":"Graph","exports":{"instructions":["instructions.md"]}}`)
	updatesWrite(t, upstream, "delivery/bundles/graph/instructions.md", "Original instructions\n")
	updatesWrite(t, upstream, "delivery/bundles/graph/meta.json", `{"kind":"bundle","name":"graph","version":"1.0.0"}`)
	updatesGit(t, upstream, "init", "--quiet", "-b", "main")
	updatesGit(t, upstream, "add", ".")
	updatesGit(t, upstream, "commit", "--quiet", "-m", "initial delivery")
	clone := filepath.Join(root, "platform")
	updatesGit(t, root, "clone", "--quiet", upstream, clone)
	return &App{Platform: clone, State: filepath.Join(root, "state")}, upstream
}

func changeUpdateFixture(t *testing.T, upstream string) {
	t.Helper()
	// Unit authors bump explicit versions alongside the content they change.
	updatesWrite(t, upstream, "delivery/skills/humanizer/references/catalog.md", "Updated reference\n")
	updatesWrite(t, upstream, "delivery/skills/humanizer/meta.json", `{"kind":"skill","name":"humanizer","version":"2.0.0"}`)
	updatesWrite(t, upstream, "delivery/mcp/servers.json", `{"mcpServers":{"browser":{"command":"new-browser"}}}`)
	updatesWrite(t, upstream, "delivery/mcp/browser.meta.json", `{"kind":"mcp","name":"browser","version":"2.0.0"}`)
	updatesWrite(t, upstream, "delivery/bundles/graph/instructions.md", "Updated instructions\n")
	updatesWrite(t, upstream, "delivery/bundles/graph/meta.json", `{"kind":"bundle","name":"graph","version":"2.0.0"}`)
	updatesGit(t, upstream, "add", ".")
	updatesGit(t, upstream, "commit", "--quiet", "-m", "update delivery")
}

func TestCheckUpdatesUsesSeparateCacheAndReportsComponents(t *testing.T) {
	a, upstream := updateFixture(t)
	ctx := context.Background()
	before := updatesGit(t, a.Platform, "rev-parse", "HEAD")
	status, err := a.CheckUpdates(ctx, true)
	if err != nil || status.Available || status.Cached {
		t.Fatalf("initial status: %+v %v", status, err)
	}
	changeUpdateFixture(t, upstream)
	status, err = a.CheckUpdates(ctx, false)
	if err != nil || status.Available || !status.Cached {
		t.Fatalf("daily cache was bypassed: %+v %v", status, err)
	}
	status, err = a.CheckUpdates(ctx, true)
	if err != nil || !status.Available || status.Cached || len(status.Changes) != 3 {
		t.Fatalf("new status: %+v %v", status, err)
	}
	want := map[string]bool{"skill/humanizer": true, "mcp/browser": true, "bundle/graph": true}
	for _, change := range status.Changes {
		if !want[change.Kind+"/"+change.Name] || change.Action != "updated" {
			t.Fatalf("unexpected change: %+v", change)
		}
	}
	if updatesGit(t, a.Platform, "rev-parse", "HEAD") != before || updatesGit(t, a.Platform, "rev-parse", "origin/main") != before || updatesGit(t, a.Platform, "status", "--porcelain") != "" {
		t.Fatal("checking updates changed the platform clone")
	}
	if _, err := os.Stat(filepath.Join(a.Platform, ".git", "FETCH_HEAD")); !os.IsNotExist(err) {
		t.Fatal("checking updates wrote FETCH_HEAD to platform")
	}
	updatesGit(t, a.Platform, "pull", "--quiet", "--ff-only")
	status, err = a.CheckUpdates(ctx, false)
	if err != nil || status.Available || len(status.Changes) != 0 || !status.Cached {
		t.Fatalf("already applied update still offered: %+v %v", status, err)
	}
}

func TestUpdateSourceUsesPublicHTTPSWithoutChangingCheckout(t *testing.T) {
	for _, remote := range []string{
		"git@github.com:foxford/ai-devcontainer.git",
		"git@github.com:foxford/ai-devcontainer",
		"ssh://git@github.com/foxford/ai-devcontainer.git",
		"ssh://git@github.com:22/foxford/ai-devcontainer.git",
		publicUpdateRemote,
	} {
		t.Run(remote, func(t *testing.T) {
			a, _ := updateFixture(t)
			updatesGit(t, a.Platform, "remote", "set-url", "origin", remote)
			before := updatesGit(t, a.Platform, "rev-parse", "HEAD")
			source, err := a.updateSource(context.Background())
			if err != nil || source.remote != publicUpdateRemote || source.ref != "refs/heads/main" {
				t.Fatalf("public check still needs SSH: %+v %v", source, err)
			}
			if updatesGit(t, a.Platform, "remote", "get-url", "origin") != remote || updatesGit(t, a.Platform, "rev-parse", "HEAD") != before || updatesGit(t, a.Platform, "status", "--porcelain") != "" {
				t.Fatal("checking the public source changed the checkout")
			}
		})
	}
}

func TestUpdateReadRemotePreservesOtherRepositories(t *testing.T) {
	for _, remote := range []string{
		"git@github.com:private-owner/ai-devcontainer.git",
		"ssh://git@git.example.com/foxford/ai-devcontainer.git",
		"ssh://git@github.com:2222/foxford/ai-devcontainer.git",
		"git@github-work:foxford/ai-devcontainer.git",
		"ssh://other@github.com/foxford/ai-devcontainer.git",
		"ssh://git:password@github.com/foxford/ai-devcontainer.git",
		"git@github.com.evil:foxford/ai-devcontainer.git",
		"ssh://git@github.com/foxford/ai-devcontainer.git?other=1",
		"ssh://git@github.com/foxford/ai-devcontainer.git#other",
		"https://git.example.com/foxford/ai-devcontainer.git",
		"file:///tmp/platform", "/tmp/platform", "../platform", ".",
	} {
		if got := updateReadRemote(remote); got != remote {
			t.Errorf("changed an unrelated remote: %q -> %q", remote, got)
		}
	}
}

func TestUpdatePublicHTTPSOverridesUserSSHRewrite(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "gitconfig")
	updatesGit(t, root, "config", "--file", config, "url.git@github.com:.insteadOf", "https://github.com/")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ remote, want string }{
		{publicUpdateRemote, publicUpdateRemote},
		{"https://github.com/private-owner/platform.git", "git@github.com:private-owner/platform.git"},
	} {
		got, err := updateGit(context.Background(), root, "ls-remote", "--get-url", tc.remote)
		if err != nil || got != tc.want {
			t.Fatalf("unexpected effective transport for %q: %q %v", tc.remote, got, err)
		}
	}
	after, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("changed the user's Git configuration", err)
	}
}

func TestUpdateHTTPSDiscardsCachedSSHFailure(t *testing.T) {
	a, _ := updateFixture(t)
	remote := "git@github.com:foxford/ai-devcontainer.git"
	updatesGit(t, a.Platform, "remote", "set-url", "origin", remote)
	old := updateCache{Version: 1, Source: remote + "\nrefs/heads/main", AttemptedAt: time.Now().UTC(), Error: "context deadline exceeded"}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	updatesWrite(t, a.State, "cache/updates/status.json", string(data))
	source, err := a.updateSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cached, _ := a.readUpdateCache(source)
	if cached.Error != "" || updateCacheFresh(cached, time.Now()) {
		t.Fatalf("old SSH failure still blocks HTTPS: %+v", cached)
	}
}

func TestCheckUpdatesPreservesOfflineCacheAndThrottlesFailures(t *testing.T) {
	a, upstream := updateFixture(t)
	changeUpdateFixture(t, upstream)
	status, err := a.CheckUpdates(context.Background(), true)
	if err != nil || !status.Available {
		t.Fatal(status, err)
	}
	checked := status.CheckedAt
	if err := os.Rename(upstream, upstream+"-offline"); err != nil {
		t.Fatal(err)
	}
	status, err = a.CheckUpdates(context.Background(), true)
	if err == nil || !status.Available || status.Error == "" || !status.CheckedAt.Equal(checked) || len(status.Changes) != 3 {
		t.Fatalf("lost last successful status: %+v %v", status, err)
	}
	if err := os.Rename(upstream+"-offline", upstream); err != nil {
		t.Fatal(err)
	}
	status, err = a.CheckUpdates(context.Background(), false)
	if err == nil || !status.Cached || status.Error == "" {
		t.Fatalf("failed check was not throttled: %+v %v", status, err)
	}
	status, err = a.CheckUpdates(context.Background(), true)
	if err != nil || status.Error != "" || status.Cached {
		t.Fatalf("explicit refresh did not recover: %+v %v", status, err)
	}
}

func TestCheckUpdatesDoesNotOfferOlderRemoteOrCache(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "old cache after pull", true: "local ahead of remote"}[refresh], func(t *testing.T) {
			a, upstream := updateFixture(t)
			if _, err := a.CheckUpdates(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if refresh {
				updatesWrite(t, a.Platform, "delivery/skills/humanizer/references/catalog.md", "Locally committed improvement\n")
				updatesGit(t, a.Platform, "add", ".")
				updatesGit(t, a.Platform, "commit", "--quiet", "-m", "local improvement")
			} else {
				changeUpdateFixture(t, upstream)
				updatesGit(t, a.Platform, "pull", "--quiet", "--ff-only")
			}
			status, err := a.CheckUpdates(context.Background(), refresh)
			if err != nil || status.Available || len(status.Changes) != 0 {
				t.Fatalf("older remote offered as an update: %+v %v", status, err)
			}
		})
	}
}

func TestCheckUpdatesReportsInvalidLocalMetadata(t *testing.T) {
	a, _ := updateFixture(t)
	updatesWrite(t, a.Platform, "delivery/skills/humanizer/meta.json", "broken JSON")
	status, err := a.CheckUpdates(context.Background(), true)
	if err == nil || status.Error == "" || status.Available {
		t.Fatalf("invalid delivery was reported as current: %+v %v", status, err)
	}
}

func TestCheckUpdatesIgnoresContentChangesWithoutVersionBump(t *testing.T) {
	a, upstream := updateFixture(t)
	updatesWrite(t, upstream, "delivery/skills/humanizer/SKILL.md", "Completely changed skill\n")
	// Payloads are neither read nor validated during version discovery.
	updatesWrite(t, upstream, "delivery/catalog.json", "not parsed by version checks")
	updatesGit(t, upstream, "add", ".")
	updatesGit(t, upstream, "commit", "--quiet", "-m", "content changed without release bump")
	status, err := a.CheckUpdates(context.Background(), true)
	if err != nil || status.Available || len(status.Changes) != 0 {
		t.Fatalf("content changes treated as release metadata: %+v %v", status, err)
	}
	data, err := os.ReadFile(filepath.Join(a.State, "cache", "updates", "status.json"))
	if err != nil || bytes.Contains(data, []byte("digest")) || bytes.Contains(data, []byte("snapshot")) || !bytes.Contains(data, []byte("versions")) {
		t.Fatalf("cache should contain explicit versions only: %s %v", data, err)
	}
}

func TestUpdateCacheTTLAndBusyLock(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		age   time.Duration
		err   string
		fresh bool
	}{{23 * time.Hour, "", true}, {25 * time.Hour, "", false}, {59 * time.Minute, "offline", true}, {61 * time.Minute, "offline", false}, {-time.Hour, "", false}} {
		if got := updateCacheFresh(updateCache{AttemptedAt: now.Add(-tc.age), Error: tc.err}, now); got != tc.fresh {
			t.Fatalf("TTL %+v: got %v", tc, got)
		}
	}
	a, _ := updateFixture(t)
	if _, err := a.CheckUpdates(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	source, err := a.updateSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, cacheFile := a.readUpdateCache(source)
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(cacheFile), "check.lockfile"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if acquired, err := tryFileLock(lock); err != nil || !acquired {
		t.Fatal(acquired, err)
	}
	defer unlockFile(lock)
	started := time.Now()
	status, err := a.CheckUpdates(context.Background(), true)
	if err != nil || !status.Cached || time.Since(started) > 2*time.Second {
		t.Fatalf("waited for another checker: %+v %v", status, err)
	}
}

func TestExtractUpdateArchiveRejectsUnsafeEntries(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind byte
	}{{"../escaped", tar.TypeReg}, {"/tmp/escaped", tar.TypeReg}, {"delivery/link", tar.TypeSymlink}, {"delivery/hardlink", tar.TypeLink}, {"delivery/C:drive", tar.TypeReg}, {"delivery\\escaped", tar.TypeReg}, {"other/unexpected", tar.TypeReg}, {"delivery/skills/humanizer/SKILL.md", tar.TypeReg}} {
		t.Run(tc.name, func(t *testing.T) {
			var archive bytes.Buffer
			writer := tar.NewWriter(&archive)
			if err := writer.WriteHeader(&tar.Header{Name: tc.name, Mode: 0600, Typeflag: tc.kind, Linkname: "../../escaped"}); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := extractUpdateArchive(context.Background(), t.TempDir(), &archive); err == nil {
				t.Fatal("accepted unsafe archive entry")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := extractUpdateArchive(ctx, t.TempDir(), bytes.NewReader(nil)); !errors.Is(err, context.Canceled) {
		t.Fatal("ignored cancellation", err)
	}
}

func TestWriteUpdateStatusColorAndPlainText(t *testing.T) {
	status := UpdateStatus{Available: true, Changes: []DeliveryChange{{Kind: "skill", Name: "humanizer", Action: "updated", BeforeVersion: "1", AfterVersion: "2"}, {Kind: "mcp", Name: "browser", Action: "added"}}}
	for _, color := range []bool{false, true} {
		var out bytes.Buffer
		if err := WriteUpdateStatus(&out, status, color); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"ADC: доступны обновления", "skill humanizer", "MCP browser", "1 → 2", "новая версия", "новый", "adc update"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("missing %q: %s", want, out.String())
			}
		}
		if strings.Contains(out.String(), "\x1b[") != color {
			t.Fatalf("unexpected ANSI mode: %q", out.String())
		}
	}
	var applied bytes.Buffer
	if err := WriteDeliveryChanges(&applied, "ADC: платформа обновлена", status.Changes, true, true); err != nil || !strings.Contains(applied.String(), "\x1b[1;32m") || !strings.Contains(applied.String(), "обновлён") || !strings.Contains(applied.String(), "добавлен") {
		t.Fatalf("missing applied status: %q %v", applied.String(), err)
	}
	var failed bytes.Buffer
	status.Error = "offline"
	if err := WriteUpdateStatus(&failed, status, true); err != nil || !strings.Contains(failed.String(), "ADC: доступны обновления") || !strings.Contains(failed.String(), "\x1b[31m  Проверка недоступна:") {
		t.Fatalf("stale result or red error missing: %q %v", failed.String(), err)
	}
	var out bytes.Buffer
	if ColorEnabled(&out) {
		t.Fatal("color enabled for redirected output")
	}
	t.Setenv("NO_COLOR", "")
	if ColorEnabled(os.Stdout) {
		t.Fatal("NO_COLOR ignored")
	}
	t.Setenv("TERM", "dumb")
	if ColorEnabled(os.Stdout) {
		t.Fatal("dumb terminal colored")
	}
}
