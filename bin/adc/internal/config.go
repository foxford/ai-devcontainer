package shared

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const Schema = 1
const Marker = "<!-- ai-devcontainer:generated -->\n"

var ValidID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)

type Knowledge struct {
	Path    string `json:"path"`
	Origin  string `json:"origin"`
	Updated string `json:"updated"`
}
type Project struct {
	ID           string      `json:"id"`
	Path         string      `json:"path"`
	RuntimePath  string      `json:"runtimePath"`
	Description  string      `json:"description,omitempty"`
	Capabilities []string    `json:"capabilities,omitempty"`
	Related      []string    `json:"related,omitempty"`
	Knowledge    []Knowledge `json:"knowledge,omitempty"`
	ReadOnly     bool        `json:"readOnly,omitempty"`
}
type Config struct {
	Version       int         `json:"version"`
	Name          string      `json:"name"`
	Port          int         `json:"port"`
	OmniRoutePort int         `json:"omniroutePort,omitempty"`
	GraphPort     int         `json:"graphPort,omitempty"`
	Projects      []Project   `json:"projects"`
	Knowledge     []Knowledge `json:"knowledge,omitempty"`
}
type App struct {
	Platform, State string
	Runtime         bool
}

func (c Config) GatewayPort() int {
	if c.OmniRoutePort == 0 {
		return 20128
	}
	return c.OmniRoutePort
}
func (c Config) PreviewPort() int {
	if c.GraphPort == 0 {
		return 4242
	}
	return c.GraphPort
}
func (c Config) ValidatePorts() error {
	seen := map[int]bool{}
	for _, port := range []int{c.Port, c.GatewayPort(), c.PreviewPort()} {
		if port < 1 || port > 65535 || seen[port] {
			return errors.New("порты Orca, OmniRoute и графа должны быть разными числами от 1 до 65535")
		}
		seen[port] = true
	}
	return nil
}

func New() (*App, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	platform := os.Getenv("AI_DEVCONTAINER_HOME")
	if platform == "" {
		platform = filepath.Join(home, ".local", "share", "ai-devcontainer")
		if _, err := os.Stat(filepath.Join(platform, "go.mod")); err != nil {
			wd, _ := os.Getwd()
			for d := wd; ; d = filepath.Dir(d) {
				if _, e := os.Stat(filepath.Join(d, "go.mod")); e == nil {
					platform = d
					break
				}
				if filepath.Dir(d) == d {
					break
				}
			}
		}
	}
	state := os.Getenv("ADC_HOME")
	if state == "" {
		state = filepath.Join(home, ".adc")
	}
	return &App{Platform: platform, State: state, Runtime: os.Getenv("ADC_RUNTIME") == "1"}, nil
}
func (a *App) Load() (Config, error) {
	var c Config
	b, e := os.ReadFile(filepath.Join(a.State, "workspace.json"))
	if e != nil {
		return c, fmt.Errorf("workspace не инициализирован: adc workspace init (%w)", e)
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	if c.Version != Schema {
		return c, fmt.Errorf("неподдерживаемая версия workspace: %d", c.Version)
	}
	if !ValidID.MatchString(c.Name) || c.Port < 1 || c.Port > 65535 {
		return c, errors.New("некорректное имя или порт workspace")
	}
	if err := c.ValidatePorts(); err != nil {
		return c, err
	}
	seen := map[string]bool{}
	for _, p := range c.Projects {
		if !ValidID.MatchString(p.ID) || seen[p.ID] || !PortableAbs(p.Path) || !strings.HasPrefix(p.RuntimePath, "/") {
			return c, errors.New("некорректный реестр проектов")
		}
		seen[p.ID] = true
	}
	return c, nil
}
func (a *App) Save(c Config) error {
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	return AtomicWrite(filepath.Join(a.State, "workspace.json"), append(b, '\n'), 0600)
}
func AtomicWrite(path string, b []byte, mode os.FileMode) error {
	old, e := os.ReadFile(path)
	if e == nil && string(old) == string(b) {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			if info.Mode().Perm() != mode.Perm() {
				return os.Chmod(path, mode)
			}
			return nil
		}
	}
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".adc-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}

// OS locks release on process exit, including an interrupted container rebuild.
// Keep the inode: unlinking a lock file can let two processes lock different files.
func (a *App) Locked(fn func() error) error {
	return a.LockedFile("write.lockfile", fn)
}

// Separate host lifecycle operations from runtime configuration writes: the
// runtime must be able to sync while the host waits for readiness.
func (a *App) LockedFile(name string, fn func() error) error {
	if filepath.Base(name) != name || name == "." || name == ".." || name == "" || strings.ContainsAny(name, `/\`) {
		return errors.New("неверное имя lock")
	}
	if e := os.MkdirAll(a.State, 0700); e != nil {
		return e
	}
	path := filepath.Join(a.State, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	deadline := time.Now().Add(30 * time.Second)
	for {
		acquired, err := tryFileLock(file)
		if err != nil {
			return err
		}
		if acquired {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("занят lock %s; проверьте другие adc процессы", path)
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer unlockFile(file)
	return fn()
}
func Abs(path string) (string, error) {
	p, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	return filepath.EvalSymlinks(p)
}

// Host paths are persisted and read by a Linux runtime even when the host is
// Windows. filepath.IsAbs alone uses the OS of the current binary.
func PortableAbs(path string) bool {
	return strings.HasPrefix(path, "/") || filepath.IsAbs(path) || len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') || strings.HasPrefix(path, `\\`)
}
func CheckoutID(path string) string {
	h := sha256.Sum256([]byte(path))
	return hex.EncodeToString(h[:4])
}
func RuntimePath(path, id string) string {
	if runtime.GOOS == "windows" {
		return "/workspace/projects/" + id
	}
	return filepath.ToSlash(path)
}
func (a *App) RequireRuntime() error {
	if !a.Runtime {
		return errors.New("команда доступна только внутри ADC runtime")
	}
	return nil
}

func (a *App) RequireHost() error {
	if a.Runtime {
		return errors.New("эта команда запускается на хосте")
	}
	return nil
}
func (a *App) Container(c Config) string { return "adc-" + c.Name }
func (a *App) ProjectPath(p Project) string {
	if a.Runtime {
		return p.RuntimePath
	}
	return p.Path
}
func (a *App) Project(c Config, key string) (Project, error) {
	for _, p := range c.Projects {
		if p.ID == key || a.ProjectPath(p) == key {
			return p, nil
		}
	}
	return Project{}, fmt.Errorf("проект %q не зарегистрирован: adc project add", key)
}
