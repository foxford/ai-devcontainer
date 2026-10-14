package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
	"strings"
)

func (a *App) WriteMap(c shared.Config) error {
	if e := os.MkdirAll("/workspace/projects", 0755); e != nil {
		return e
	}
	for _, p := range c.Projects {
		alias := filepath.Join("/workspace/projects", p.ID)
		if alias == p.RuntimePath {
			continue
		}
		if link, e := os.Readlink(alias); e == nil {
			if link == p.RuntimePath {
				continue
			}
			if e = os.Remove(alias); e != nil {
				return e
			}
		} else if !os.IsNotExist(e) {
			return fmt.Errorf("workspace project alias %s: %w", alias, e)
		}
		if e := os.Symlink(p.RuntimePath, alias); e != nil {
			return e
		}
	}
	var b strings.Builder
	b.WriteString(shared.Marker + "# Проекты и знания разработчика\n\nВсе перечисленные проекты доступны в общей среде. Выбирай необходимые источники по задаче; правила каждого репозитория действуют внутри него. Доступ для чтения не разрешает изменения.\n\n")
	for _, p := range c.Projects {
		capabilities, e := a.shared().ProjectCapabilities(p)
		if e != nil {
			return e
		}
		mode := "read/write"
		if p.ReadOnly {
			mode = "read-only"
		}
		fmt.Fprintf(&b, "## %s\n\nПуть: `%s` (%s). %s\n\nКонтракт: `%s/AGENTS.md`. Возможности: %s. Связи: %s.\n\n", p.ID, p.RuntimePath, mode, p.Description, p.RuntimePath, strings.Join(capabilities, ", "), strings.Join(p.Related, ", "))
		for _, k := range p.Knowledge {
			fmt.Fprintf(&b, "- `%s/%s` — источник: %s; зарегистрирован: %s. Перед использованием проверь текущую версию.\n", p.RuntimePath, k.Path, k.Origin, k.Updated)
		}
	}
	b.WriteString("\n## Общие источники\n\n")
	for _, k := range c.Knowledge {
		fmt.Fprintf(&b, "- `%s` — %s; зарегистрирован: %s.\n", k.Path, k.Origin, k.Updated)
	}
	if e := shared.AtomicWrite("/workspace/AGENTS.workspace.md", []byte(b.String()), 0644); e != nil {
		return e
	}
	core, e := a.shared().InstructionText(nil)
	if e != nil {
		return e
	}
	if e = shared.AtomicWrite("/workspace/AGENTS.md", append([]byte(shared.Marker+"Прочитай [карту всех проектов](AGENTS.workspace.md), затем AGENTS.md затронутых репозиториев.\n\n"), core...), 0644); e != nil {
		return e
	}
	if e = shared.AtomicWrite("/workspace/CLAUDE.md", []byte(shared.Marker+"@AGENTS.md\n@AGENTS.workspace.md\n"), 0644); e != nil {
		return e
	}
	// Every starting directory gets a managed global entry pointing at the map;
	// project rules are read locally. No repo can replace another repo's home.
	for _, pair := range [][2]string{{".codex/AGENTS.md", ""}, {".claude/CLAUDE.md", "@/workspace/AGENTS.md\n@/workspace/AGENTS.workspace.md\n"}, {".hermes/AGENTS.md", ""}} {
		home, _ := os.UserHomeDir()
		body := pair[1]
		if body == "" {
			body = "Прочитай /workspace/AGENTS.md и /workspace/AGENTS.workspace.md. Для изменений в проекте прочитай его AGENTS.md и AGENTS.platform.md. Проектные скиллы: его AGENTS.skills.md; выбирай форки только в их области. MCP с префиксом project/checkоut относится к указанному checkout.\n"
		}
		if e = shared.ManagedBlock(filepath.Join(home, pair[0]), "<!-- adc:workspace:start -->", "<!-- adc:workspace:end -->", body); e != nil {
			return e
		}
	}
	return nil
}

type projectSyncError struct{ error }

func (e *projectSyncError) Unwrap() error { return e.error }

func (a *App) Sync(args []string) error {
	var indexed []shared.Project
	err := a.shared().Locked(func() error {
		if err := a.shared().RefreshDelivery(); err != nil {
			return err
		}
		c, e := a.shared().Load()
		if e != nil {
			return e
		}
		if e = a.WriteMap(c); e != nil {
			return e
		}
		selected := []shared.Project{}
		if len(args) > 0 && args[0] == "--all" {
			selected = allSyncCheckouts(c.Projects)
		} else {
			key := ""
			if len(args) > 0 {
				key = args[0]
			} else {
				wd, _ := os.Getwd()
				key = wd
			}
			p, e := a.shared().Project(c, key)
			if e != nil {
				root, re := shared.Output(key, "git", "rev-parse", "--show-toplevel")
				if re != nil {
					return e
				}
				p, e = a.FindCheckout(c, root)
				if e != nil {
					return e
				}
				p.RuntimePath = root
			}
			selected = append(selected, p)
		}
		var failures []error
		for _, p := range selected {
			shared.StartupProgress("Доставка контекста: " + p.ID)
			if p.ReadOnly {
				fmt.Println(p.ID, "read-only: карта доступна, раздача пропущена")
				continue
			}
			if e = a.SyncProject(p); e != nil {
				failures = append(failures, fmt.Errorf("%s: %w", p.ID, e))
				fmt.Fprintln(os.Stderr, p.ID, "FAILED:", e)
			} else {
				indexed = append(indexed, p)
				fmt.Println(p.ID, "OK")
			}
		}
		if len(failures) > 0 {
			return &projectSyncError{errors.Join(failures...)}
		}
		return nil
	})
	// Shared agent configs are complete. Indexing runs in separate processes so
	// startup, worktree setup and agent launch never wait for a checkout graph.
	for _, p := range indexed {
		a.syncGraph(p)
	}
	return err
}
func (a *App) FindCheckout(c shared.Config, root string) (shared.Project, error) {
	common, e := shared.Output(root, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if e != nil {
		return shared.Project{}, e
	}
	for _, p := range c.Projects {
		other, e := shared.Output(p.RuntimePath, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
		if e == nil && common == other {
			return p, nil
		}
	}
	return shared.Project{}, errors.New("Git common dir не принадлежит подключённому проекту")
}
func (a *App) SyncProject(p shared.Project) error {
	root := p.RuntimePath
	if err := shared.MigrateProjectState(root); err != nil {
		return err
	}
	capabilities, e := a.shared().ProjectCapabilities(p)
	if e != nil {
		return e
	}
	p.Capabilities = capabilities
	// A newly selected browser capability also works in an already running workspace.
	if os.Getenv("ADC_INSTALL_AI_TOOLS") != "0" {
		shared.StartupProgress("Проверка браузерных инструментов: " + p.ID)
		if e = a.InstallMCPBrowsers(shared.Config{Projects: []shared.Project{p}}); e != nil {
			return e
		}
	}
	namespace := p.ID + "-" + shared.CheckoutID(root)
	for _, deliver := range []func() error{
		func() error { return a.DeliverSkills(root, capabilities) },
		func() error { return a.DeliverDocuments(p) },
		func() error { return a.DeliverMCP(p) },
	} {
		if err := deliver(); err != nil {
			return err
		}
	}
	core, e := a.shared().InstructionText(capabilities)
	if e != nil {
		return e
	}
	var b strings.Builder
	b.WriteString(shared.Marker)
	b.Write(core)
	fmt.Fprintf(&b, "\n## Этот checkout\n\nProject: `%s`. Path: `%s`. MCP namespace: `%s-`.\n\nПрочитай `/workspace/AGENTS.workspace.md`, чтобы видеть остальные проекты и источники знаний. Проектные скиллы перечислены в AGENTS.skills.md; читай соответствующий SKILL.md из .claude/skills.\n", p.ID, root, namespace)
	target := filepath.Join(root, "AGENTS.platform.md")
	old, e := os.ReadFile(target)
	if e == nil && !strings.Contains(string(old), "ai-devcontainer:generated") {
		return errors.New("AGENTS.platform.md принадлежит проекту; перенесите проектные правила в AGENTS.md")
	}
	if e = shared.AtomicWrite(target, []byte(b.String()), 0644); e != nil {
		return e
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		body := "Прочитай [контракт ADC](AGENTS.platform.md) и [все проекты](/workspace/AGENTS.workspace.md). Правила этого файла и вложенных AGENTS.md сохраняют область действия."
		if name == "CLAUDE.md" {
			body = "@AGENTS.platform.md\n@/workspace/AGENTS.workspace.md\n@AGENTS.md"
		}
		if e = shared.ManagedBlock(filepath.Join(root, name), "<!-- adc:context:start -->", "<!-- adc:context:end -->", body); e != nil {
			return e
		}
	}
	if e = wireOrca(root); e != nil {
		return e
	}
	if e = a.PublishProjectSkills(p, namespace); e != nil {
		return e
	}
	if shared.Contains(capabilities, "graphify") && os.Getenv("ADC_INSTALL_AI_TOOLS") != "0" {
		if e = a.installGraphify(); e != nil {
			return e
		}
	}
	return a.ignoreProject(p)
}

func (a *App) ignoreProject(p shared.Project) error {
	root := p.RuntimePath
	capabilities, e := a.shared().ProjectCapabilities(p)
	if e != nil {
		return e
	}
	ignore := filepath.Join(root, ".gitignore")
	patterns := []string{"/.adc/", "/.ai-devcontainer/", "/AGENTS.platform.md", "/AGENTS.skills.md", "/.claude/skills/", "/.dsh/skills/", "/.mcp.json", "/.agents/mcp.local.json", "/.agents/mcp.secrets.env"}
	if shared.Contains(capabilities, "graphify") {
		patterns = append(patterns, "/graphify-out/", "/.graphify-cache/", "/.graphify_cache/")
	}
	documents, e := a.documents(capabilities)
	if e != nil {
		return e
	}
	for _, doc := range documents {
		pattern := "/" + filepath.ToSlash(doc.Target)
		if !shared.Contains(patterns, pattern) && ownedDocument(filepath.Join(root, doc.Target), a.shared().DeliveryPath(doc.Source)) {
			patterns = append(patterns, pattern)
		}
	}
	if e = shared.ManagedBlock(ignore, "# adc:generated:start", "# adc:generated:end", strings.Join(patterns, "\n")); e != nil {
		return e
	}
	return nil
}

func (a *App) syncGraph(p shared.Project) {
	if p.ReadOnly || os.Getenv("ADC_INSTALL_AI_TOOLS") == "0" {
		return
	}
	caps, err := a.shared().ProjectCapabilities(p)
	if err != nil || !shared.Contains(caps, "graphify") {
		return
	}
	if err := a.startGraphRefresh(p.RuntimePath); err != nil {
		fmt.Fprintln(os.Stderr, "Graphify: не удалось запустить фоновую индексацию:", err, "; повтор: adc tools graphify index")
	} else {
		fmt.Fprintln(os.Stderr, "Graphify: фоновая проверка индекса", p.RuntimePath, "; лог:", graphLogPath(p.RuntimePath))
	}
}
func (a *App) PublishProjectSkills(p shared.Project, namespace string) error {
	home, _ := os.UserHomeDir()
	dest := filepath.Join(home, ".codex", "skills")
	if e := os.MkdirAll(dest, 0755); e != nil {
		return e
	}
	// Common skills are the platform layer, independent of the sync order.
	catalog, e := a.shared().DeliveryCatalog()
	if e != nil {
		return e
	}
	entries, e := os.ReadDir(a.shared().DeliveryPath(catalog.Base.Skills))
	if e != nil {
		return e
	}
	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		target := filepath.Join(dest, d.Name())
		if info, e := os.Lstat(target); e == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			link, _ := os.Readlink(target)
			link = filepath.Clean(link)
			platformLink := strings.HasPrefix(link, filepath.Clean(a.Platform)+string(os.PathSeparator))
			deliveryLink := filepath.IsAbs(a.State) && strings.HasPrefix(link, filepath.Join(a.State, "delivery")+string(os.PathSeparator))
			if !platformLink && !deliveryLink {
				continue
			}
			if e = os.Remove(target); e != nil {
				return e
			}
		}
		if e = os.Symlink(filepath.Join(a.shared().DeliveryPath(catalog.Base.Skills), d.Name()), target); e != nil {
			return e
		}
	}
	// Scoped adapters have unique frontmatter names and direct the agent to the
	// real composed tree. No imported skill instruction is rewritten.
	forks, e := os.ReadDir(filepath.Join(p.RuntimePath, ".agents", "skills"))
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	for _, dest := range []string{dest, filepath.Join(home, ".hermes", "skills"), filepath.Join(home, ".config", "opencode", "skills")} {
		if e = os.MkdirAll(dest, 0755); e != nil {
			return e
		}
		present := map[string]bool{}
		names := map[string]bool{}
		for _, d := range forks {
			if d.IsDir() {
				names[d.Name()] = true
			}
		}
		for _, capability := range p.Capabilities {
			for _, source := range catalog.Capabilities[capability].Skills {
				names[filepath.Base(source)] = true
			}
		}
		for skill := range names {
			name := namespace + "--" + skill
			present[name] = true
			path := filepath.Join(p.RuntimePath, ".claude", "skills", skill, "SKILL.md")
			body := fmt.Sprintf("---\nname: %s\ndescription: 'Project %s only. Use the project variant of %s when working in %s.'\n---\n\nRead `%s` and use its sibling references/scripts. Apply only inside `%s`.\n", name, p.ID, skill, strings.ReplaceAll(p.RuntimePath, "'", "''"), path, p.RuntimePath)
			if e = shared.AtomicWrite(filepath.Join(dest, name, "SKILL.md"), []byte(body), 0644); e != nil {
				return e
			}
		}
		existing, e := os.ReadDir(dest)
		if e != nil {
			return e
		}
		for _, d := range existing {
			if strings.HasPrefix(d.Name(), namespace+"--") && !present[d.Name()] {
				if e = os.RemoveAll(filepath.Join(dest, d.Name())); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func (a *App) Init() error {
	if e := os.Remove("/tmp/adc-ready"); e != nil && !os.IsNotExist(e) {
		return e
	}
	c, e := a.shared().Load()
	if e != nil {
		return e
	}
	for _, dir := range []string{".codex", ".claude", ".hermes", ".dsh"} {
		home, _ := os.UserHomeDir()
		if e = os.MkdirAll(filepath.Join(home, dir), 0700); e != nil {
			return e
		}
	}
	if os.Getenv("ADC_INSTALL_AI_TOOLS") != "0" {
		if e = a.InstallTools(); e != nil {
			return e
		}
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	if e = initializeShells(home, "/opt/adc-shell"); e != nil {
		return e
	}
	if os.Getenv("ADC_INSTALL_AI_TOOLS") != "0" {
		shared.StartupProgress("Установка браузеров выбранных MCP")
		if e = a.InstallMCPBrowsers(c); e != nil {
			return e
		}
	}
	syncErr := a.Sync([]string{"--all"})
	var projects *projectSyncError
	if syncErr != nil && !errors.As(syncErr, &projects) {
		return syncErr
	}
	// Public CLI only; errors stay visible. No direct writes into Orca's database.
	for _, p := range c.Projects {
		shared.StartupProgress("Регистрация в Orca: " + p.ID)
		if e = shared.Run("", "orca", "repo", "add", "--path", p.RuntimePath, "--json"); e != nil {
			return e
		}
	}
	if syncErr != nil {
		return syncErr
	}
	return shared.AtomicWrite("/tmp/adc-ready", []byte("ready\n"), 0644)
}
func (a *App) Worktree(args []string) error {
	if len(args) != 3 || args[0] != "create" || !shared.ValidID.MatchString(args[2]) {
		return errors.New("adc worktree create <project> <name>")
	}

	c, e := a.shared().Load()
	if e != nil {
		return e
	}
	p, e := a.shared().Project(c, args[1])
	if e != nil {
		return e
	}
	if p.ReadOnly {
		return errors.New("проект read-only")
	}
	result, e := shared.Output("", "orca", "worktree", "create", "--repo", "path:"+p.RuntimePath, "--name", args[2], "--setup", "run", "--json")
	if e != nil {
		return e
	}
	var response struct {
		Result struct{ Worktree struct{ Path string } }
	}
	if e = json.Unmarshal([]byte(result), &response); e != nil {
		return e
	}
	path := response.Result.Worktree.Path
	if path == "" {
		return errors.New("Orca не вернула путь нового worktree")
	}
	if e = a.Sync([]string{path}); e != nil {
		return e
	}
	fmt.Println("Worktree готов:", path)
	return nil
}
