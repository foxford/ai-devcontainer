package host

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}
type OrcaLock struct {
	Version string              `json:"version"`
	Assets  map[string]Artifact `json:"assets"`
}

func download(client *http.Client, asset Artifact, dest string) error {
	if len(asset.SHA256) != 64 || !strings.HasPrefix(asset.URL, "https://") {
		return errors.New("артефакт требует HTTPS и SHA-256")
	}
	if b, e := os.ReadFile(dest); e == nil {
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) == asset.SHA256 {
			return nil
		}
	}
	resp, e := client.Get(asset.URL)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download %s: HTTP %d", asset.URL, resp.StatusCode)
	}
	if e = os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	hash := sha256.New()
	_, e = io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, 2<<30))
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return errors.New("SHA-256 не совпадает; артефакт не установлен")
	}
	return os.Rename(f.Name(), dest)
}
func (a *App) Install(noOrca, noShell bool, root *cobra.Command) error {
	if e := a.shared().RequireHost(); e != nil {
		return e
	}
	// Register the command first so a required Docker reboot does not leave the
	// user unable to run the continuation suggested by the diagnostic.
	if !noShell {
		if e := a.InstallShell(root); e != nil {
			return e
		}
	}
	var pending *prerequisitePending
	if e := a.Prerequisites(); e != nil && !errors.As(e, &pending) {
		return e
	}
	if !noOrca {
		if e := a.InstallOrca(); e != nil {
			return e
		}
	}
	if _, e := os.Stat(filepath.Join(a.State, "workspace.json")); os.IsNotExist(e) {
		if e = a.Workspace([]string{"init"}); e != nil {
			return e
		}
	}
	if pending != nil {
		fmt.Println("Установка завершена. Перед workspace start:", pending.Error())
		return nil
	}
	if noShell {
		fmt.Println("Параллельная установка завершена. Используйте бинарник из ADC_HOME/bin с явными AI_DEVCONTAINER_HOME и ADC_HOME; PATH не изменён.")
	} else {
		fmt.Println("Установка завершена. Откройте новый терминал; adc project add <path>; adc workspace start")
	}
	return nil
}

// A reboot or a new login is a pending runtime prerequisite, not a reason to
// abandon installation of the command, completions, and desktop client.
type prerequisitePending struct{ message string }

func (p *prerequisitePending) Error() string { return p.message }

func adminRun(args ...string) error {
	if len(args) == 0 {
		return errors.New("пустая системная команда")
	}
	if os.Geteuid() == 0 {
		return shared.Run("", args[0], args[1:]...)
	}
	return shared.Run("", "sudo", args...)
}

func (a *App) Prerequisites() error {
	if _, e := exec.LookPath("git"); e != nil {
		return errors.New("Git не найден: bootstrap install.sh/install.ps1 устанавливает Git перед клонированием")
	}
	if _, e := exec.LookPath("docker"); e != nil {
		fmt.Println("Docker отсутствует — устанавливаю...")
		switch runtime.GOOS {
		case "darwin":
			if _, e = exec.LookPath("brew"); e != nil {
				return errors.New("для установки Docker Desktop нужен Homebrew; bootstrap установит его")
			}
			if e = shared.Run("", "brew", "install", "--cask", "docker"); e != nil {
				return e
			}
			if e = shared.Run("", "open", "-a", "Docker"); e != nil {
				return e
			}
		case "windows":
			if _, e = exec.LookPath("winget"); e != nil {
				return errors.New("нужен Windows App Installer (winget) для Docker Desktop; установите App Installer из Microsoft Store и повторите install.ps1")
			}
			if e = shared.Run("", "winget", "install", "--id", "Docker.DockerDesktop", "--exact", "--accept-package-agreements", "--accept-source-agreements"); e != nil {
				return e
			}
			return &prerequisitePending{"запустите Docker Desktop и перезапустите терминал; Windows может потребовать перезагрузку"}
		case "linux":
			var command []string
			if _, e = exec.LookPath("apt-get"); e == nil {
				if e = adminRun("apt-get", "update"); e != nil {
					return e
				}
				command = []string{"apt-get", "install", "-y", "docker.io"}
			} else if _, e = exec.LookPath("dnf"); e == nil {
				command = []string{"dnf", "install", "-y", "moby-engine"}
			} else if _, e = exec.LookPath("pacman"); e == nil {
				command = []string{"pacman", "-S", "--needed", "--noconfirm", "docker"}
			} else {
				return errors.New("нет поддерживаемого package manager: apt, dnf, pacman")
			}
			if e = adminRun(command...); e != nil {
				return e
			}
			if e = adminRun("systemctl", "enable", "--now", "docker"); e != nil {
				return e
			}
			if os.Geteuid() == 0 {
				break
			}
			user := os.Getenv("USER")
			if user == "" {
				return errors.New("USER не установлен")
			}
			if e = adminRun("usermod", "-aG", "docker", user); e != nil {
				return e
			}
			return &prerequisitePending{"войдите в систему заново, чтобы применить группу docker"}
		default:
			return errors.New("неподдерживаемая ОС")
		}
	}
	if _, e := shared.Output("", "docker", "info", "--format", "{{.OSType}}"); e != nil {
		if runtime.GOOS == "darwin" {
			_ = shared.Run("", "open", "-a", "Docker")
			for i := 0; i < 60; i++ {
				time.Sleep(time.Second)
				if _, e = shared.Output("", "docker", "info", "--format", "{{.OSType}}"); e == nil {
					return nil
				}
			}
		}
		return &prerequisitePending{"Docker установлен, но демон недоступен: запустите Docker Desktop/службу и проверьте adc doctor"}
	}
	return nil
}
func (a *App) InstallOrca() error {
	b, e := os.ReadFile(filepath.Join(a.Platform, "runtime", "orca-lock.json"))
	if e != nil {
		return e
	}
	var lock OrcaLock
	if e = json.Unmarshal(b, &lock); e != nil {
		return e
	}
	asset, ok := lock.Assets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return fmt.Errorf("Orca %s не публикует пакет %s/%s", lock.Version, runtime.GOOS, runtime.GOARCH)
	}
	dir := filepath.Join(a.State, "orca", lock.Version)
	dest := filepath.Join(dir, filepath.Base(asset.URL))
	fmt.Println("Orca", lock.Version, "— загрузка и проверка SHA-256")
	if e = download(&http.Client{Timeout: 15 * time.Minute}, asset, dest); e != nil {
		return e
	}
	switch runtime.GOOS {
	case "linux":
		if e = os.Chmod(dest, 0755); e != nil {
			return e
		}
		if _, e = os.Stat(filepath.Join(dir, "squashfs-root", "AppRun")); os.IsNotExist(e) {
			if e = shared.Run(dir, dest, "--appimage-extract"); e != nil {
				return e
			}
		}
		if e = installOrcaLibraries(filepath.Join(dir, "squashfs-root", "orca-ide")); e != nil {
			return e
		}
		// A desktop entry opens the host UI. adc orca remains the runtime CLI.
		home, _ := os.UserHomeDir()
		desktop := "[Desktop Entry]\nType=Application\nName=Orca (ADC)\nExec=\"" + filepath.Join(dir, "squashfs-root", "AppRun") + "\"\nTerminal=false\nCategories=Development;\n"
		if e = shared.AtomicWrite(filepath.Join(home, ".local", "share", "applications", "orca-adc.desktop"), []byte(desktop), 0644); e != nil {
			return e
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		if e = extractZip(dest, filepath.Join(home, "Applications")); e != nil {
			return e
		}
	case "windows":
		if e = shared.Run("", dest, "/S"); e != nil {
			return e
		}
	}
	return shared.AtomicWrite(filepath.Join(a.State, "orca-version"), []byte(lock.Version), 0644)
}
func installOrcaLibraries(binary string) error {
	cmd := exec.Command("ldd", binary)
	data, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("проверка библиотек Orca: %w: %s", err, data)
	}
	if !strings.Contains(string(data), "not found") {
		return nil
	}
	fmt.Println("Orca: устанавливаю отсутствующие системные библиотеки")
	if _, err = exec.LookPath("apt-get"); err == nil {
		if err = adminRun("apt-get", "update"); err != nil {
			return err
		}
		packages := []string{"libgtk-3-0", "libnss3", "libasound2", "libgbm1", "libxss1", "libxtst6"}
		for i, name := range packages {
			// Ubuntu 24.04+ transitioned these libraries to time64 packages.
			if _, e := shared.Output("", "apt-cache", "show", name+"t64"); e == nil {
				packages[i] = name + "t64"
			}
		}
		err = adminRun(append([]string{"apt-get", "install", "-y"}, packages...)...)
	} else if _, err = exec.LookPath("dnf"); err == nil {
		err = adminRun("dnf", "install", "-y", "gtk3", "nss", "alsa-lib", "mesa-libgbm", "libXScrnSaver", "libXtst")
	} else if _, err = exec.LookPath("pacman"); err == nil {
		err = adminRun("pacman", "-S", "--needed", "--noconfirm", "gtk3", "nss", "alsa-lib", "mesa", "libxss", "libxtst")
	} else {
		return fmt.Errorf("нет поддерживаемого package manager для библиотек Orca: %s", data)
	}
	if err != nil {
		return err
	}
	data, err = exec.Command("ldd", binary).CombinedOutput()
	if err != nil || strings.Contains(string(data), "not found") {
		return fmt.Errorf("после установки остались отсутствующие библиотеки Orca: %s", data)
	}
	return nil
}

func extractZip(src, dest string) error {
	r, e := zip.OpenReader(src)
	if e != nil {
		return e
	}
	defer r.Close()
	for _, f := range r.File {
		path := filepath.Join(dest, filepath.FromSlash(f.Name))
		rel, e := filepath.Rel(dest, path)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return errors.New("zip path выходит за назначение")
		}
		if f.FileInfo().IsDir() {
			if e = os.MkdirAll(path, f.Mode().Perm()); e != nil {
				return e
			}
			continue
		}
		rc, e := f.Open()
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(rc, 1<<30))
		rc.Close()
		if e != nil {
			return e
		}
		if f.Mode()&os.ModeSymlink != 0 {
			if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
				return e
			}
			if _, e = os.Lstat(path); e == nil {
				if e = os.Remove(path); e != nil {
					return e
				}
			}
			if e = os.Symlink(string(b), path); e != nil {
				return e
			}
		} else if e = shared.AtomicWrite(path, b, f.Mode().Perm()); e != nil {
			return e
		}
	}
	return nil
}
func (a *App) InstallShell(root *cobra.Command) error {
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	bin := filepath.Join(home, ".local", "bin")
	name := "adc"
	if runtime.GOOS == "windows" {
		bin = filepath.Join(a.State, "bin")
		name += ".exe"
	}
	b, e := os.ReadFile(exe)
	if e != nil {
		return e
	}
	if e = installCommandFile(filepath.Join(bin, name), b); e != nil {
		return e
	}
	comp := filepath.Join(a.State, "completions")
	if e = os.MkdirAll(comp, 0755); e != nil {
		return e
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		data, err := shared.CompletionScript(root, shell, true)
		if err != nil {
			return err
		}
		if e = shared.AtomicWrite(filepath.Join(comp, shared.CompletionFilename(shell)), data, 0644); e != nil {
			return e
		}
	}
	psBody := "$env:ADC_HOME='" + strings.ReplaceAll(a.State, "'", "''") + "'\n$env:AI_DEVCONTAINER_HOME='" + strings.ReplaceAll(a.Platform, "'", "''") + "'\n. '" + strings.ReplaceAll(filepath.Join(comp, shared.CompletionFilename("powershell")), "'", "''") + "'"
	psInstalled := false
	for _, command := range []string{"powershell", "pwsh"} {
		if _, err := exec.LookPath(command); err != nil {
			continue
		}
		script := "$PROFILE.CurrentUserAllHosts"
		if runtime.GOOS == "windows" {
			script = "$p=[Environment]::GetEnvironmentVariable('Path','User'); $b='" + strings.ReplaceAll(bin, "'", "''") + "'; if (($p -split ';') -notcontains $b) {[Environment]::SetEnvironmentVariable('Path', ($b+';'+$p), 'User')}; " +
				"[Environment]::SetEnvironmentVariable('ADC_HOME','" + strings.ReplaceAll(a.State, "'", "''") + "','User'); " +
				"[Environment]::SetEnvironmentVariable('AI_DEVCONTAINER_HOME','" + strings.ReplaceAll(a.Platform, "'", "''") + "','User'); " + script
		}
		profile, err := shared.Output("", command, "-NoProfile", "-Command", script)
		if err != nil {
			return err
		}
		if err = shared.ManagedBlock(profile, "# adc:start", "# adc:end", psBody); err != nil {
			return err
		}
		psInstalled = true
	}
	if runtime.GOOS == "windows" {
		if !psInstalled {
			return errors.New("PowerShell не найден для установки PATH и completion")
		}
		return nil
	}
	env := "export PATH=" + shared.ShellQuote(bin) + ":\"$PATH\"\nexport AI_DEVCONTAINER_HOME=" + shared.ShellQuote(a.Platform) + "\nexport ADC_HOME=" + shared.ShellQuote(a.State)
	for _, path := range []string{filepath.Join(home, ".profile"), filepath.Join(home, ".bash_profile"), filepath.Join(home, ".bashrc")} {
		body := env
		if filepath.Base(path) != ".profile" {
			body += "\nif [ -n \"${BASH_VERSION:-}\" ]; then . " + shared.ShellQuote(filepath.Join(comp, "adc.bash")) + "; fi"
		}
		if e = shared.ManagedBlock(path, "# adc:start", "# adc:end", body); e != nil {
			return e
		}
	}
	zdir := os.Getenv("ZDOTDIR")
	if zdir == "" {
		zdir = home
	}
	zbody := env + "\nif ! command -v compdef >/dev/null 2>&1; then autoload -Uz compinit; compinit; fi\n. " + shared.ShellQuote(filepath.Join(comp, "adc.zsh"))
	if e = shared.ManagedBlock(filepath.Join(zdir, ".zshrc"), "# adc:start", "# adc:end", zbody); e != nil {
		return e
	}
	fishCompletion, e := a.shared().CompletionPath("fish")
	if e != nil {
		return e
	}
	fish := filepath.Dir(filepath.Dir(fishCompletion))
	if e = shared.AtomicWrite(filepath.Join(fish, "conf.d", "adc.fish"), []byte("fish_add_path "+shared.ShellQuote(bin)+"\nset -gx AI_DEVCONTAINER_HOME "+shared.ShellQuote(a.Platform)+"\nset -gx ADC_HOME "+shared.ShellQuote(a.State)+"\n"), 0644); e != nil {
		return e
	}
	b, e = os.ReadFile(filepath.Join(comp, "adc.fish"))
	if e != nil {
		return e
	}
	return shared.AtomicWrite(fishCompletion, b, 0644)
}
