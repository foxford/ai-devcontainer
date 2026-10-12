package host

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// A stateful Docker fixture exercises the production handoff and crash recovery
// using actual journal/key files. Real Docker behavior is covered by smoke.
type replacementDocker struct {
	containers map[string]*workspaceContainer
	fail       map[string]error
	created    bool
	calls      []string
}

func (f *replacementDocker) find(id string) (string, *workspaceContainer) {
	for name, c := range f.containers {
		if c.ID == id || name == id {
			return name, c
		}
	}
	return "", nil
}

func (f *replacementDocker) commands() dockerCommands {
	return dockerCommands{
		output: func(args ...string) (string, error) {
			switch args[0] {
			case "container":
				name := strings.TrimSuffix(strings.TrimPrefix(args[5], "name=^/"), "$")
				if c := f.containers[name]; c != nil {
					return c.ID, nil
				}
				return "", nil
			case "inspect":
				_, c := f.find(args[len(args)-1])
				if c == nil {
					return "", errors.New("no such container")
				}
				data, err := json.Marshal(c)
				return string(data), err
			}
			return "", errors.New("unexpected Docker query")
		},
		run: func(args ...string) error {
			f.calls = append(f.calls, strings.Join(args, " "))
			failure := f.fail[args[0]]
			delete(f.fail, args[0])
			if failure != nil && (args[0] != "run" || !f.created) {
				return failure
			}
			name, c := f.find(args[len(args)-1])
			switch args[0] {
			case "stop", "start":
				if c == nil {
					return errors.New("no such container")
				}
				c.State.Status = map[string]string{"stop": "exited", "start": "running"}[args[0]]
			case "rename":
				name, c = f.find(args[1])
				if c == nil || f.containers[args[2]] != nil {
					return errors.New("rename conflict")
				}
				delete(f.containers, name)
				f.containers[args[2]] = c
			case "rm":
				if c == nil || c.State.Status == "running" && len(args) == 2 {
					return errors.New("cannot remove container")
				}
				delete(f.containers, name)
			case "run":
				candidate := &workspaceContainer{ID: "candidate-id", Image: args[len(args)-1]}
				candidate.State.Status = "running"
				candidate.Config.Labels = map[string]string{}
				for i := 1; i < len(args)-1; i++ {
					if args[i] == "--name" {
						name = args[i+1]
					}
					if args[i] == "--label" {
						key, value, _ := strings.Cut(args[i+1], "=")
						candidate.Config.Labels[key] = value
					}
				}
				if f.containers[name] != nil {
					return errors.New("name conflict")
				}
				f.containers[name] = candidate
			default:
				return errors.New("unexpected Docker mutation")
			}
			return failure
		},
	}
}

func replacementFixture(t *testing.T) (*App, *preparedWorkspace, *replacementDocker) {
	t.Helper()
	a := &App{State: t.TempDir()}
	c := shared.Config{Name: "fixture", Port: 6768}
	old := &workspaceContainer{ID: "previous-id", Image: "previous-image"}
	old.State.Status = "running"
	p := &preparedWorkspace{Config: c, Args: []string{"run", "-d", "--name", "adc-fixture", "new-image"}, Key: "new-key", OldKey: "previous-key", Existing: old}
	f := &replacementDocker{containers: map[string]*workspaceContainer{"adc-fixture": old}, fail: map[string]error{}}
	if err := os.WriteFile(filepath.Join(a.State, "container-key"), []byte(p.OldKey), 0600); err != nil {
		t.Fatal(err)
	}
	return a, p, f
}

func assertReplacementCleared(t *testing.T, a *App, f *replacementDocker, id, status, key string) {
	t.Helper()
	c := f.containers["adc-fixture"]
	if c == nil || c.ID != id || c.State.Status != status || len(f.containers) != 1 {
		t.Fatalf("unexpected containers: %#v", f.containers)
	}
	if data, err := os.ReadFile(filepath.Join(a.State, "container-key")); err != nil || string(data) != key {
		t.Fatalf("key %q: %v", data, err)
	}
	if _, err := os.Stat(a.replacementPath()); !os.IsNotExist(err) {
		t.Fatalf("journal was not cleared: %v", err)
	}
}

func TestWorkspaceReplacementRetainsPreviousUntilReady(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "interrupted wait"}[pending], func(t *testing.T) {
			a, p, f := replacementFixture(t)
			err := a.launchWorkspace(p, f.commands(), func(bool) (bool, error) {
				r, err := a.readReplacement(p.Config)
				if err != nil || r == nil || f.containers[r.Backup] == nil || f.containers[r.Backup].State.Status != "exited" {
					t.Fatalf("old container removed before readiness: %v", err)
				}
				return !pending, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if pending {
				// A new host process continues solely from persisted identities.
				a = &App{State: a.State}
				resumed, err := a.resumeReplacement(p.Config, f.commands(), func(resuming bool) (bool, error) {
					if !resuming {
						t.Fatal("did not resume candidate")
					}
					return true, nil
				})
				if !resumed || err != nil {
					t.Fatal(resumed, err)
				}
			}
			assertReplacementCleared(t, a, f, "candidate-id", "running", p.Key)
		})
	}
}

func TestWorkspaceCreationFailureRestoresPreviousState(t *testing.T) {
	for _, running := range []bool{false, true} {
		for _, created := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "stopped", true: "running"}[running], map[bool]string{false: "absent", true: "created"}[created]}, "/"), func(t *testing.T) {
				a, p, f := replacementFixture(t)
				status := "exited"
				if running {
					status = "running"
				}
				p.Existing.State.Status = status
				failure := errors.New("cannot bind port")
				f.fail["run"], f.created = failure, created
				err := a.launchWorkspace(p, f.commands(), func(bool) (bool, error) { t.Fatal("wait after run failure"); return false, nil })
				if !errors.Is(err, failure) || !strings.Contains(err.Error(), "восстановлен") {
					t.Fatal(err)
				}
				assertReplacementCleared(t, a, f, p.Existing.ID, status, p.OldKey)
			})
		}
	}
}

func TestWorkspaceReplacementFailureCanBeRetried(t *testing.T) {
	for _, step := range []string{"stop", "rename", "wait", "rm", "rollback"} {
		t.Run(step, func(t *testing.T) {
			a, p, f := replacementFixture(t)
			failure := errors.New("temporary failure")
			f.fail[step] = failure
			if step == "rollback" {
				f.fail["run"], f.fail["start"] = errors.New("create failed"), failure
			}
			err := a.launchWorkspace(p, f.commands(), func(bool) (bool, error) {
				if step == "wait" {
					return false, failure
				}
				return true, nil
			})
			if !errors.Is(err, failure) {
				t.Fatalf("lost cause: %v", err)
			}
			if _, err := os.Stat(a.replacementPath()); err != nil {
				t.Fatal("lost recovery journal", err)
			}
			resumed, err := (&App{State: a.State}).resumeReplacement(p.Config, f.commands(), func(bool) (bool, error) { return true, nil })
			if err != nil {
				t.Fatal(err)
			}
			if step == "wait" || step == "rm" {
				if !resumed {
					t.Fatal("candidate was replaced instead of resumed")
				}
				assertReplacementCleared(t, a, f, "candidate-id", "running", p.Key)
			} else {
				if resumed {
					t.Fatal("should prepare a new attempt after restoring previous")
				}
				assertReplacementCleared(t, a, f, p.Existing.ID, "running", p.OldKey)
			}
		})
	}
}

func TestWorkspaceRecoversCrashWindowsAndRejectsForeignContainers(t *testing.T) {
	for _, window := range []string{"before stop", "after stop", "after rename", "after run", "after cleanup", "foreign active", "foreign backup", "running backup", "corrupt journal"} {
		t.Run(window, func(t *testing.T) {
			a, p, f := replacementFixture(t)
			r := &workspaceReplacement{Version: 1, Container: "adc-fixture", Token: "0123456789abcdef", PreviousID: p.Existing.ID, PreviousRunning: true, PreviousKey: p.OldKey, Key: p.Key}
			r.Backup = r.Container + "-previous-" + r.Token
			if window != "before stop" {
				p.Existing.State.Status = "exited"
			}
			if window != "before stop" && window != "after stop" && window != "corrupt journal" {
				delete(f.containers, r.Container)
				f.containers[r.Backup] = p.Existing
			}
			if window == "after run" || window == "after cleanup" || strings.Contains(window, "foreign") || window == "running backup" {
				candidate := &workspaceContainer{ID: "candidate-id"}
				candidate.State.Status = "running"
				candidate.Config.Labels = map[string]string{"adc.replacement": r.Token}
				f.containers[r.Container] = candidate
				if window == "after cleanup" {
					delete(f.containers, r.Backup)
				}
				if window == "foreign active" {
					candidate.Config.Labels["adc.replacement"] = "someone-else"
				}
				if window == "foreign backup" {
					f.containers[r.Backup] = &workspaceContainer{ID: "someone-else"}
				}
				if window == "running backup" {
					p.Existing.State.Status = "running"
				}
			}
			if window == "corrupt journal" {
				r.Backup = "unrelated-container"
			}
			data, _ := json.Marshal(r)
			if err := os.WriteFile(a.replacementPath(), data, 0600); err != nil {
				t.Fatal(err)
			}
			resumed, err := a.resumeReplacement(p.Config, f.commands(), func(bool) (bool, error) { return true, nil })
			if strings.Contains(window, "foreign") || window == "running backup" || window == "corrupt journal" {
				if err == nil || len(f.calls) != 0 {
					t.Fatalf("modified foreign containers: %v %v", err, f.calls)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if window == "after run" || window == "after cleanup" {
				if !resumed {
					t.Fatal("lost candidate")
				}
				assertReplacementCleared(t, a, f, "candidate-id", "running", p.Key)
			} else {
				assertReplacementCleared(t, a, f, p.Existing.ID, "running", p.OldKey)
			}
		})
	}
}

func TestWorkspaceJournalFailureLeavesRuntimeUntouched(t *testing.T) {
	a, p, f := replacementFixture(t)
	if err := os.Mkdir(a.replacementPath(), 0700); err != nil {
		t.Fatal(err)
	}
	err := a.launchWorkspace(p, f.commands(), func(bool) (bool, error) { t.Fatal("wait after journal failure"); return false, nil })
	if err == nil || len(f.calls) != 0 || p.Existing.State.Status != "running" {
		t.Fatalf("runtime changed without journal: %v %v", err, f.calls)
	}
}

func TestWorkspaceUpdateReplacesEvenUnchangedImage(t *testing.T) {
	a, p, f := replacementFixture(t)
	p.Args[len(p.Args)-1], p.Key, p.Replace = p.Existing.Image, p.OldKey, true
	if err := a.launchWorkspace(p, f.commands(), func(bool) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	assertReplacementCleared(t, a, f, "candidate-id", "running", p.Key)
}

func TestWorkspaceStartKeepsUnchangedContainerAndRetriesPreparation(t *testing.T) {
	a, p, f := replacementFixture(t)
	p.Args[len(p.Args)-1], p.Key = p.Existing.Image, p.OldKey
	if err := a.launchWorkspace(p, f.commands(), func(resuming bool) (bool, error) {
		if !resuming {
			t.Fatal("failed first-install runtime cannot retry preparation")
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatal("unchanged runtime was replaced", f.calls)
	}
	assertReplacementCleared(t, a, f, p.Existing.ID, "running", p.OldKey)
}
