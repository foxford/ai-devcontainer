package host

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// ReplaceLegacyInstallation is invoked by the newly compiled bootstrap before
// installing the command. Agent homes, checkouts and Docker volumes are not
// modified here. The old platform gets a verified copy before its Git switch.
func (a *App) ReplaceLegacyInstallation(target string) error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	return a.shared().Locked(func() error {
		return replaceLegacyInstallation(a.Platform, a.State, target, legacyPlatformStopped)
	})
}

func legacyPlatformStopped(target string) error {
	ids, err := shared.Output("", "docker", "ps", "-q")
	if err != nil {
		return fmt.Errorf("перед заменой платформы нужен доступ к Docker: %w", err)
	}
	if ids == "" {
		return nil
	}
	containers, err := inspectLegacy(strings.Fields(ids)...)
	if err != nil {
		return err
	}
	return checkLegacyPlatformMounts(target, containers)
}

func checkLegacyPlatformMounts(target string, containers []legacyInspection) error {
	target, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("не удалось определить путь платформы: %w", err)
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("не удалось разрешить путь платформы: %w", err)
	}
	for _, container := range containers {
		for _, mount := range container.Mounts {
			source := mount.Source
			if resolved, err := filepath.EvalSymlinks(source); err == nil {
				source = resolved
			}
			if mount.Type == "bind" && overlappingLegacyPaths(source, target) {
				return fmt.Errorf("платформу использует %s: сохраните работу и выполните docker stop %s, затем повторите обновление", strings.TrimPrefix(container.Name, "/"), strings.TrimPrefix(container.Name, "/"))
			}
		}
	}
	return nil
}

type legacyPlatformBackup struct {
	Version int    `json:"version"`
	Source  string `json:"source"`
	OldRev  string `json:"oldRev"`
	NewRev  string `json:"newRev"`
	Digest  string `json:"digest"`
}

func replaceLegacyInstallation(source, state, target string, stopped func(string) error) error {
	target, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	gitDir, err := os.Lstat(filepath.Join(target, ".git"))
	if err != nil || !gitDir.IsDir() {
		return errors.New("для замены старой платформы нужен самостоятельный Git-клон с каталогом .git")
	}
	if info, err := os.Stat(filepath.Join(target, "bin", "adc")); err != nil || !info.Mode().IsRegular() {
		return errors.New("по указанному пути нет старого Bash adc")
	}
	if _, err := os.Lstat(filepath.Join(target, "go.mod")); !os.IsNotExist(err) {
		return errors.New("платформа уже новая; повторите обычную установку")
	}
	state, err = filepath.Abs(state)
	if err != nil {
		return err
	}
	state, err = resolveLegacyInstallPath(state)
	if err != nil {
		return err
	}
	if overlappingLegacyPaths(target, state) {
		return errors.New("ADC_HOME для резервной копии должен находиться вне клона платформы")
	}
	if err = os.MkdirAll(state, 0700); err != nil {
		return err
	}
	root, err := shared.Output("", "git", "-C", target, "rev-parse", "--show-toplevel")
	if err != nil || !strings.EqualFold(filepath.Clean(root), filepath.Clean(target)) {
		return errors.New("путь старой платформы должен указывать на корень Git-клона")
	}
	dirty, err := shared.Output("", "git", "--no-optional-locks", "-C", target, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if dirty != "" {
		return errors.New("в старом клоне платформы есть незакоммиченные изменения: сохраните их перед обновлением")
	}
	if err = stopped(target); err != nil {
		return err
	}
	newRev, err := shared.Output("", "git", "-C", source, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	oldRev, err := shared.Output("", "git", "-C", target, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	// Fetch objects without modifying the old working tree. Use the exact commit
	// that built this bootstrap, even if origin/main advances during installation.
	if err = shared.Run("", "git", "-C", target, "fetch", "origin", "main:refs/remotes/origin/main"); err != nil {
		return err
	}
	if _, err = shared.Output("", "git", "-C", target, "cat-file", "-e", newRev+":go.mod"); err != nil {
		return errors.New("новый bootstrap и origin старого клона относятся к разным ревизиям; исходные файлы не заменены")
	}
	mainExists := false
	if _, err = shared.Output("", "git", "-C", target, "show-ref", "--verify", "refs/heads/main"); err == nil {
		mainExists = true
		base, err := shared.Output("", "git", "-C", target, "merge-base", "main", newRev)
		if err != nil {
			return err
		}
		mainRev, err := shared.Output("", "git", "-C", target, "rev-parse", "main")
		if err != nil || base != mainRev {
			return errors.New("локальная main содержит свои коммиты; автоматический переход не сбрасывает ветку")
		}
	}
	backupRoot := filepath.Join(state, "backups", "platform-v0")
	backupRoot, err = resolveLegacyInstallPath(backupRoot)
	if err != nil {
		return err
	}
	if overlappingLegacyPaths(target, backupRoot) {
		return errors.New("каталог резервных копий ссылается внутрь исходной платформы")
	}
	if err = os.MkdirAll(backupRoot, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(backupRoot, time.Now().UTC().Format("20060102T150405Z")+"-")
	if err != nil {
		return err
	}
	checkout := filepath.Join(dir, "checkout")
	if err = copyLegacyPlatform(target, checkout); err != nil {
		return fmt.Errorf("копия платформы не завершена (%s); оригинал не заменён: %w", dir, err)
	}
	digest, err := legacyPlatformDigest(target)
	if err != nil {
		return err
	}
	copyDigest, err := legacyPlatformDigest(checkout)
	if err != nil || digest != copyDigest {
		return fmt.Errorf("копия платформы не прошла проверку (%s); оригинал не заменён", dir)
	}
	if err = stopped(target); err != nil {
		return err
	}
	dirty, err = shared.Output("", "git", "--no-optional-locks", "-C", target, "status", "--porcelain", "--untracked-files=all")
	if err != nil || dirty != "" {
		return errors.New("старый клон изменился во время копирования; переход остановлен")
	}
	currentHead, err := shared.Output("", "git", "-C", target, "rev-parse", "HEAD")
	if err != nil || currentHead != oldRev {
		return errors.New("ветка старой платформы изменилась во время копирования; переход остановлен")
	}
	// Recheck the full snapshot before any change to the working tree.
	currentDigest, err := legacyPlatformDigest(target)
	if err != nil || currentDigest != digest {
		return errors.New("старый клон изменился после проверки копии; повторите обновление")
	}
	metadata, err := json.MarshalIndent(legacyPlatformBackup{Version: 1, Source: target, OldRev: oldRev, NewRev: newRev, Digest: digest}, "", "  ")
	if err != nil {
		return err
	}
	if err = shared.AtomicWrite(filepath.Join(dir, "backup.json"), append(metadata, '\n'), 0600); err != nil {
		return err
	}
	fmt.Println("Проверенная копия старой платформы:", dir)
	if mainExists {
		if err = shared.Run("", "git", "-C", target, "switch", "main"); err != nil {
			return err
		}
		if err = shared.Run("", "git", "-C", target, "merge", "--ff-only", newRev); err != nil {
			return err
		}
	} else if err = shared.Run("", "git", "-C", target, "switch", "-c", "main", newRev); err != nil {
		return err
	}
	if err = shared.Run("", "git", "-C", target, "branch", "--set-upstream-to=origin/main", "main"); err != nil {
		return err
	}
	fmt.Println("Платформа обновлена. Данные агентов и старые тома сохранены; следующий шаг: adc migrate legacy plan.")
	return nil
}

// Resolve symlinks in the nearest existing parent before creating directories.
// A backup symlink into the source must never cause recursive copying.
func resolveLegacyInstallPath(path string) (string, error) {
	parent := path
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) || filepath.Dir(parent) == parent {
			return "", err
		}
		tail = append(tail, filepath.Base(parent))
		parent = filepath.Dir(parent)
	}
}

func copyLegacyPlatform(source, dest string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.Mkdir(out, 0700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, out)
		case info.Mode().IsRegular():
			input, err := os.Open(path)
			if err != nil {
				return err
			}
			defer input.Close()
			output, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600|info.Mode().Perm()&0100)
			if err != nil {
				return err
			}
			_, err = io.Copy(output, input)
			if err == nil {
				err = output.Sync()
			}
			closeErr := output.Close()
			if err != nil {
				return err
			}
			return closeErr
		default:
			return fmt.Errorf("неподдерживаемый тип файла: %s", rel)
		}
	})
}

func legacyPlatformDigest(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%q\x00", filepath.ToSlash(rel))
		switch {
		case entry.IsDir():
			io.WriteString(hash, "dir\x00")
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(hash, "link:%q\x00", link)
		case info.Mode().IsRegular():
			fmt.Fprintf(hash, "file:%d:%d\x00", info.Size(), info.Mode().Perm()&0100)
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(hash, f)
			f.Close()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("неподдерживаемый тип файла: %s", rel)
		}
		io.WriteString(hash, "\x00")
		return nil
	})
	return hex.EncodeToString(hash.Sum(nil)), err
}
