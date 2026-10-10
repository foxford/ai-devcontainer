package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/legacy"
)

// Select only stores consumed by the importer. Code and all other mounts remain
// at their original host paths; rebuilding tools does not require their caches.
func legacyProfilePlan(plan legacy.Plan) legacy.Plan {
	next := plan
	next.Containers = append([]legacy.Container(nil), plan.Containers...)
	next.Resources = nil
	used := map[string]bool{}
	for i := range next.Containers {
		next.Containers[i].Mounts = append([]legacy.Mount(nil), plan.Containers[i].Mounts...)
		for j := range next.Containers[i].Mounts {
			m := &next.Containers[i].Mounts[j]
			prefix, _ := legacy.HomeMount(m.Destination)
			if m.Resource != "" && (prefix != "" || m.Destination == "/opt/ai-tools") {
				used[m.Resource] = true
			} else if m.Resource != "" {
				m.Resource = ""
				m.Skip = "остаётся на хосте или в старом томе; для импорта профилей не копируется"
			}
		}
	}
	for _, r := range plan.Resources {
		if used[r.ID] {
			next.Resources = append(next.Resources, r)
		}
	}
	return next
}

func legacyToolResource(plan legacy.Plan, id string) bool {
	for _, c := range plan.Containers {
		for _, m := range c.Mounts {
			if m.Resource == id && m.Destination == "/opt/ai-tools" {
				return true
			}
		}
	}
	return false
}

// LegacyImportHost copies mounted profiles into fresh volumes using the same
// checked importer as a full snapshot. A small recovery copy is retained; no
// repository, container filesystem, DinD or cache is archived.
func (a *App) LegacyImportHost(planFile, account, workspace string, mappings []string) error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	var plan legacy.Plan
	if err := legacy.ReadJSON(planFile, &plan); err != nil {
		return err
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	if err := stoppedLegacy(plan); err != nil {
		return err
	}
	selected := false
	for _, c := range plan.Containers {
		for _, m := range c.Mounts {
			prefix, _ := legacy.HomeMount(m.Destination)
			if c.Project == account && prefix != "" && m.Resource != "" {
				selected = true
			}
		}
	}
	if !selected {
		return errors.New("--account должен выбрать проект с примонтированными профилями агентов")
	}
	config, err := a.shared().Load()
	if err != nil {
		return err
	}
	if len(config.Projects) != 0 {
		return errors.New("импорт выполняется в workspace без проектов")
	}
	if _, err := os.Lstat(filepath.Join(a.State, "migration-pending.json")); !os.IsNotExist(err) {
		return errors.New("предыдущий импорт не завершён: adc migrate legacy finish")
	}
	parent := filepath.Join(a.State, "backups", "profiles")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(parent, time.Now().UTC().Format("20060102T150405Z")+"-")
	if err != nil {
		return err
	}
	snapshot := filepath.Join(dir, "snapshot")
	fmt.Println("Импортируем примонтированные профили; репозитории, кеши и старые DinD-тома остаются на месте.")
	fmt.Println("Копия профилей:", snapshot)
	if err = a.legacySnapshot(planFile, snapshot, true); err != nil {
		return err
	}
	return a.LegacyImport(snapshot, account, workspace, mappings)
}
