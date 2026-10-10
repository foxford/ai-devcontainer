package legacy

import "strings"

func HomeMount(destination string) (string, string) {
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
