package shared

import (
	"runtime/debug"
	"strings"
)

// Revision is injected when building without Git metadata, notably in Docker.
var Revision string

func BuildVersion() string {
	revision := Revision
	if revision == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			dirty := false
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" {
					revision = setting.Value
				}
				if setting.Key == "vcs.modified" {
					dirty = setting.Value == "true"
				}
			}
			if dirty && revision != "" {
				revision += "-dirty"
			}
		}
	}
	return versionWithRevision(revision)
}

func versionWithRevision(revision string) string {
	if revision == "" {
		revision = "unknown"
	}
	hash, suffix, dirty := strings.Cut(revision, "-dirty")
	if len(hash) > 12 {
		hash = hash[:12]
	}
	if dirty && suffix == "" {
		hash += "-dirty"
	}
	return "0.1.0 (" + hash + ")"
}

func SourceRevision(root string) string {
	revision, err := Output(root, "git", "rev-parse", "HEAD")
	if err != nil {
		return "unknown"
	}
	// Only a SHA is passed to a linker argument. No Git output can add flags.
	if len(revision) != 40 && len(revision) != 64 {
		return "unknown"
	}
	for _, c := range revision {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return "unknown"
		}
	}
	if status, err := Output(root, "git", "status", "--porcelain"); err == nil && status != "" {
		revision += "-dirty"
	}
	return revision
}
