package runtime

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type planEntry struct{ Path, Status, Updated, Legacy string }

func readPlans(root string) ([]planEntry, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "task.md" && entry.Name() != "epic.md" {
			return nil
		}
		dir := filepath.Dir(path)
		if entry.Name() == "task.md" || files[dir] == "" {
			files[dir] = path
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	entries := []planEntry{}
	for dir, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, err
		}
		entry := planEntry{Path: filepath.ToSlash(rel), Status: frontmatterField(string(data), "status"), Updated: frontmatterField(string(data), "updated")}
		if entry.Status == "" {
			entry.Status = "—"
		}
		if entry.Updated == "" {
			entry.Updated = frontmatterField(string(data), "created")
		}
		if entry.Updated == "" {
			entry.Updated = "0000-00-00"
		}
		if strings.HasPrefix(entry.Path, "archive/") {
			entry.Legacy = "archive/"
		} else {
			first, _, ok := strings.Cut(entry.Path, "/")
			if !ok || len(first) != 4 || strings.Trim(first, "0123456789") != "" {
				entry.Legacy = "вне plans/<год>/"
			}
		}
		if filepath.Base(path) == "epic.md" {
			entry.Legacy += " epic.md"
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Updated == entries[j].Updated {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Updated < entries[j].Updated
	})
	return entries, nil
}

func Plans(root string, all bool) error {
	entries, err := readPlans(filepath.Join(root, "plans"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !all && (entry.Status == "done" || entry.Status == "dropped") {
			continue
		}
		fmt.Printf("%-9s %s  %s %s\n", entry.Status, entry.Updated, entry.Path, entry.Legacy)
	}
	if len(entries) == 0 {
		fmt.Println("В проекте нет задач в plans/<год>/<задача>/task.md")
	}
	return nil
}
