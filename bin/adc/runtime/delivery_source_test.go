package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func TestCommonSkillsFollowDeliveryReleasesAndFullUpdate(t *testing.T) {
	requireSymlinks(t)
	home := testutil.Home(t)
	a := &App{Platform: testutil.Delivery(t), State: t.TempDir(), Runtime: true}
	p := shared.Project{ID: "demo", RuntimePath: t.TempDir()}
	destination := filepath.Join(home, ".codex", "skills")
	if err := os.MkdirAll(destination, 0755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(a.State, "delivery-external", "personal")
	if err := os.MkdirAll(foreign, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(destination, "personal")); err != nil {
		t.Fatal(err)
	}
	roots := []string{""}
	for _, revision := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		release := filepath.Join(a.State, "delivery", revision, "delivery")
		if err := os.MkdirAll(filepath.Dir(release), 0755); err != nil {
			t.Fatal(err)
		}
		if err := shared.CopyPrivateTree(filepath.Join(a.Platform, "delivery"), release); err != nil {
			t.Fatal(err)
		}
		roots = append(roots, release)
	}
	roots = append(roots, "")
	for _, root := range roots {
		a.DeliveryRoot = root
		for _, name := range []string{"demo", "personal"} {
			writeFixture(t, a.shared().DeliveryPath("skills/"+name+"/SKILL.md"), "Platform skill\n")
		}
		if err := a.PublishProjectSkills(p, "demo-main"); err != nil {
			t.Fatal(err)
		}
		link, err := os.Readlink(filepath.Join(destination, "demo"))
		if err != nil || link != a.shared().DeliveryPath("skills/demo") {
			t.Fatalf("common skill stayed on another delivery source: %q, %v", link, err)
		}
		link, err = os.Readlink(filepath.Join(destination, "personal"))
		if err != nil || link != foreign {
			t.Fatalf("foreign link replaced: %q, %v", link, err)
		}
	}
}

func TestDocumentsFollowDeliveryReleasesAndPreserveForeignLinks(t *testing.T) {
	requireSymlinks(t)
	a := &App{Platform: testutil.Delivery(t), State: t.TempDir(), Runtime: true}
	root := t.TempDir()
	p := shared.Project{ID: "demo", RuntimePath: root, Capabilities: []string{"notes"}}
	doc := shared.DeliveryDocument{Source: "bundles/notes/guide.md", Target: "GUIDE.md", Mode: "link"}
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Capabilities["notes"] = shared.DeliveryCapability{
		Description:  "Notes",
		Instructions: []string{"instructions/AGENTS.platform.md"},
		Documents:    []shared.DeliveryDocument{doc},
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, a.shared().DeliveryPath("catalog.json"), string(data))
	writeFixture(t, a.shared().DeliveryPath(doc.Source), "Guide\n")
	roots := []string{""}
	for _, revision := range []string{strings.Repeat("a", 40), strings.Repeat("b", 64)} {
		release := filepath.Join(a.State, "delivery", revision, "delivery")
		if err := os.MkdirAll(filepath.Dir(release), 0755); err != nil {
			t.Fatal(err)
		}
		if err := shared.CopyPrivateTree(filepath.Join(a.Platform, "delivery"), release); err != nil {
			t.Fatal(err)
		}
		roots = append(roots, release)
	}
	roots = append(roots, "")
	target := filepath.Join(root, doc.Target)
	for _, delivery := range roots {
		a.DeliveryRoot = delivery
		if err := a.DeliverDocuments(p); err != nil {
			t.Fatal(err)
		}
		link, err := os.Readlink(target)
		if err != nil || link != a.shared().DeliveryPath(doc.Source) {
			t.Fatalf("document stayed on another delivery source: %q, %v", link, err)
		}
	}
	// Disabling a capability also owns its document from a previous release.
	a.DeliveryRoot = roots[1]
	if err := a.DeliverDocuments(p); err != nil {
		t.Fatal(err)
	}
	a.DeliveryRoot = roots[2]
	p.Capabilities = nil
	if err := a.DeliverDocuments(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("inactive document from previous release was retained:", err)
	}
	for _, foreign := range []string{
		filepath.Join(a.State, "delivery-external", strings.Repeat("a", 40), "delivery", filepath.FromSlash(doc.Source)),
		filepath.Join(a.State, "delivery", "my-notes", "delivery", filepath.FromSlash(doc.Source)),
		filepath.Join(a.State, "delivery", strings.Repeat("a", 40), "delivery", "other.md"),
	} {
		if err := os.Symlink(foreign, target); err != nil {
			t.Fatal(err)
		}
		for _, capabilities := range [][]string{{"notes"}, nil} {
			p.Capabilities = capabilities
			if err := a.DeliverDocuments(p); err != nil {
				t.Fatal(err)
			}
			link, err := os.Readlink(target)
			if err != nil || link != foreign {
				t.Fatalf("foreign document link changed: %q, %v", link, err)
			}
		}
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
	}
}
