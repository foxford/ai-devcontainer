package shared

import (
	"archive/tar"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const updateCheckTTL = 24 * time.Hour
const updateFailureTTL = time.Hour
const publicUpdateRemote = "https://github.com/foxford/ai-devcontainer.git"

// UpdateStatus compares explicit unit versions in the platform and its remote.
type UpdateStatus struct {
	Available   bool             `json:"available"`
	CheckedAt   time.Time        `json:"checkedAt,omitempty"`
	AttemptedAt time.Time        `json:"attemptedAt,omitempty"`
	Changes     []DeliveryChange `json:"changes"`
	Error       string           `json:"error,omitempty"`
	Cached      bool             `json:"cached"`
}

type updateCache struct {
	Version        int              `json:"version"`
	Source         string           `json:"source"`
	RemoteRevision string           `json:"remoteRevision,omitempty"`
	CheckedAt      time.Time        `json:"checkedAt,omitempty"`
	AttemptedAt    time.Time        `json:"attemptedAt,omitempty"`
	Error          string           `json:"error,omitempty"`
	Versions       DeliveryVersions `json:"versions"`
}

type updateSource struct{ remote, ref string }

func updateGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := updateGitCommand(ctx, dir, args...)
	data, err := cmd.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return strings.TrimSpace(string(data)), err
}

func updateGitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	// A broad user insteadOf rule may otherwise turn our public HTTPS read
	// back into SSH. Override only this exact repository, for this command.
	args = append([]string{"-c", "url." + publicUpdateRemote + ".insteadOf=" + publicUpdateRemote}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Remote inspection never prompts, executes repository hooks or inherits
	// a caller's alternate Git worktree/index environment.
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "GIT_TERMINAL_PROMPT" && key != "GIT_DIR" && key != "GIT_WORK_TREE" && key != "GIT_INDEX_FILE" && key != "GIT_ASKPASS" && key != "SSH_ASKPASS" && key != "GCM_INTERACTIVE" {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=", "GCM_INTERACTIVE=Never")
	cmd.WaitDelay = time.Second
	return cmd
}

func (a *App) updateSource(ctx context.Context) (updateSource, error) {
	var source updateSource
	var err error
	branch, err := updateGit(ctx, a.Platform, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return source, errors.New("платформа не на ветке: проверка требует tracking branch")
	}
	remote, err := updateGit(ctx, a.Platform, "config", "--get", "branch."+branch+".remote")
	if err != nil || remote == "" {
		return source, errors.New("у ветки платформы нет tracking remote")
	}
	source.ref, err = updateGit(ctx, a.Platform, "config", "--get", "branch."+branch+".merge")
	if err != nil || !strings.HasPrefix(source.ref, "refs/heads/") || strings.ContainsAny(source.ref, "\r\n") {
		return source, errors.New("у ветки платформы нет корректной tracking branch")
	}
	if remote == "." {
		source.remote = a.Platform
	} else {
		source.remote, err = updateGit(ctx, a.Platform, "remote", "get-url", remote)
		if err != nil || source.remote == "" || strings.HasPrefix(source.remote, "-") || strings.ContainsAny(source.remote, "\r\n") {
			return source, errors.New("не удалось прочитать адрес tracking remote платформы")
		}
	}
	// A relative local remote is relative to the original platform checkout.
	if !PortableAbs(source.remote) && !strings.Contains(source.remote, ":") {
		source.remote = filepath.Join(a.Platform, source.remote)
	}
	source.remote = updateReadRemote(source.remote)
	return source, nil
}

func updateReadRemote(remote string) string {
	// The official platform is public. Reading its versions must also work
	// inside a runtime without the host's SSH keys or known_hosts. Keep other
	// remotes (including private forks) and the checkout's Git config intact.
	switch strings.TrimSuffix(remote, ".git") {
	case "git@github.com:foxford/ai-devcontainer",
		"ssh://git@github.com/foxford/ai-devcontainer",
		"ssh://git@github.com:22/foxford/ai-devcontainer":
		return publicUpdateRemote
	default:
		return remote
	}
}

func (a *App) readUpdateCache(source updateSource) (updateCache, string) {
	file := filepath.Join(a.State, "cache", "updates", "status.json")
	var cached updateCache
	data, err := os.ReadFile(file)
	identity := source.remote + "\n" + source.ref
	if err != nil || json.Unmarshal(data, &cached) != nil || cached.Version != 1 || cached.Source != identity {
		cached = updateCache{Version: 1, Source: identity}
	}
	return cached, file
}

func updateStatus(cached updateCache, current DeliveryVersions, fromCache bool) UpdateStatus {
	status := UpdateStatus{CheckedAt: cached.CheckedAt, AttemptedAt: cached.AttemptedAt, Error: cached.Error, Cached: fromCache, Changes: []DeliveryChange{}}
	if cached.Versions != nil {
		status.Changes = DiffDeliveryVersions(current, cached.Versions)
	}
	status.Available = len(status.Changes) > 0
	return status
}

func updateCacheFresh(cached updateCache, now time.Time) bool {
	ttl := updateCheckTTL
	if cached.Error != "" {
		ttl = updateFailureTTL
	}
	return !cached.AttemptedAt.IsZero() && !now.Before(cached.AttemptedAt) && now.Sub(cached.AttemptedAt) < ttl
}

// CheckUpdates fetches into a separate bare cache, never into the platform or a
// project. This also works with the runtime's read-only platform bind mount.
func (a *App) CheckUpdates(ctx context.Context, force bool) (status UpdateStatus, resultErr error) {
	defer func() {
		if resultErr != nil && status.Error == "" {
			status.Error = resultErr.Error()
		}
	}()
	source, err := a.updateSource(ctx)
	if err != nil {
		return UpdateStatus{Error: err.Error(), Changes: []DeliveryChange{}}, err
	}
	current, err := a.DeliveryVersions()
	if err != nil {
		return UpdateStatus{Error: err.Error(), Changes: []DeliveryChange{}}, err
	}
	cached, file := a.readUpdateCache(source)
	if !force && updateCacheFresh(cached, time.Now()) {
		status := updateStatus(cached, current, true)
		if cached.Error != "" {
			return status, errors.New(cached.Error)
		}
		return status, nil
	}
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return updateStatus(cached, current, true), err
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(file), "check.lockfile"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return updateStatus(cached, current, true), err
	}
	defer lock.Close()
	acquired, err := tryFileLock(lock)
	if err != nil || !acquired {
		if err == nil && cached.Versions == nil {
			err = errors.New("проверка обновлений уже выполняется; повторите adc updates")
		}
		return updateStatus(cached, current, true), err
	}
	defer unlockFile(lock)
	// A previous process may have refreshed the cache before this lock.
	cached, _ = a.readUpdateCache(source)
	if !force && updateCacheFresh(cached, time.Now()) {
		if cached.Error != "" {
			return updateStatus(cached, current, true), errors.New(cached.Error)
		}
		return updateStatus(cached, current, true), nil
	}
	cached.AttemptedAt = time.Now().UTC()
	refreshed, checkErr := a.fetchUpdate(ctx, source, cached, filepath.Dir(file))
	if checkErr != nil {
		cached.Error = checkErr.Error()
	} else {
		cached = refreshed
		cached.CheckedAt = time.Now().UTC()
		cached.Error = ""
	}
	data, err := json.MarshalIndent(cached, "", "  ")
	if err == nil {
		err = AtomicWrite(file, append(data, '\n'), 0600)
	}
	if checkErr == nil {
		checkErr = err
	}
	return updateStatus(cached, current, false), checkErr
}

func (a *App) fetchUpdate(ctx context.Context, source updateSource, cached updateCache, directory string) (updateCache, error) {
	refs, err := updateGit(ctx, a.Platform, "ls-remote", "--exit-code", "--", source.remote, source.ref)
	if err != nil {
		return cached, fmt.Errorf("не удалось проверить remote платформы: %w", updateCheckError(ctx, err))
	}
	fields := strings.Fields(refs)
	if len(fields) != 2 || fields[1] != source.ref || !validUpdateRevision(fields[0]) {
		return cached, errors.New("remote не вернул ожидаемую ветку платформы")
	}
	if cached.RemoteRevision == fields[0] && cached.Versions != nil {
		return cached, nil
	}
	repository := filepath.Join(directory, "remote.git")
	if _, err := os.Stat(filepath.Join(repository, "HEAD")); os.IsNotExist(err) {
		if _, err = updateGit(ctx, directory, "init", "--bare", "--quiet", repository); err != nil {
			return cached, fmt.Errorf("не удалось подготовить кэш обновлений: %w", updateCheckError(ctx, err))
		}
	}
	if _, err = updateGit(ctx, directory, "--git-dir", repository, "-c", "core.hooksPath="+os.DevNull, "fetch", "--quiet", "--no-tags", "--depth=1", "--", source.remote, source.ref); err != nil {
		return cached, fmt.Errorf("не удалось получить обновления платформы: %w", updateCheckError(ctx, err))
	}
	revision, err := updateGit(ctx, directory, "--git-dir", repository, "rev-parse", "--verify", "FETCH_HEAD")
	if err != nil || !validUpdateRevision(revision) {
		return cached, errors.New("не удалось прочитать версию полученного обновления")
	}
	staging, err := os.MkdirTemp(directory, ".metadata-*")
	if err != nil {
		return cached, err
	}
	defer os.RemoveAll(staging)
	paths, err := updateGit(ctx, directory, "--git-dir", repository, "ls-tree", "-r", "--name-only", revision, "delivery")
	if err != nil || paths == "" {
		return cached, errors.New("в обновлении отсутствует каталог доставки")
	}
	var metadata []string
	for _, name := range strings.Split(paths, "\n") {
		if updateMetadataPath(name) {
			metadata = append(metadata, name)
		}
	}
	if len(metadata) == 0 {
		return cached, errors.New("в обновлении отсутствуют метафайлы версий доставки")
	}
	args := append([]string{"--git-dir", repository, "archive", "--format=tar", revision, "--"}, metadata...)
	cmd := updateGitCommand(ctx, directory, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return cached, err
	}
	if err = cmd.Start(); err != nil {
		return cached, err
	}
	extractErr := extractUpdateArchive(ctx, staging, stdout)
	if extractErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if extractErr != nil {
		return cached, extractErr
	}
	if waitErr != nil {
		return cached, fmt.Errorf("не удалось прочитать доставку обновления: %w", updateCheckError(ctx, waitErr))
	}
	versions, err := (&App{Platform: staging}).DeliveryVersions()
	if err != nil {
		return cached, fmt.Errorf("не удалось проверить каталог обновления: %w", err)
	}
	cached.RemoteRevision, cached.Versions = revision, versions
	return cached, nil
}

func updateMetadataPath(name string) bool {
	return strings.HasPrefix(name, "delivery/") && IsDeliveryMetadata(filepath.FromSlash(name))
}

func updateCheckError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Git stderr may contain credentials embedded in a configured remote URL.
	return err
}

func validUpdateRevision(revision string) bool {
	if len(revision) != 40 && len(revision) != 64 {
		return false
	}
	_, err := hex.DecodeString(revision)
	return err == nil
}

func extractUpdateArchive(ctx context.Context, root string, input io.Reader) error {
	return extractDeliveryFiles(ctx, root, input, true)
}

func extractDeliveryFiles(ctx context.Context, root string, input io.Reader, metadataOnly bool) error {
	reader := tar.NewReader(input)
	var total int64
	maxFiles, maxFile, maxTotal := 5000, int64(64<<10), int64(8<<20)
	if !metadataOnly {
		maxFiles, maxFile, maxTotal = 20000, 16<<20, 128<<20
	}
	for count := 0; ; count++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(header.Name)
		if count >= maxFiles || header.Size > maxFile || header.Size < 0 || total+header.Size > maxTotal {
			return errors.New("каталог обновления превышает ограничение размера")
		}
		total += header.Size
		// Git archive records its commit ID in a global PAX header. It does
		// not represent a filesystem entry and must never be extracted.
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if strings.ContainsAny(header.Name, "\\:\x00") || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || !(name == "delivery" || strings.HasPrefix(name, "delivery/")) {
			return errors.New("небезопасный путь в каталоге обновления")
		}
		destination := filepath.Join(root, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(destination, 0755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if metadataOnly && !updateMetadataPath(name) {
				return errors.New("архив проверки должен содержать только метафайлы версий")
			}
			if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
				return err
			}
			mode := os.FileMode(0644)
			if !metadataOnly && header.Mode&0111 != 0 {
				mode = 0755
			}
			file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return errors.New("ссылки и специальные файлы в архиве обновления не поддерживаются")
		}
	}
}

func WriteUpdateStatus(out io.Writer, status UpdateStatus, color bool) error {
	if status.Available {
		if err := WriteDeliveryChanges(out, "ADC: доступны обновления", status.Changes, color, false); err != nil {
			return err
		}
		fmt.Fprintln(out, "  Применить: adc update --delivery. Без пересборки образа и перезапуска workspace.")
	} else if status.Error == "" {
		fmt.Fprintln(out, "ADC: доступных обновлений нет.")
	}
	if !status.CheckedAt.IsZero() {
		label := "Проверено"
		if status.Cached || status.Error != "" {
			label = "Последняя успешная проверка"
		}
		fmt.Fprintf(out, "  %s: %s\n", label, status.CheckedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if status.Error != "" {
		_, err := fmt.Fprintln(out, noticeColor("  Проверка недоступна: "+noticeText(status.Error)+". Повторить: adc updates --refresh", "31", color))
		return err
	}
	return nil
}

// ShowUpdateNotice is best effort and never blocks shell/agent startup on a
// remote failure. New shells retry at most daily (hourly after an error).
func (a *App) ShowUpdateNotice(out io.Writer) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	status, _ := a.CheckUpdates(ctx, false)
	if status.Available {
		_ = WriteUpdateStatus(out, status, ColorEnabled(out))
		fmt.Fprintln(out)
	}
}
