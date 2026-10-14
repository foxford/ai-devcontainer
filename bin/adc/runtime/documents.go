package runtime

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func ownedDocument(path, source string) bool {
	if link, err := os.Readlink(path); err == nil {
		return filepath.Clean(link) == filepath.Clean(source)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	first, _, _ := bytes.Cut(data, []byte("\n"))
	return bytes.Contains(first, []byte("ai-devcontainer:generated"))
}

func (a *App) ownedDeliveryDocument(path string, doc shared.DeliveryDocument) bool {
	if ownedDocument(path, a.shared().DeliveryPath(doc.Source)) {
		return true
	}
	link, err := os.Readlink(path)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(path), link)
	}
	link = filepath.Clean(link)
	// The source root changes after a delivery-only update and again after a
	// full platform update. Match the same document within an ADC source root;
	// a similarly named file elsewhere still belongs to the user.
	if link == filepath.Join(a.Platform, "delivery", filepath.FromSlash(doc.Source)) {
		return true
	}
	if filepath.IsAbs(a.State) {
		relative, err := filepath.Rel(filepath.Join(a.State, "delivery"), link)
		parts := strings.SplitN(relative, string(os.PathSeparator), 3)
		if err == nil && len(parts) == 3 && (len(parts[0]) == 40 || len(parts[0]) == 64) && strings.Trim(parts[0], "0123456789abcdefABCDEF") == "" && parts[1] == "delivery" && parts[2] == filepath.FromSlash(doc.Source) {
			return true
		}
	}
	legacy := map[string][]string{
		".agents/skills/README.md":        {"docs/skills-README.md", "delivery/documents/skills-README.md"},
		"plans/README.md":                 {"docs/plans-README.md", "delivery/documents/plans-README.md"},
		"MONOREPO.md":                     {"docs/MONOREPO.md", "delivery/documents/MONOREPO.md"},
		".agents/mcp.secrets.env.example": {"docs/mcp-secrets.example"},
	}
	for _, root := range []string{a.Platform, "/opt/ai-devcontainer", "/opt/dev-platform"} {
		for _, rel := range legacy[doc.Target] {
			if filepath.Clean(link) == filepath.Join(root, filepath.FromSlash(rel)) {
				return true
			}
		}
	}
	return false
}

func (a *App) documents(capabilities []string) ([]shared.DeliveryDocument, error) {
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		return nil, err
	}
	documents := append([]shared.DeliveryDocument{}, catalog.Base.Documents...)
	seen := map[string]bool{}
	for _, doc := range documents {
		seen[doc.Target] = true
	}
	for _, name := range capabilities {
		capability, ok := catalog.Capabilities[name]
		if !ok {
			return nil, fmt.Errorf("неизвестная capability %s", name)
		}
		for _, doc := range capability.Documents {
			if !seen[doc.Target] {
				documents = append(documents, doc)
				seen[doc.Target] = true
			}
		}
	}
	return documents, nil
}

func (a *App) DeliverDocuments(p shared.Project) error {
	if filepath.Clean(p.RuntimePath) == filepath.Clean(a.Platform) {
		return nil
	}
	documents, err := a.documents(p.Capabilities)
	if err != nil {
		return err
	}
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		return err
	}
	active := map[string]bool{}
	for _, doc := range documents {
		active[doc.Target] = true
	}
	for _, capability := range catalog.Capabilities {
		for _, doc := range capability.Documents {
			path := filepath.Join(p.RuntimePath, doc.Target)
			if !active[doc.Target] && a.ownedDeliveryDocument(path, doc) {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		}
	}
	for _, doc := range documents {
		if doc.Target == "AGENTS.platform.md" {
			continue
		} // Composed by SyncProject.
		path, source := filepath.Join(p.RuntimePath, doc.Target), a.shared().DeliveryPath(doc.Source)
		if _, err := os.Lstat(path); err == nil && !a.ownedDeliveryDocument(path, doc) {
			continue
		}
		if doc.Mode == "copy" {
			data, err := os.ReadFile(source)
			if err != nil {
				return err
			}
			if err := shared.AtomicWrite(path, append([]byte(shared.Marker), data...), 0644); err != nil {
				return err
			}
		} else if err := replaceLink(path, source); err != nil {
			return err
		}
	}
	return nil
}

func replaceLink(path, target string) error {
	if current, err := os.Readlink(path); err == nil && current == target {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, path)
}

func childPath(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.ContainsAny(rel, "\\:\r\n") {
		return "", fmt.Errorf("некорректный относительный путь: %q", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("путь выходит за каталог: %q", rel)
	}
	return filepath.Join(root, clean), nil
}
