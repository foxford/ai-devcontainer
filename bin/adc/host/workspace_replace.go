package host

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type dockerCommands struct {
	run    func(...string) error
	output func(...string) (string, error)
}

func realDocker() dockerCommands {
	return dockerCommands{
		run:    func(args ...string) error { return shared.Run("", "docker", args...) },
		output: func(args ...string) (string, error) { return shared.Output("", "docker", args...) },
	}
}

type workspaceContainer struct {
	ID     string
	Image  string
	State  struct{ Status string }
	Config struct{ Labels map[string]string }
}

// Listing distinguishes an absent container from an unavailable Docker daemon.
func (d dockerCommands) container(name string) (*workspaceContainer, error) {
	id, err := d.output("container", "ls", "--all", "--no-trunc", "--filter", "name=^/"+name+"$", "--format", "{{.ID}}")
	if err != nil || strings.TrimSpace(id) == "" {
		return nil, err
	}
	data, err := d.output("inspect", "--format", "{{json .}}", strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	var container workspaceContainer
	if err := json.Unmarshal([]byte(data), &container); err != nil {
		return nil, err
	}
	if container.ID == "" {
		return nil, errors.New("Docker не вернул ID контейнера")
	}
	return &container, nil
}

type preparedWorkspace struct {
	Config   shared.Config
	Args     []string
	Key      string
	OldKey   string
	Existing *workspaceContainer
	Replace  bool
}

func (a *App) prepareWorkspace(c shared.Config, image string) (*preparedWorkspace, error) {
	if err := a.checkMigration(); err != nil {
		return nil, err
	}
	if err := a.prepareDockerSecurity(); err != nil {
		return nil, fmt.Errorf("подготовка Docker seccomp: %w", err)
	}
	args, err := a.DockerArgs(c)
	if err != nil {
		return nil, err
	}
	profile, err := a.prepareDockerAppArmor()
	if err != nil {
		return nil, fmt.Errorf("подготовка Docker AppArmor: %w", err)
	}
	if profile != "" {
		args = append(args[:len(args)-1], "--security-opt", "apparmor="+profile, args[len(args)-1])
	}
	if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(image) {
		return nil, errors.New("Docker не вернул неизменяемый ID образа")
	}
	// Preserve compatibility with existing container-key files, but launch by ID
	// so a concurrent build cannot substitute another image after preflight.
	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	args[len(args)-1] = image
	existing, err := realDocker().container(a.shared().Container(c))
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.State.Status == "paused" {
		return nil, errors.New("workspace приостановлен; выполните docker unpause перед запуском")
	}
	oldKey, err := os.ReadFile(filepath.Join(a.State, "container-key"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return &preparedWorkspace{Config: c, Args: args, Key: hex.EncodeToString(sum[:]) + image, OldKey: string(oldKey), Existing: existing}, nil
}

// The journal stores identities, never Docker arguments or account data.
type workspaceReplacement struct {
	Version         int    `json:"version"`
	Container       string `json:"container"`
	Backup          string `json:"backup"`
	Token           string `json:"token"`
	PreviousID      string `json:"previousId"`
	PreviousRunning bool   `json:"previousRunning"`
	PreviousKey     string `json:"previousKey"`
	Key             string `json:"key"`
}

func (a *App) replacementPath() string { return filepath.Join(a.State, "workspace-replacement.json") }

func (a *App) readReplacement(c shared.Config) (*workspaceReplacement, error) {
	data, err := os.ReadFile(a.replacementPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var replacement workspaceReplacement
	if err := json.Unmarshal(data, &replacement); err != nil {
		return nil, fmt.Errorf("журнал переключения workspace: %w", err)
	}
	if replacement.Version != 1 || replacement.Container != a.shared().Container(c) ||
		!regexp.MustCompile(`^[a-f0-9]{16}$`).MatchString(replacement.Token) ||
		replacement.Backup != replacement.Container+"-previous-"+replacement.Token ||
		replacement.PreviousID == "" || replacement.Key == "" {
		return nil, errors.New("некорректный журнал переключения workspace; контейнеры сохранены")
	}
	return &replacement, nil
}

type replacementWait func(resuming bool) (ready bool, err error)

func (a *App) replacementWait(c shared.Config) replacementWait {
	return func(resuming bool) (bool, error) {
		if resuming {
			state, err := dockerStartup(context.Background(), a.shared().Container(c))
			if err != nil {
				return false, err
			}
			if state.Status == "paused" {
				return false, errors.New("workspace приостановлен; выполните docker unpause")
			}
			if state.Progress.State == "failed" || state.Status != "running" {
				if state.Status == "running" || state.Status == "restarting" {
					if err := realDocker().run("stop", a.shared().Container(c)); err != nil {
						return false, err
					}
				}
				if err := realDocker().run("start", a.shared().Container(c)); err != nil {
					return false, err
				}
			}
		}
		return a.waitForWorkspaceReady(c)
	}
}

func (a *App) launchWorkspace(p *preparedWorkspace, d dockerCommands, wait replacementWait) error {
	name := a.shared().Container(p.Config)
	if !p.Replace && p.Existing != nil && p.OldKey == p.Key && p.Existing.Image == p.Args[len(p.Args)-1] {
		if p.Existing.State.Status != "running" {
			if err := d.run("start", p.Existing.ID); err != nil {
				return err
			}
		}
		// A first installation may have failed before any replacement journal
		// existed. Retrying start must also restart that failed runtime.
		_, err := wait(true)
		return err
	}
	if p.Existing == nil {
		if err := d.run(p.Args...); err != nil {
			return err
		}
		if err := shared.AtomicWrite(filepath.Join(a.State, "container-key"), []byte(p.Key), 0600); err != nil {
			return err
		}
		_, err := wait(false)
		return err
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(random[:])
	r := &workspaceReplacement{Version: 1, Container: name, Backup: name + "-previous-" + token,
		Token: token, PreviousID: p.Existing.ID, PreviousRunning: p.Existing.State.Status == "running" || p.Existing.State.Status == "restarting",
		PreviousKey: p.OldKey, Key: p.Key}
	if other, err := d.container(r.Backup); err != nil {
		return err
	} else if other != nil {
		return errors.New("имя резервного контейнера занято; workspace сохранён")
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := shared.AtomicWrite(a.replacementPath(), append(data, '\n'), 0600); err != nil {
		return err
	}
	if r.PreviousRunning {
		if err := d.run("stop", r.PreviousID); err != nil {
			return fmt.Errorf("остановка workspace: %w; продолжить: adc workspace start", err)
		}
	}
	if err := d.run("rename", r.PreviousID, r.Backup); err != nil {
		return fmt.Errorf("сохранение прежнего контейнера: %w; продолжить: adc workspace start", err)
	}
	args := append([]string{}, p.Args[:len(p.Args)-1]...)
	args = append(args, "--label", "adc.replacement="+r.Token, p.Args[len(p.Args)-1])
	if err := d.run(args...); err != nil {
		if rollback := a.restorePrevious(r, d); rollback != nil {
			return errors.Join(fmt.Errorf("создание нового контейнера: %w", err), fmt.Errorf("восстановление: %w; повтор: adc workspace start", rollback))
		}
		return fmt.Errorf("новый контейнер не создан; прежний контейнер восстановлен (активные сеансы завершены): %w", err)
	}
	return a.finishReplacement(r, d, wait, false)
}

func (a *App) finishReplacement(r *workspaceReplacement, d dockerCommands, wait replacementWait, resuming bool) error {
	if err := shared.AtomicWrite(filepath.Join(a.State, "container-key"), []byte(r.Key), 0600); err != nil {
		return err
	}
	ready, err := wait(resuming)
	if err != nil {
		return fmt.Errorf("новый runtime не подтвердил готовность; прежний контейнер сохранён как %s; диагностика: adc workspace logs; повтор: adc workspace start: %w", r.Backup, err)
	}
	if !ready {
		return nil
	}
	backup, err := d.container(r.Backup)
	if err != nil {
		return err
	}
	if backup != nil {
		if backup.ID != r.PreviousID || backup.State.Status == "running" || backup.State.Status == "restarting" || backup.State.Status == "paused" {
			return errors.New("резервный контейнер изменился; он сохранён, повтор: adc workspace start")
		}
		// Never force removal: an unexpectedly restarted backup stays intact.
		if err := d.run("rm", r.PreviousID); err != nil {
			return fmt.Errorf("новый runtime готов; очистка прежнего контейнера: %w; повтор: adc workspace start", err)
		}
	}
	return os.Remove(a.replacementPath())
}

func (a *App) restorePrevious(r *workspaceReplacement, d dockerCommands) error {
	active, err := d.container(r.Container)
	if err != nil {
		return err
	}
	if active != nil && active.ID != r.PreviousID {
		if active.Config.Labels["adc.replacement"] != r.Token {
			return errors.New("имя workspace занято другим контейнером; контейнеры сохранены")
		}
		if err := d.run("rm", "-f", active.ID); err != nil {
			return err
		}
		active = nil
	}
	if active == nil {
		backup, err := d.container(r.Backup)
		if err != nil {
			return err
		}
		if backup == nil || backup.ID != r.PreviousID {
			return errors.New("прежний контейнер не найден или изменился; журнал сохранён")
		}
		if err := d.run("rename", r.PreviousID, r.Container); err != nil {
			return err
		}
		active = backup
	}
	keyPath := filepath.Join(a.State, "container-key")
	if r.PreviousKey == "" {
		if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if err := shared.AtomicWrite(keyPath, []byte(r.PreviousKey), 0600); err != nil {
		return err
	}
	if r.PreviousRunning && active.State.Status != "running" {
		if err := d.run("start", r.PreviousID); err != nil {
			return err
		}
	}
	return os.Remove(a.replacementPath())
}

// Inspect identities rather than trusting the last recorded phase: a process
// can exit between any Docker operation and the next filesystem write.
func (a *App) resumeReplacement(c shared.Config, d dockerCommands, wait replacementWait) (bool, error) {
	r, err := a.readReplacement(c)
	if err != nil || r == nil {
		return false, err
	}
	active, err := d.container(r.Container)
	if err != nil {
		return true, err
	}
	if active == nil || active.ID == r.PreviousID {
		return false, a.restorePrevious(r, d)
	}
	if active.Config.Labels["adc.replacement"] != r.Token {
		return true, errors.New("контейнер workspace не соответствует журналу; контейнеры сохранены")
	}
	return true, a.finishReplacement(r, d, wait, true)
}
