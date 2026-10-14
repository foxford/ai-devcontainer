package runtime

import (
	"os/exec"
	"path/filepath"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// Keep registered entries unchanged, including missing and read-only projects.
// Worktrees are discovered only through their repository's Git registry and
// verified against it; an existing directory at a stale path is not sufficient.
func allSyncCheckouts(projects []shared.Project) []shared.Project {
	selected := append([]shared.Project(nil), projects...)
	seen := map[string]bool{}
	for _, p := range projects {
		if root, err := shared.Abs(p.RuntimePath); err == nil {
			seen[root] = true
		}
	}
	for _, p := range projects {
		common, err := checkoutGitPath(p.RuntimePath, "--git-common-dir")
		if err != nil {
			continue
		}
		listing, err := shared.Output(p.RuntimePath, "git", "worktree", "list", "--porcelain", "-z")
		if err != nil {
			continue
		}
		for _, record := range strings.Split(listing, "\x00\x00") {
			fields := strings.Split(record, "\x00")
			if len(fields) == 0 || !strings.HasPrefix(fields[0], "worktree ") {
				continue
			}
			bareOrPrunable := false
			for _, field := range fields[1:] {
				if field == "bare" || field == "prunable" || strings.HasPrefix(field, "prunable ") {
					bareOrPrunable = true
				}
			}
			candidate := strings.TrimPrefix(fields[0], "worktree ")
			if bareOrPrunable || !filepath.IsAbs(candidate) {
				continue
			}
			root, err := shared.Abs(candidate)
			if err != nil || seen[root] {
				continue
			}
			actualRoot, err := checkoutGitPath(root, "--show-toplevel")
			if err != nil || actualRoot != root {
				continue
			}
			other, err := checkoutGitPath(root, "--git-common-dir")
			if err != nil || other != common {
				continue
			}
			checkout := p
			checkout.RuntimePath = root
			selected = append(selected, checkout)
			seen[root] = true
		}
	}
	return selected
}

func checkoutGitPath(root, field string) (string, error) {
	command := exec.Command("git", "rev-parse", "--path-format=absolute", field)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	// Remove Git's terminator only: whitespace is valid inside checkout paths.
	return shared.Abs(strings.TrimSuffix(string(output), "\n"))
}
