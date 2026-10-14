package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type cleanupEntry struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Digest string `json:"digest,omitempty"`
}

type cleanupPlan struct {
	Version      int            `json:"version"`
	Devcontainer bool           `json:"devcontainer"`
	Root         string         `json:"root"`
	Entries      []cleanupEntry `json:"entries"`
	Kept         []string       `json:"kept"`
	Status       string         `json:"status,omitempty"`
}

// Never traverse a symlinked parent, even when the leaf looks generated.
func cleanupInfo(root, rel string) (os.FileInfo, error) {
	name := filepath.Join(root, filepath.FromSlash(rel))
	for parent := filepath.Dir(name); parent != root; parent = filepath.Dir(parent) {
		if filepath.Dir(parent) == parent {
			return nil, errors.New("путь очистки выходит за проект")
		}
		info, err := os.Lstat(parent)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("родитель %s не является обычным каталогом", parent)
		}
	}
	return os.Lstat(name)
}

func cleanupHeader(root, rel string) string {
	info, err := cleanupInfo(root, rel)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, 1024))
	first, _, _ := bytes.Cut(data, []byte("\n"))
	return string(first)
}

func cleanupLink(root, rel string, prefixes []string) bool {
	link, err := os.Readlink(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	if !shared.PortableAbs(link) {
		link = filepath.Join(root, filepath.Dir(filepath.FromSlash(rel)), link)
	}
	link = strings.ReplaceAll(filepath.Clean(link), "\\", "/")
	for _, prefix := range prefixes {
		prefix = strings.TrimRight(strings.ReplaceAll(filepath.Clean(prefix), "\\", "/"), "/")
		if link == prefix || strings.HasPrefix(link, prefix+"/") {
			return true
		}
	}
	return false
}

func cleanupMCPOwned(root string) bool {
	for _, rel := range []string{".adc/mcp-state.json", ".adc/mcp-state", ".ai-devcontainer/mcp-state.json", ".ai-devcontainer/mcp-state", ".claude/.ai-devcontainer-mcp"} {
		info, err := cleanupInfo(root, rel)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
			continue
		}
		if strings.HasSuffix(rel, ".json") {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			var state struct {
				Version     int  `json:"version"`
				ClaudeOwned bool `json:"claudeOwned"`
			}
			return err == nil && json.Unmarshal(data, &state) == nil && state.Version == 1 && state.ClaudeOwned
		}
		return true
	}
	return false
}

func (a *App) planLegacyCleanup(root string, devcontainer bool) (cleanupPlan, error) {
	p := cleanupPlan{Version: 1, Root: root, Devcontainer: devcontainer, Entries: []cleanupEntry{}, Kept: []string{}}
	selected := map[string]string{}
	add := func(rel, reason string, owned bool) {
		if _, err := cleanupInfo(root, rel); os.IsNotExist(err) {
			return
		} else if err != nil || !owned {
			p.Kept = append(p.Kept, rel)
			return
		}
		selected[rel] = reason
	}
	// Reset only obsolete state; preserve the new MCP ownership, fork bases and backups.
	info, err := cleanupInfo(root, ".ai-devcontainer")
	add(".ai-devcontainer", "служебное состояние ADC", err == nil && info.IsDir())
	info, err = cleanupInfo(root, ".claude/.ai-devcontainer-mcp")
	add(".claude/.ai-devcontainer-mcp", "старое состояние MCP", err == nil && info.Mode().IsRegular())
	add(".mcp.json", "доставленный MCP-конфиг", cleanupMCPOwned(root))
	if devcontainer {
		add(".devcontainer", "проектный devcontainer: явно выбран --devcontainer", true)
	}
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		return p, err
	}
	documents := map[string]bool{"AGENTS.platform.md": true, "AGENTS.skills.md": true}
	for _, doc := range catalog.Base.Documents {
		documents[doc.Target] = true
	}
	for _, capability := range catalog.Capabilities {
		for _, doc := range capability.Documents {
			documents[doc.Target] = true
		}
	}
	platformRoots := []string{a.Platform, "/opt/ai-devcontainer", "/opt/dev-platform"}
	var docRoots, skillRoots []string
	for _, platform := range platformRoots {
		docRoots = append(docRoots, filepath.Join(platform, "docs"), filepath.Join(platform, "delivery"))
		skillRoots = append(skillRoots, filepath.Join(platform, "skills"), filepath.Join(platform, "delivery"))
	}
	for rel := range documents {
		header := cleanupHeader(root, rel)
		owned := strings.Contains(header, "ai-devcontainer:generated") || strings.Contains(header, "Генерируется tooling/wire-agent-skills.sh.") || cleanupLink(root, rel, docRoots)
		add(rel, "доставленный документ", owned)
	}
	// A marked skill tree is disposable; project forks live in .agents/skills.
	for _, dir := range []string{".claude/skills", ".dsh/skills", ".hermes/skills"} {
		info, err := cleanupInfo(root, dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			p.Kept = append(p.Kept, dir)
			continue
		}
		prefixes := append(append([]string{}, skillRoots...), filepath.Join(root, ".agents", "skills"), filepath.Join(root, ".claude", "skills"))
		if info.Mode()&os.ModeSymlink != 0 {
			add(dir, "ссылка на доставленное дерево скиллов", cleanupLink(root, dir, prefixes))
			continue
		}
		if !info.IsDir() {
			p.Kept = append(p.Kept, dir)
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			return p, err
		}
		for _, entry := range entries {
			rel := dir + "/" + entry.Name()
			marker, err := cleanupInfo(root, rel+"/.ai-devcontainer-wired")
			owned := entry.IsDir() && err == nil && marker.Mode().IsRegular()
			add(rel, "доставленный скилл или ссылка", owned || cleanupLink(root, rel, prefixes))
		}
	}
	for rel, reason := range selected {
		nested := false
		for parent := filepath.Dir(filepath.FromSlash(rel)); parent != "."; parent = filepath.Dir(parent) {
			if _, ok := selected[filepath.ToSlash(parent)]; ok {
				nested = true
				break
			}
		}
		if !nested {
			p.Entries = append(p.Entries, cleanupEntry{Path: rel, Reason: reason})
		}
	}
	sort.Slice(p.Entries, func(i, j int) bool { return p.Entries[i].Path < p.Entries[j].Path })
	sort.Strings(p.Kept)
	return p, nil
}

func (a *App) cleanupRoot(target string) (string, error) {
	root, err := shared.Abs(target)
	if err != nil {
		config, loadErr := a.shared().Load()
		if loadErr != nil {
			return "", err
		}
		project, projectErr := a.shared().Project(config, target)
		if projectErr != nil {
			return "", projectErr
		}
		if project.ReadOnly {
			return "", errors.New("очистка read-only проекта запрещена")
		}
		root, err = shared.Abs(project.Path)
	}
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(filepath.Join(root, ".git"))
	if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		return "", errors.New("для очистки укажите корень Git-репозитория")
	}
	if platform, err := shared.Abs(a.Platform); err == nil && root == platform {
		return "", errors.New("очистка самого репозитория платформы запрещена")
	}
	if config, err := a.shared().Load(); err == nil {
		for _, project := range config.Projects {
			if path, err := shared.Abs(project.Path); err == nil && root == path && project.ReadOnly {
				return "", errors.New("очистка read-only проекта запрещена")
			}
		}
	}
	return root, nil
}

func (a *App) LegacyCleanup(target string, apply, devcontainer bool) error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	root, err := a.cleanupRoot(target)
	if err != nil {
		return err
	}
	plan, err := a.planLegacyCleanup(root, devcontainer)
	if err != nil {
		return err
	}
	for _, entry := range plan.Entries {
		fmt.Printf("Убрать: %s (%s)\n", entry.Path, entry.Reason)
	}
	for _, rel := range plan.Kept {
		fmt.Printf("Оставить: %s (нет подтверждения доставки ADC или родитель — ссылка)\n", rel)
	}
	if !apply {
		fmt.Println("Предпросмотр: файлы не изменены. Для очистки повторите с --apply; --devcontainer включает проектный devcontainer.")
		return nil
	}
	state, err := filepath.Abs(a.State)
	if err != nil {
		return err
	}
	state, err = resolveLegacyInstallPath(state)
	if err != nil {
		return err
	}
	if overlappingLegacyPaths(root, state) {
		return errors.New("ADC_HOME для очистки должен находиться вне проекта")
	}
	return a.shared().Locked(func() error {
		backup, err := a.applyLegacyCleanup(plan, legacyPlatformStopped)
		if err != nil {
			return err
		}
		if backup != "" {
			fmt.Println("Очистка завершена. Проверенная копия:", backup)
			fmt.Println("Запустите общую среду командой adc workspace start: контекст проекта будет доставлен автоматически.")
		}
		return nil
	})
}

func (a *App) recheckLegacyCleanup(plan cleanupPlan) error {
	current, err := a.planLegacyCleanup(plan.Root, plan.Devcontainer)
	if err != nil {
		return err
	}
	if len(current.Entries) != len(plan.Entries) {
		return errors.New("состав доставки изменился после предпросмотра")
	}
	for i, entry := range current.Entries {
		if entry.Path != plan.Entries[i].Path {
			return errors.New("файл больше не принадлежит доставке ADC; очистка отменена")
		}
	}
	return nil
}

func (a *App) applyLegacyCleanup(plan cleanupPlan, stopped func(string) error) (string, error) {
	if len(plan.Entries) == 0 {
		return "", nil
	}
	if err := stopped(plan.Root); err != nil {
		return "", err
	}
	if err := a.recheckLegacyCleanup(plan); err != nil {
		return "", err
	}
	backupRoot, err := filepath.Abs(filepath.Join(a.State, "backups", "project-cleanup"))
	if err != nil {
		return "", err
	}
	backupRoot, err = resolveLegacyInstallPath(backupRoot)
	if err != nil {
		return "", err
	}
	projectRoot, err := shared.Abs(plan.Root)
	if err != nil {
		return "", err
	}
	if overlappingLegacyPaths(projectRoot, backupRoot) {
		return "", errors.New("резервная копия очистки должна находиться вне проекта")
	}
	if err = os.MkdirAll(backupRoot, 0700); err != nil {
		return "", err
	}
	backup, err := os.MkdirTemp(backupRoot, time.Now().UTC().Format("20060102T150405Z")+"-")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		return backup, fmt.Errorf("очистка остановлена; копия: %s: %w", backup, err)
	}
	for index := range plan.Entries {
		entry := &plan.Entries[index]
		if _, err := cleanupInfo(plan.Root, entry.Path); err != nil {
			return fail(err)
		}
		source := filepath.Join(plan.Root, filepath.FromSlash(entry.Path))
		dest := filepath.Join(backup, "checkout", filepath.FromSlash(entry.Path))
		entry.Digest, err = legacyPlatformDigest(source)
		if err != nil {
			return fail(err)
		}
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return fail(err)
		}
		if err = copyLegacyPlatform(source, dest); err != nil {
			return fail(err)
		}
		copyDigest, err := legacyPlatformDigest(dest)
		if err != nil || copyDigest != entry.Digest {
			return fail(errors.New("копия не прошла проверку SHA-256"))
		}
	}
	if err = stopped(plan.Root); err != nil {
		return fail(err)
	}
	if err = a.recheckLegacyCleanup(plan); err != nil {
		return fail(err)
	}
	// Verify every source before deleting the first entry.
	for _, entry := range plan.Entries {
		if _, err := cleanupInfo(plan.Root, entry.Path); err != nil {
			return fail(err)
		}
		digest, err := legacyPlatformDigest(filepath.Join(plan.Root, filepath.FromSlash(entry.Path)))
		if err != nil || digest != entry.Digest {
			return fail(errors.New("проект изменился во время копирования"))
		}
	}
	writeManifest := func(status string) error {
		plan.Status = status
		data, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		return shared.AtomicWrite(filepath.Join(backup, "cleanup.json"), append(data, '\n'), 0600)
	}
	if err = writeManifest("backed-up"); err != nil {
		return fail(err)
	}
	for _, entry := range plan.Entries {
		if _, err := cleanupInfo(plan.Root, entry.Path); err != nil {
			return fail(err)
		}
		source := filepath.Join(plan.Root, filepath.FromSlash(entry.Path))
		digest, err := legacyPlatformDigest(source)
		if err != nil || digest != entry.Digest {
			return fail(errors.New("файл изменился после проверки копии"))
		}
		if err = os.RemoveAll(source); err != nil {
			return fail(err)
		}
	}
	if err = writeManifest("complete"); err != nil {
		return fail(err)
	}
	return backup, nil
}
