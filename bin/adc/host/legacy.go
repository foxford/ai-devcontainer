package host

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/legacy"
)

type legacyInspection struct {
	ID    string `json:"Id"`
	Name  string
	State struct {
		Running bool
		Paused  bool
	}
	Config struct{ Labels map[string]string }
	Mounts []struct{ Type, Source, Destination, Name string }
}

type legacyPending struct {
	Before      shared.Config `json:"before"`
	After       shared.Config `json:"after"`
	Account     string        `json:"account"`
	Fingerprint string        `json:"fingerprint"`
}

func inspectLegacy(ids ...string) ([]legacyInspection, error) {
	cmd := exec.Command("docker", append([]string{"inspect"}, ids...)...)
	var diagnostic strings.Builder
	cmd.Stderr = &diagnostic
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker inspect: %w: %s", err, diagnostic.String())
	}
	var result []legacyInspection
	err = json.Unmarshal(data, &result)
	return result, err
}

func legacyPlan(inspections []legacyInspection) (legacy.Plan, error) {
	p := legacy.Plan{Version: legacy.Version}
	resources := map[string]string{}
	for _, inspected := range inspections {
		folder := inspected.Config.Labels["devcontainer.local_folder"]
		if folder == "" {
			return p, fmt.Errorf("%s не имеет devcontainer.local_folder", inspected.Name)
		}
		// VS Code on Windows labels WSL projects with UNC paths. Match the
		// checkout's final component, then use the actual Docker bind source.
		base := path.Base(strings.ReplaceAll(folder, "\\", "/"))
		c := legacy.Container{ID: inspected.ID, Name: strings.TrimPrefix(inspected.Name, "/"), Project: shared.RegexpID(strings.ToLower(base))}
		for _, m := range inspected.Mounts {
			mount := legacy.Mount{Type: m.Type, Source: m.Source, Destination: m.Destination}
			if m.Type == "volume" {
				mount.Source = m.Name
			}
			if m.Type != "bind" && m.Type != "volume" || m.Destination == "/tmp" || strings.HasPrefix(m.Destination, "/tmp/") || strings.HasPrefix(m.Destination, "/run/") || strings.HasPrefix(m.Destination, "/dev/") {
				mount.Skip = "временный маунт; пересоздаётся средой"
			} else {
				key := mount.Type + ":" + mount.Source
				if resources[key] == "" {
					id := fmt.Sprintf("resource-%03d", len(p.Resources)+1)
					resources[key] = id
					p.Resources = append(p.Resources, legacy.Resource{ID: id, Type: mount.Type, Source: mount.Source})
				}
				mount.Resource = resources[key]
			}
			c.Mounts = append(c.Mounts, mount)
			if m.Type == "bind" && (path.Base(m.Destination) == base && strings.HasPrefix(m.Destination, "/workspaces/") || path.Base(strings.ReplaceAll(m.Source, "\\", "/")) == base) {
				if c.ProjectPath != "" {
					return p, fmt.Errorf("неоднозначный checkout %s", base)
				}
				c.ProjectPath, c.RuntimePath = m.Source, m.Destination
			}
		}
		if c.ProjectPath == "" {
			return p, fmt.Errorf("не найден основной bind checkout для %s", inspected.Name)
		}
		p.Containers = append(p.Containers, c)
	}
	// Per-project mounts omit the parent registry and inactive project homes.
	// Snapshot the entire legacy state root as well; it stays archival and is
	// never overlaid onto a developer's new home.
	for _, r := range append([]legacy.Resource(nil), p.Resources...) {
		if r.Type != "bind" {
			continue
		}
		normalized := strings.ReplaceAll(r.Source, "\\", "/")
		parts := strings.Split(normalized, "/")
		for i, part := range parts {
			if part != ".ai-devcontainer-dev" {
				continue
			}
			root := strings.Join(parts[:i+1], "/")
			key := "bind:" + root
			if resources[key] == "" {
				id := fmt.Sprintf("resource-%03d", len(p.Resources)+1)
				resources[key] = id
				p.Resources = append(p.Resources, legacy.Resource{ID: id, Type: "bind", Source: root})
			}
			break
		}
	}
	// Docker may reorder mounts when a created container is first started.
	// Assign resource IDs from source identity, never inspect's incidental order.
	sort.Slice(p.Resources, func(i, j int) bool {
		return p.Resources[i].Type+":"+p.Resources[i].Source < p.Resources[j].Type+":"+p.Resources[j].Source
	})
	renamed := map[string]string{}
	for i := range p.Resources {
		renamed[p.Resources[i].ID] = fmt.Sprintf("resource-%03d", i+1)
		p.Resources[i].ID = fmt.Sprintf("resource-%03d", i+1)
	}
	sort.Slice(p.Containers, func(i, j int) bool { return p.Containers[i].Project < p.Containers[j].Project })
	for i := range p.Containers {
		for j := range p.Containers[i].Mounts {
			m := &p.Containers[i].Mounts[j]
			if m.Resource != "" {
				m.Resource = renamed[m.Resource]
			}
		}
		sort.Slice(p.Containers[i].Mounts, func(a, b int) bool {
			return p.Containers[i].Mounts[a].Destination < p.Containers[i].Mounts[b].Destination
		})
	}
	return p, p.Validate()
}

func (a *App) LegacyPlan(containers []string, output string) error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	if len(containers) == 0 {
		return errors.New("укажите --container для каждого старого devcontainer")
	}
	inspections, err := inspectLegacy(containers...)
	if err != nil {
		return err
	}
	p, err := legacyPlan(inspections)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if output == "" {
		fmt.Println(string(data))
		return nil
	}
	if err = shared.AtomicWrite(output, append(data, '\n'), 0600); err != nil {
		return err
	}
	fmt.Printf("План: %s. Проектов: %d; уникальных постоянных маунтов: %d. Исходные данные не изменены.\n", output, len(p.Containers), len(p.Resources))
	return nil
}

func stoppedLegacy(p legacy.Plan) error {
	ids := []string{}
	for _, c := range p.Containers {
		ids = append(ids, c.ID)
	}
	inspections, err := inspectLegacy(ids...)
	if err != nil {
		return err
	}
	for _, c := range inspections {
		if c.State.Running || c.State.Paused {
			return fmt.Errorf("остановите %s перед резервным копированием: docker stop %s", c.Name, strings.TrimPrefix(c.Name, "/"))
		}
	}
	current, err := legacyPlan(inspections)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, p) {
		return errors.New("маунты изменились после plan; создайте новый план")
	}
	active, err := shared.Output("", "docker", "ps", "-q")
	if err != nil {
		return err
	}
	if strings.TrimSpace(active) == "" {
		return nil
	}
	running, err := inspectLegacy(strings.Fields(active)...)
	if err != nil {
		return err
	}
	for _, c := range running {
		for _, m := range c.Mounts {
			source := m.Source
			if m.Type == "volume" {
				source = m.Name
			}
			for _, r := range p.Resources {
				if m.Type == r.Type && (source == r.Source || m.Type == "bind" && overlappingLegacyPaths(source, r.Source)) {
					return fmt.Errorf("ресурс backup используется работающим контейнером %s; остановите его", c.Name)
				}
			}
		}
	}
	return nil
}

func overlappingLegacyPaths(a, b string) bool {
	a, b = strings.TrimRight(strings.ReplaceAll(a, "\\", "/"), "/"), strings.TrimRight(strings.ReplaceAll(b, "\\", "/"), "/")
	if len(a) > 1 && a[1] == ':' || strings.HasPrefix(a, "//") {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func streamLegacyArchive(output, key string, args []string) (legacy.Archive, error) {
	archive := legacy.Archive{File: key + ".tar"}
	if strings.HasPrefix(key, "metadata-") {
		archive.File = key + ".json"
	}
	f, err := os.OpenFile(filepath.Join(output, archive.File), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return archive, err
	}
	defer f.Close()
	h := sha256.New()
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = io.MultiWriter(f, h), os.Stderr
	if err = cmd.Run(); err != nil {
		return archive, err
	}
	if err = f.Sync(); err != nil {
		return archive, err
	}
	info, err := f.Stat()
	if err != nil {
		return archive, err
	}
	archive.Bytes, archive.SHA256 = info.Size(), hex.EncodeToString(h.Sum(nil))
	return archive, f.Close()
}

func legacyResourceMount(r legacy.Resource) (string, error) {
	if r.Type == "bind" {
		return mount(r.Source, "/source", true)
	}
	if strings.ContainsAny(r.Source, ",\r\n") {
		return "", errors.New("некорректное имя тома")
	}
	return "type=volume,src=" + r.Source + ",dst=/source,readonly,volume-nocopy", nil
}

func (a *App) LegacyBackup(planFile, output string) error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	var p legacy.Plan
	if err := legacy.ReadJSON(planFile, &p); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if err := stoppedLegacy(p); err != nil {
		return err
	}
	out, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	parent, err := shared.Abs(filepath.Dir(out))
	if err != nil {
		return err
	}
	out = filepath.Join(parent, filepath.Base(out))
	for _, r := range p.Resources {
		source := r.Source
		if r.Type == "bind" {
			if resolved, err := shared.Abs(source); err == nil {
				source = resolved
			}
		}
		if r.Type == "bind" && overlappingLegacyPaths(out, source) {
			return errors.New("backup должен находиться вне всех исходных маунтов")
		}
	}
	if err = a.EnsureImage(); err != nil {
		return err
	}
	if err = os.Mkdir(out, 0700); err != nil {
		return fmt.Errorf("нужен новый каталог backup: %w", err)
	}
	b := legacy.Backup{Version: legacy.Version, Created: time.Now().UTC().Format(time.RFC3339), Plan: p, Archives: map[string]legacy.Archive{}}
	for _, r := range p.Resources {
		if r.Type == "volume" {
			if _, err := shared.Output("", "docker", "volume", "inspect", "--format", "{{.Name}}", r.Source); err != nil {
				return fmt.Errorf("исходный том исчез после plan: %w", err)
			}
		}
		m, err := legacyResourceMount(r)
		if err != nil {
			return err
		}
		fmt.Println("Backup:", r.ID, r.Type, r.Source)
		archive, err := streamLegacyArchive(out, r.ID, []string{"run", "--rm", "--network", "none", "--read-only", "--mount", m, "-e", "ADC_RUNTIME=1", "--entrypoint", "adc", "adc-runtime:local", "runtime-legacy-archive", "/source"})
		if err != nil {
			return fmt.Errorf("неполный backup %s: %w", out, err)
		}
		b.Archives[r.ID] = archive
	}
	for _, c := range p.Containers {
		key := "container-" + c.ID
		archive, err := streamLegacyArchive(out, key, []string{"export", c.ID})
		if err != nil {
			return fmt.Errorf("неполный backup %s: %w", out, err)
		}
		b.Archives[key] = archive
		// Keep environment, image, labels, ports and the rest of inspect in a
		// private file. They can contain secrets and never go to stdout.
		metadata, err := streamLegacyArchive(out, "metadata-"+c.ID, []string{"inspect", c.ID})
		if err != nil {
			return fmt.Errorf("неполный backup %s: %w", out, err)
		}
		b.Archives["metadata-"+c.ID] = metadata
	}
	// Recheck writers after copying; an interrupted or inconsistent snapshot has
	// no completion manifest and the importer refuses it.
	if err := stoppedLegacy(p); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if err = shared.AtomicWrite(filepath.Join(out, "backup.json"), append(data, '\n'), 0600); err != nil {
		return err
	}
	if _, err = legacy.Verify(out); err != nil {
		return err
	}
	fmt.Println("Backup завершён и SHA-256 проверены:", out)
	return nil
}

func (a *App) LegacyVerify(dir string) error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	b, err := legacy.Verify(dir)
	if err == nil {
		fmt.Printf("Backup проверен: %d архивов, %d проектов.\n", len(b.Archives), len(b.Plan.Containers))
	}
	return err
}

func (a *App) LegacyImport(dir, account, workspace string, mappings []string) error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	if account == "" {
		return errors.New("выберите --account <project>: его аккаунты и настройки станут общими")
	}
	backup, err := shared.Abs(dir)
	if err != nil {
		return err
	}
	b, err := legacy.Verify(backup)
	if err != nil {
		return err
	}
	paths := map[string]string{}
	for _, mapping := range mappings {
		id, value, ok := strings.Cut(mapping, "=")
		if !ok || value == "" || paths[id] != "" {
			return errors.New("--project требует уникальные project-id=/host/path")
		}
		paths[id] = value
	}
	return a.shared().Locked(func() error {
		if _, err := os.Stat(filepath.Join(a.State, "migration-pending.json")); !os.IsNotExist(err) {
			return errors.New("предыдущий импорт не завершён: adc migrate legacy finish")
		}
		config, err := a.shared().Load()
		if err != nil {
			return err
		}
		if len(config.Projects) != 0 {
			return errors.New("импорт выполняется только в новый workspace без проектов")
		}
		pending := legacyPending{Before: config, Account: account, Fingerprint: legacy.Fingerprint(b)}
		if workspace != "" {
			if !shared.ValidID.MatchString(workspace) {
				return errors.New("некорректное имя нового workspace")
			}
			config.Name = workspace
		}
		selected := false
		for _, old := range b.Plan.Containers {
			selected = selected || old.Project == account
			projectPath := old.ProjectPath
			if value, ok := paths[old.Project]; ok {
				projectPath = value
				delete(paths, old.Project)
			}
			projectPath, err = shared.Abs(projectPath)
			if err != nil {
				return fmt.Errorf("%s: укажите --project %s=/host/path: %w", old.Project, old.Project, err)
			}
			root, err := shared.Output(projectPath, "git", "rev-parse", "--show-toplevel")
			if err != nil {
				return err
			}
			root, err = shared.Abs(root)
			if err != nil {
				return err
			}
			if root != projectPath {
				return errors.New("project path должен указывать на корень Git checkout")
			}
			for _, existing := range config.Projects {
				if overlappingLegacyPaths(existing.Path, projectPath) {
					return errors.New("повторные или вложенные checkout требуют отдельной регистрации")
				}
			}
			config.Projects = append(config.Projects, shared.Project{ID: old.Project, Path: projectPath, RuntimePath: shared.RuntimePath(projectPath, old.Project)})
		}
		if !selected || len(paths) != 0 {
			return errors.New("неизвестный --account или --project")
		}
		if _, err := a.DockerArgs(config); err != nil {
			return err
		}
		containers, err := shared.Output("", "docker", "container", "ls", "-a", "--format", "{{.Names}}")
		if err != nil {
			return err
		}
		if shared.Contains(strings.Fields(containers), a.shared().Container(config)) {
			return errors.New("контейнер назначения уже существует; выберите новый --workspace")
		}
		for _, suffix := range []string{"home", "tools", "worktrees"} {
			name := "adc-" + config.Name + "-" + suffix
			// Query the complete name list: a missing volume and an unavailable
			// daemon must not be treated as the same condition.
			volumes, err := shared.Output("", "docker", "volume", "ls", "--format", "{{.Name}}")
			if err != nil {
				return err
			}
			if shared.Contains(strings.Fields(volumes), name) {
				return fmt.Errorf("том %s уже существует; нужен отдельный новый workspace", name)
			}
		}
		if err = a.EnsureImage(); err != nil {
			return err
		}
		m, err := mount(backup, "/backup", true)
		if err != nil {
			return err
		}
		uid, gid := os.Getuid(), os.Getgid()
		if uid <= 0 {
			uid, gid = 1000, 1000
		}
		pending.After = config
		journal, err := json.MarshalIndent(pending, "", "  ")
		if err != nil {
			return err
		}
		if err = shared.AtomicWrite(filepath.Join(a.State, "migration-pending.json"), append(journal, '\n'), 0600); err != nil {
			return err
		}
		// The helper sees immutable backup archives and fresh destination volumes;
		// it cannot access any old live mount or repository.
		args := []string{"run", "--rm", "--network", "none", "--read-only", "-e", "ADC_RUNTIME=1", "-e", "ADC_UID=" + strconv.Itoa(uid), "-e", "ADC_GID=" + strconv.Itoa(gid), "--mount", m, "--mount", "type=volume,src=adc-" + config.Name + "-home,dst=/home/node,volume-nocopy", "--mount", "type=volume,src=adc-" + config.Name + "-tools,dst=/opt/ai-tools,volume-nocopy", "--entrypoint", "adc", "adc-runtime:local", "runtime-legacy-import", "/backup", account}
		if err = shared.Run("", "docker", args...); err != nil {
			return fmt.Errorf("импорт не завершён; исходные данные и backup сохранены: %w", err)
		}
		if err = a.legacyFinish(pending); err != nil {
			return err
		}
		fmt.Println("Проекты зарегистрированы. Запустите adc workspace start; проверьте входы, сессии и проектные сервисы. Старые контейнеры и тома сохранены.")
		return nil
	})
}

func (a *App) LegacyFinish() error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	return a.shared().Locked(func() error {
		var pending legacyPending
		if err := legacy.ReadJSON(filepath.Join(a.State, "migration-pending.json"), &pending); err != nil {
			return err
		}
		if err := a.legacyFinish(pending); err != nil {
			return err
		}
		fmt.Println("Копирование подтверждено, проекты зарегистрированы. adc workspace start")
		return nil
	})
}

func (a *App) legacyFinish(pending legacyPending) error {
	current, err := a.shared().Load()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, pending.Before) && !reflect.DeepEqual(current, pending.After) {
		return errors.New("workspace изменился после начала импорта; автоматическая регистрация отменена")
	}
	if !shared.ValidID.MatchString(pending.After.Name) {
		return errors.New("неверный workspace в migration journal")
	}
	volume := "adc-" + pending.After.Name + "-home"
	if _, err = shared.Output("", "docker", "volume", "inspect", "--format", "{{.Name}}", volume); err != nil {
		return err
	}
	if err = shared.Run("", "docker", "run", "--rm", "--network", "none", "--read-only", "-e", "ADC_RUNTIME=1", "--mount", "type=volume,src="+volume+",dst=/home/node,readonly,volume-nocopy", "--entrypoint", "adc", "adc-runtime:local", "runtime-legacy-finish", pending.Fingerprint, pending.Account); err != nil {
		return fmt.Errorf("назначение не готово; оригиналы и backup сохранены. Для повторного копирования используйте новый ADC_HOME и --workspace: %w", err)
	}
	if err = a.shared().Save(pending.After); err != nil {
		return err
	}
	return os.Remove(filepath.Join(a.State, "migration-pending.json"))
}
