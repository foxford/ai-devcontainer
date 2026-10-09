package host

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func legacyGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=ADC-Test", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=ADC-Test", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, data)
	}
	return string(data)
}

func oldInstallFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	upstream := filepath.Join(root, "upstream")
	if err := os.Mkdir(upstream, 0700); err != nil {
		t.Fatal(err)
	}
	legacyGit(t, upstream, "init", "-q", "-b", "main")
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(upstream, "bin/adc"), "old ADC executable")
	write(filepath.Join(upstream, "Dockerfile"), "old Dockerfile")
	legacyGit(t, upstream, "add", ".")
	legacyGit(t, upstream, "commit", "-qm", "old platform")
	legacyGit(t, upstream, "branch", "v0")
	legacyGit(t, upstream, "rm", "bin/adc")
	write(filepath.Join(upstream, "go.mod"), "module fixture\n")
	write(filepath.Join(upstream, "bin/adc/main.go"), "package main\n")
	write(filepath.Join(upstream, "runtime/orca-lock.json"), "{}\n")
	legacyGit(t, upstream, "add", ".")
	legacyGit(t, upstream, "commit", "-qm", "new platform")
	old, source := filepath.Join(root, "old"), filepath.Join(root, "new")
	legacyGit(t, root, "clone", "-q", "--branch", "v0", upstream, old)
	legacyGit(t, root, "clone", "-q", upstream, source)
	// A real previously installed main exists before the user's v0 switch.
	legacyGit(t, old, "branch", "main", "v0")
	return source, old, filepath.Join(root, "state")
}

func TestPOSIXBootstrapReplacesLegacyAtDefaultPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX bootstrap; Go replacement tests run on every OS")
	}
	source, _, _ := oldInstallFixture(t)
	upstream := strings.TrimSpace(legacyGit(t, source, "remote", "get-url", "origin"))
	home := t.TempDir()
	platform := filepath.Join(home, ".local/share/ai-devcontainer")
	legacyGit(t, home, "clone", "-q", "--branch", "v0", upstream, platform)
	installed := filepath.Join(home, ".local/bin/adc")
	if err := os.MkdirAll(filepath.Dir(installed), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(platform, "bin/adc"), installed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("# personal settings\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, ".adc")
	helper := filepath.Join(t.TempDir(), "adc")
	build := exec.Command("go", "build", "-o", helper, "./bin/adc")
	build.Dir = testutil.Root(t)
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build bootstrap helper: %v %s", err, data)
	}
	writeExecutable := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Exercise the actual installer and native helper without downloading Go or
	// installing system packages. The compiler fixture supplies that helper.
	writeExecutable(filepath.Join(state, "toolchains/go1.26.1/go/bin/go"), "#!/bin/sh\nwhile [ $# -gt 0 ]; do if [ \"$1\" = -o ]; then shift; cp \"$ADC_BOOTSTRAP_FIXTURE_BINARY\" \"$1\"; exit; fi; shift; done\nexit 1\n")
	path := t.TempDir()
	writeExecutable(filepath.Join(path, "docker"), "#!/bin/sh\ncase \"$1\" in ps) exit 0;; info) echo linux; exit 0;; *) exit 99;; esac\n")
	writeExecutable(filepath.Join(path, "brew"), "#!/bin/sh\nexit 99\n")
	for _, command := range []string{"powershell", "pwsh"} {
		writeExecutable(filepath.Join(path, command), "#!/bin/sh\nprintf '%s\\n' \"$HOME/.config/powershell/profile.ps1\"\n")
	}
	cmd := exec.Command("sh", filepath.Join(testutil.Root(t), "install.sh"), "--no-orca")
	cmd.Env = append(os.Environ(), "HOME="+home, "ZDOTDIR=", "AI_DEVCONTAINER_HOME=", "ADC_HOME=", "ADC_RUNTIME=0", "AI_DEVCONTAINER_REPO=", "ADC_BOOTSTRAP_FIXTURE_BINARY="+helper, "PATH="+path+string(os.PathListSeparator)+os.Getenv("PATH"))
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("default-path bootstrap: %v %s", err, data)
	}
	if legacyGit(t, platform, "branch", "--show-current") != "main\n" {
		t.Fatal("default platform was not replaced")
	}
	if _, err := os.Stat(filepath.Join(state, "bin/adc")); err != nil {
		t.Fatal("new CLI missing at default state path")
	}
	if _, err := os.Stat(filepath.Join(state, "workspace.json")); err != nil {
		t.Fatal("new workspace was not initialized")
	}
	if info, err := os.Lstat(installed); err != nil || !info.Mode().IsRegular() {
		t.Fatal("old ADC link was not replaced by a regular executable")
	}
	profile, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil || !strings.Contains(string(profile), "# personal settings") || !strings.Contains(string(profile), platform) {
		t.Fatal("PATH installation lost the profile or refers to a temporary checkout")
	}
	if _, err := os.Stat(filepath.Join(state, "completions/adc.zsh")); err != nil {
		t.Fatal("completion was not installed")
	}
	stages, err := os.ReadDir(filepath.Join(state, "bootstrap"))
	if err != nil || len(stages) != 0 {
		t.Fatal("temporary bootstrap checkout was not cleaned up")
	}
	markers, _ := filepath.Glob(filepath.Join(state, "backups/platform-v0/*/backup.json"))
	if len(markers) != 1 {
		t.Fatal("missing verified platform backup")
	}
}

func TestLegacyInstallationCopiesBeforeSwitchingToMain(t *testing.T) {
	source, old, state := oldInstallFixture(t)
	oldRev := legacyGit(t, old, "rev-parse", "HEAD")
	if err := replaceLegacyInstallation(source, state, old, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if legacyGit(t, old, "branch", "--show-current") != "main\n" || legacyGit(t, old, "rev-parse", "HEAD") != legacyGit(t, source, "rev-parse", "HEAD") {
		t.Fatal("did not switch the existing clone to new main")
	}
	if legacyGit(t, old, "rev-parse", "--abbrev-ref", "@{upstream}") != "origin/main\n" {
		t.Fatal("later adc updates would not pull main")
	}
	markers, err := filepath.Glob(filepath.Join(state, "backups/platform-v0/*/backup.json"))
	if err != nil || len(markers) != 1 {
		t.Fatal("missing verified backup")
	}
	copy := filepath.Join(filepath.Dir(markers[0]), "checkout")
	if data, err := os.ReadFile(filepath.Join(copy, "bin/adc")); err != nil || string(data) != "old ADC executable" {
		t.Fatal("lost old command in backup")
	}
	if legacyGit(t, copy, "rev-parse", "HEAD") != oldRev {
		t.Fatal("backup lost Git state")
	}
	data, err := os.ReadFile(markers[0])
	if err != nil {
		t.Fatal(err)
	}
	var marker legacyPlatformBackup
	if err = json.Unmarshal(data, &marker); err != nil {
		t.Fatal(err)
	}
	digest, err := legacyPlatformDigest(copy)
	if err != nil || digest != marker.Digest {
		t.Fatal("backup content changed or was not verified")
	}
}

func TestLegacyInstallationRejectsUnsafeTransitions(t *testing.T) {
	for _, kind := range []string{"active container", "dirty platform", "nested state", "state symlink into clone", "diverged main", "container started during copy"} {
		t.Run(kind, func(t *testing.T) {
			source, old, state := oldInstallFixture(t)
			stopped := func(string) error { return nil }
			switch kind {
			case "active container":
				stopped = func(string) error { return errors.New("stop old container") }
			case "dirty platform":
				if err := os.WriteFile(filepath.Join(old, "bin/adc"), []byte("personal edit"), 0600); err != nil {
					t.Fatal(err)
				}
			case "nested state":
				state = filepath.Join(old, "state")
			case "state symlink into clone":
				if err := os.Symlink(old, state); err != nil {
					t.Skipf("symlink privilege required: %v", err)
				}
				state = filepath.Join(state, "nested")
			case "diverged main":
				legacyGit(t, old, "switch", "main")
				legacyGit(t, old, "commit", "--allow-empty", "-qm", "local changes")
				legacyGit(t, old, "switch", "v0")
			case "container started during copy":
				calls := 0
				stopped = func(string) error {
					calls++
					if calls == 2 {
						return errors.New("new active container")
					}
					return nil
				}
			}
			before := legacyGit(t, old, "rev-parse", "HEAD")
			data, err := os.ReadFile(filepath.Join(old, "bin/adc"))
			if err != nil {
				t.Fatal(err)
			}
			if err := replaceLegacyInstallation(source, state, old, stopped); err == nil {
				t.Fatal("accepted unsafe platform replacement")
			}
			after, err := os.ReadFile(filepath.Join(old, "bin/adc"))
			if err != nil || string(after) != string(data) || legacyGit(t, old, "rev-parse", "HEAD") != before {
				t.Fatal("failed transition changed original working tree")
			}
		})
	}
}

func TestLegacyPlatformCopyPreservesLinksWithoutFollowing(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "old")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("original outside data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "link")); err != nil {
		t.Skipf("symlink privilege required: %v", err)
	}
	dest := filepath.Join(root, "copy")
	if err := copyLegacyPlatform(source, dest); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(filepath.Join(dest, "link")); err != nil || link != outside {
		t.Fatal("copied external target instead of the link")
	}
	a, err := legacyPlatformDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	b, err := legacyPlatformDigest(dest)
	if err != nil || a != b {
		t.Fatal("copy not equivalent")
	}
}

func TestLegacyPlatformMountGuardResolvesHostAliases(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "platform")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	container := legacyInspection{Name: "/running-devcontainer"}
	container.Mounts = append(container.Mounts, struct{ Type, Source, Destination, Name string }{Type: "bind", Source: source, Destination: "/opt/ai-devcontainer"})
	if err := checkLegacyPlatformMounts(source, []legacyInspection{container}); err == nil {
		t.Fatal("accepted an actively mounted platform")
	}
	container.Mounts[0].Source = filepath.Join(root, "unrelated")
	if err := checkLegacyPlatformMounts(source, []legacyInspection{container}); err != nil {
		t.Fatal("blocked an unrelated mount")
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Skipf("symlink privilege required: %v", err)
	}
	container.Mounts[0].Source = alias
	if err := checkLegacyPlatformMounts(source, []legacyInspection{container}); err == nil {
		t.Fatal("accepted the platform mounted via a host symlink")
	}
}
