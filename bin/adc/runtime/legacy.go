package runtime

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/legacy"
)

// LegacyArchive streams a mount without following links. A changed regular file
// fails the snapshot instead of returning a deceptively successful tar.
func LegacyArchive(root string, out io.Writer) error {
	tw := tar.NewWriter(out)
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(file)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			fmt.Fprintln(os.Stderr, "Backup пропускает временный special file:", file)
			return nil
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(file)
			if err != nil {
				return err
			}
		}
		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		if err = tw.WriteHeader(h); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err = io.CopyN(tw, f, info.Size()); err != nil {
			return err
		}
		after, err := f.Stat()
		if err != nil {
			return err
		}
		if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return fmt.Errorf("файл изменился во время backup: %s", file)
		}
		return f.Close()
	})
	if err != nil {
		_ = tw.Close()
		return err
	}
	return tw.Close()
}

type legacyImportReport struct {
	Version       int      `json:"version"`
	Account       string   `json:"account"`
	BackupCreated string   `json:"backupCreated"`
	Files         int      `json:"importedFiles"`
	Conflicts     []string `json:"conflicts"`
	Links         []string `json:"archivedLinks"`
	Notes         []string `json:"notes"`
	Complete      bool     `json:"complete"`
	Fingerprint   string   `json:"fingerprint"`
}

func legacyHomeMount(destination string) (string, string) {
	for _, suffix := range []string{".config/opencode", ".local/share/opencode", ".claude.json", ".claude", ".codex", ".hermes", ".dsh", ".history"} {
		if strings.HasSuffix(destination, "/"+suffix) {
			return suffix, strings.TrimSuffix(destination, "/"+suffix)
		}
	}
	for _, suffix := range []string{"obsidian", "obsidian-vault"} {
		if strings.HasSuffix(destination, "/"+suffix) {
			return suffix, strings.TrimSuffix(destination, "/"+suffix)
		}
	}
	return "", ""
}

func legacyManagedFile(name string) bool {
	for _, prefix := range []string{".claude/skills", ".codex/skills", ".hermes/skills", ".dsh/skills", ".config/opencode/skills", ".hermes/hermes-agent", ".hermes/.venv"} {
		if name == prefix || strings.HasPrefix(name, prefix+"/") {
			return true
		}
	}
	return false
}
func legacyHistory(name string) bool {
	for _, prefix := range []string{".claude/projects/", ".codex/sessions/", ".codex/archived_sessions/"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func emptyLegacyDestination(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("назначение %s не пустое; существующие данные не перезаписываются", root)
	}
	return nil
}

// writeLegacyFile only writes to private, freshly created staging trees. It
// detects collisions and never follows archive links or destination links.
func writeLegacyFile(stage, name string, data []byte) (bool, error) {
	return writeLegacyReader(stage, name, bytes.NewReader(data))
}

func writeLegacyReader(stage, name string, source io.Reader) (bool, error) {
	name, err := legacy.CleanName(name)
	if err != nil || name == "." {
		return false, errors.New("некорректное назначение импорта")
	}
	file := filepath.Join(stage, filepath.FromSlash(name))
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		old, err := os.Open(file)
		if err != nil {
			return false, err
		}
		defer old.Close()
		existing, incoming := sha256.New(), sha256.New()
		size, err := io.Copy(existing, old)
		if err != nil {
			return false, err
		}
		otherSize, err := io.Copy(incoming, source)
		if err != nil {
			return false, err
		}
		return size == otherSize && bytes.Equal(existing.Sum(nil), incoming.Sum(nil)), nil
	}
	if err != nil {
		return false, err
	}
	_, err = io.Copy(f, source)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return false, err
	}
	return true, closeErr
}

func mergeLegacyHistory(stage, name string, source *os.File, size int64) error {
	equal, err := writeLegacyReader(stage, name, io.NewSectionReader(source, 0, size))
	if err != nil || equal {
		return err
	}
	file := filepath.Join(stage, filepath.FromSlash(name))
	f, err := os.OpenFile(file, os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 0 {
		var last [1]byte
		if _, err = f.ReadAt(last[:], info.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			if _, err = f.Write([]byte("\n")); err != nil {
				return err
			}
		}
	}
	if _, err = io.Copy(f, io.NewSectionReader(source, 0, size)); err != nil {
		return err
	}
	return f.Sync()
}

func legacyTextHistory(name string) bool {
	return shared.Contains([]string{".history/.bash_history", ".history/.zsh_history", ".codex/history.jsonl", ".claude/history.jsonl"}, name)
}

func readLegacyTar(file, expectedSHA string, visit func(*tar.Header, string, io.Reader) error) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	reader := io.TeeReader(f, hash)
	tr := tar.NewReader(reader)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			if _, err = io.Copy(io.Discard, reader); err != nil {
				return err
			}
			if hex.EncodeToString(hash.Sum(nil)) != expectedSHA {
				return errors.New("архив изменился во время копирования")
			}
			return nil
		}
		if err != nil {
			return err
		}
		name, err := legacy.CleanName(h.Name)
		if err != nil {
			return err
		}
		if err = visit(h, name, tr); err != nil {
			return err
		}
	}
}

// ImportLegacy promotes one explicitly selected account/settings profile. Every
// project's data profile is retained separately; regular Claude/Codex session
// files also merge into the shared home when their names do not conflict.
func ImportLegacy(dir, account, home, tools string, uid, gid int) error {
	b, err := legacy.Verify(dir)
	if err != nil {
		return err
	}
	found := false
	for _, c := range b.Plan.Containers {
		found = found || c.Project == account
	}
	if !found {
		return errors.New("неизвестный профиль --account")
	}
	if err = emptyLegacyDestination(home); err != nil {
		return err
	}
	if err = emptyLegacyDestination(tools); err != nil {
		return err
	}
	homeStage, err := os.MkdirTemp(home, ".adc-legacy-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(homeStage)
	toolStage, err := os.MkdirTemp(tools, ".adc-legacy-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(toolStage)
	report := legacyImportReport{Version: legacy.Version, Account: account, BackupCreated: b.Created, Fingerprint: legacy.Fingerprint(b), Notes: []string{
		"Оригинальные репозитории, включая Git, untracked и ignored файлы, сохранены в backup; импорт не изменяет checkout.",
		"Docker-in-Docker, кеши и writable layers сохранены в backup; их сервисы автоматически не запускаются.",
		"Исходные данные профилей находятся в .adc-legacy/<project>/home; аккаунты и настройки общего home выбраны через --account.",
		"Claude/Codex session files скопированы без изменения cwd и содержимого. Для старого cwd может потребоваться явный resume по ID; SQLite и другие общие базы не объединяются.",
		"Ссылки и управляемые skills/venv остаются в исходных архивах; новая платформа доставляет свои инструменты и навыки.",
	}}
	sharedTools := map[string]bool{}
	hermesLinks := map[string]bool{}
	copied := map[string]bool{}
	copyEntry := func(project, normalized string, h *tar.Header, reader io.Reader) error {
		if normalized == "" || normalized == "." {
			return nil
		}
		if h.Typeflag == tar.TypeSymlink || h.Typeflag == tar.TypeLink {
			report.Links = append(report.Links, project+":"+normalized)
			if normalized == ".hermes/auth.json" {
				if h.Linkname != "/opt/ai-tools/share/hermes/auth.json" {
					return fmt.Errorf("неизвестная ссылка авторизации Hermes для %s; сохранена в backup", project)
				}
				hermesLinks[project] = true
			} else if shared.Contains(authFiles, normalized) || normalized == ".local/share/opencode/auth.json" {
				return fmt.Errorf("ссылка авторизации %s:%s требует отдельного разрешения; оригинал сохранён в backup", project, normalized)
			}
			return nil
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return nil
		}
		if normalized == ".hermes/hermes-agent" || strings.HasPrefix(normalized, ".hermes/hermes-agent/") || normalized == ".hermes/.venv" || strings.HasPrefix(normalized, ".hermes/.venv/") {
			return nil
		}
		spool, err := os.CreateTemp(homeStage, ".entry-")
		if err != nil {
			return err
		}
		defer os.Remove(spool.Name())
		defer spool.Close()
		size, err := io.Copy(spool, reader)
		if err != nil {
			return err
		}
		profile := path.Join(".adc-legacy", project, "home", normalized)
		ok, err := writeLegacyReader(homeStage, profile, io.NewSectionReader(spool, 0, size))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("несовпадающие копии профиля %s", profile)
		}
		key := project + ":" + normalized
		if copied[key] {
			return nil
		}
		copied[key] = true
		if h.Mode&0100 != 0 && !shared.Contains(authFiles, normalized) {
			if err = os.Chmod(filepath.Join(homeStage, filepath.FromSlash(profile)), 0700); err != nil {
				return err
			}
		}
		if legacyManagedFile(normalized) {
			// Retain personal regular skill files in their source profile. The
			// delivery engine builds the active skill tree in the new home.
			report.Files++
			return nil
		}
		if project != account && legacyTextHistory(normalized) {
			if err = mergeLegacyHistory(homeStage, normalized, spool, size); err != nil {
				return err
			}
		} else if project == account || legacyHistory(normalized) {
			ok, err = writeLegacyReader(homeStage, normalized, io.NewSectionReader(spool, 0, size))
			if err != nil {
				return err
			}
			if !ok {
				report.Conflicts = append(report.Conflicts, project+":"+normalized)
			} else if h.Mode&0100 != 0 && !shared.Contains(authFiles, normalized) {
				if err = os.Chmod(filepath.Join(homeStage, filepath.FromSlash(normalized)), 0700); err != nil {
					return err
				}
			}
		}
		report.Files++
		return nil
	}
	// Import the primary first, so a colliding secondary history never wins.
	ordered := append([]legacy.Container(nil), b.Plan.Containers...)
	for i, c := range ordered {
		if c.Project == account {
			ordered[0], ordered[i] = ordered[i], ordered[0]
			break
		}
	}
	for _, c := range ordered {
		oldHome := "/home/node"
		for _, m := range c.Mounts {
			if _, prefix := legacyHomeMount(m.Destination); prefix != "" {
				oldHome = prefix
				break
			}
		}
		// docker export omits mounted data but captures agent stores that were
		// accidentally left in the writable layer (e.g. OpenCode).
		archive := b.Archives["container-"+c.ID]
		err = readLegacyTar(filepath.Join(dir, archive.File), archive.SHA256, func(h *tar.Header, name string, r io.Reader) error {
			prefix := strings.TrimPrefix(oldHome, "/") + "/"
			if !strings.HasPrefix(name, prefix) {
				return nil
			}
			rel := strings.TrimPrefix(name, prefix)
			for _, allowed := range []string{".claude", ".codex", ".hermes", ".dsh", ".claude.json", ".config/opencode", ".local/share/opencode", ".history", "obsidian", "obsidian-vault"} {
				if rel == allowed || strings.HasPrefix(rel, allowed+"/") {
					return copyEntry(c.Project, rel, h, r)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, m := range c.Mounts {
			if m.Resource == "" {
				continue
			}
			prefix, _ := legacyHomeMount(m.Destination)
			toolMount := m.Destination == "/opt/ai-tools"
			if prefix == "" && !toolMount {
				continue
			}
			if toolMount && sharedTools[m.Resource] {
				continue
			}
			archive = b.Archives[m.Resource]
			err = readLegacyTar(filepath.Join(dir, archive.File), archive.SHA256, func(h *tar.Header, name string, r io.Reader) error {
				if !toolMount {
					return copyEntry(c.Project, path.Join(prefix, name), h, r)
				}
				if name != "share/mcp/servers.json" && name != "share/mcp/secrets.env" && name != "share/hermes/auth.json" {
					return nil
				}
				if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
					return errors.New("общий MCP/Hermes store должен содержать обычные файлы")
				}
				if h.Size > 8<<20 {
					return errors.New("слишком большой файл общего MCP/Hermes store")
				}
				data, err := io.ReadAll(r)
				if err != nil {
					return err
				}
				ok, err := writeLegacyFile(toolStage, name, data)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("конфликт общих MCP/Hermes stores; требуется отдельный выбор")
				}
				return nil
			})
			if err != nil {
				return err
			}
			if toolMount {
				sharedTools[m.Resource] = true
			}
		}
	}
	for project := range hermesLinks {
		data, err := os.ReadFile(filepath.Join(toolStage, "share", "hermes", "auth.json"))
		if err != nil {
			return fmt.Errorf("Hermes auth для %s ссылается на отсутствующий общий store: %w", project, err)
		}
		if _, err = writeLegacyFile(homeStage, path.Join(".adc-legacy", project, "home", ".hermes/auth.json"), data); err != nil {
			return err
		}
		if project == account {
			ok, err := writeLegacyFile(homeStage, ".hermes/auth.json", data)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("конфликт выбранной авторизации Hermes")
			}
		}
	}
	if report.Files == 0 {
		return errors.New("в backup не найдены профили агентов; импорт не выполнен")
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if _, err = writeLegacyFile(homeStage, ".adc-legacy/report.json", append(data, '\n')); err != nil {
		return err
	}
	// Everything is validated and staged before any destination becomes live.
	// Roll back only entries created here if publication fails.
	published := []string{}
	success := false
	defer func() {
		if !success {
			for _, file := range published {
				_ = os.RemoveAll(file)
			}
		}
	}()
	for _, roots := range [][2]string{{homeStage, home}, {toolStage, tools}} {
		if err = filepath.WalkDir(roots[0], func(file string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Chown(file, uid, gid)
		}); err != nil {
			return err
		}
		entries, err := os.ReadDir(roots[0])
		if err != nil {
			return err
		}
		for _, entry := range entries {
			target := filepath.Join(roots[1], entry.Name())
			if _, err = os.Lstat(target); !os.IsNotExist(err) {
				return errors.New("назначение изменилось во время импорта")
			}
			if err = os.Rename(filepath.Join(roots[0], entry.Name()), target); err != nil {
				return err
			}
			published = append(published, target)
		}
		if err = os.Chown(roots[1], uid, gid); err != nil {
			return err
		}
		if err = os.Chmod(roots[1], 0700); err != nil {
			return err
		}
	}
	report.Complete = true
	data, err = json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	reportFile := filepath.Join(home, ".adc-legacy", "report.json")
	if err = shared.AtomicWrite(reportFile, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err = os.Chown(reportFile, uid, gid); err != nil {
		return err
	}
	success = true
	fmt.Printf("Импортировано файлов профилей: %d; конфликтов истории: %d; ссылок оставлено в backup: %d. Отчёт: ~/.adc-legacy/report.json\n", report.Files, len(report.Conflicts), len(report.Links))
	return nil
}

func VerifyLegacyImport(home, fingerprint, account string) error {
	var report legacyImportReport
	if err := legacy.ReadJSON(filepath.Join(home, ".adc-legacy", "report.json"), &report); err != nil {
		return err
	}
	if report.Version != legacy.Version || !report.Complete || report.Fingerprint != fingerprint || report.Account != account {
		return errors.New("копирование не завершено или назначение не соответствует migration journal")
	}
	return nil
}

func LegacyImportEnvironment(dir, account string) error {
	uid, gid := os.Getuid(), os.Getgid()
	for _, pair := range []struct {
		key   string
		value *int
	}{{"ADC_UID", &uid}, {"ADC_GID", &gid}} {
		if raw := os.Getenv(pair.key); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				return errors.New("некорректный UID/GID")
			}
			*pair.value = parsed
		}
	}
	return ImportLegacy(dir, account, "/home/node", "/opt/ai-tools", uid, gid)
}
