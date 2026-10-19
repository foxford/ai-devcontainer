package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var dshSkillName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func (defaultAgentBehavior) ProjectSkills(_ string, _ map[string]bool) error { return nil }

func (dshDriver) ProjectSkills(root string, live map[string]bool) error {
	dsh := filepath.Join(root, ".dsh", "skills")
	if err := os.MkdirAll(dsh, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dsh)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dsh, entry.Name())
		link, err := os.Readlink(path)
		if err == nil && managedDSHLink(root, entry.Name(), link) && !live[entry.Name()] {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	for name := range live {
		if !dshSkillName.MatchString(name) {
			fmt.Fprintln(os.Stderr, "DSH: имя не kebab-case:", name)
			continue
		}
		path := filepath.Join(dsh, name)
		if _, err := os.Lstat(path); err == nil {
			link, err := os.Readlink(path)
			if err != nil || !managedDSHLink(root, name, link) {
				continue
			}
		}
		target, err := filepath.Rel(dsh, filepath.Join(composedSkillsPath(root), name))
		if err != nil {
			return err
		}
		if err := replaceLink(path, target); err != nil {
			return err
		}
	}
	return nil
}

func managedDSHLink(root, name, link string) bool {
	if !filepath.IsAbs(link) {
		link = filepath.Join(root, ".dsh", "skills", link)
	}
	for _, base := range []string{root, "/workspaces/" + filepath.Base(root)} {
		if filepath.Clean(link) == filepath.Join(composedSkillsPath(base), name) {
			return true
		}
	}
	return false
}
