package shared

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDeliverySource(t *testing.T, a *App, source DeliverySource) {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(filepath.Join(a.State, "delivery", "current.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDeliverySourceSelectsReleaseUntilPlatformChanges(t *testing.T) {
	a, upstream := updateFixture(t)
	base, err := a.PlatformRevision()
	if err != nil {
		t.Fatal(err)
	}
	changeUpdateFixture(t, upstream)
	revision := updatesGit(t, upstream, "rev-parse", "HEAD")
	release := filepath.Join(a.State, "delivery", revision, "delivery")
	if err := os.MkdirAll(filepath.Dir(release), 0700); err != nil {
		t.Fatal(err)
	}
	if err := CopyPrivateTree(filepath.Join(upstream, "delivery"), release); err != nil {
		t.Fatal(err)
	}
	updatesWrite(t, release, "catalog.json", `{"version":1,"base":{"instructions":["instructions/released.md"],"skills":"skills","mcp":{"source":"mcp/servers.json","servers":[]}},"capabilities":{}}`)
	updatesWrite(t, release, "instructions/released.md", "New delivery contract\n")
	writeDeliverySource(t, a, DeliverySource{Revision: revision, PlatformRevision: base})
	if err := a.RefreshDelivery(); err != nil {
		t.Fatal(err)
	}
	if a.DeliveryPath("") != release || a.DeliveryPath("skills/humanizer") != filepath.Join(release, "skills", "humanizer") {
		t.Fatal("downloaded delivery was not selected:", a.DeliveryRoot)
	}
	catalog, err := a.DeliveryCatalog()
	if err != nil || len(catalog.Base.Instructions) != 1 || catalog.Base.Instructions[0] != "instructions/released.md" {
		t.Fatalf("catalog uses another source: %+v, %v", catalog, err)
	}
	versions, err := a.DeliveryVersions()
	if err != nil || versions["skill/humanizer"].Version != "2.0.0" {
		t.Fatalf("versions use another source: %+v, %v", versions, err)
	}
	if updatesGit(t, a.Platform, "rev-parse", "HEAD") != base {
		t.Fatal("delivery selection changed the platform checkout")
	}
	t.Setenv("AI_DEVCONTAINER_HOME", a.Platform)
	t.Setenv("ADC_HOME", a.State)
	fresh, err := New()
	if err != nil || fresh.DeliveryRoot != release {
		t.Fatalf("new command did not select active delivery: %+v, %v", fresh, err)
	}
	updatesGit(t, a.Platform, "pull", "--quiet", "--ff-only")
	if err := a.RefreshDelivery(); err != nil {
		t.Fatal(err)
	}
	if a.DeliveryRoot != "" || a.DeliveryPath("") != filepath.Join(a.Platform, "delivery") {
		t.Fatal("full update remains shadowed by downloaded delivery:", a.DeliveryRoot)
	}
	if _, err := os.Stat(filepath.Join(release, "instructions", "released.md")); err != nil {
		t.Fatal("old release links lost their target:", err)
	}
	if err := os.RemoveAll(release); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshDelivery(); err != nil {
		t.Fatal("inactive release should not be required:", err)
	}
}

func TestDeliverySourceRejectsInvalidActiveMarker(t *testing.T) {
	a, _ := updateFixture(t)
	base, err := a.PlatformRevision()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{"broken json", "{"},
		{"missing revisions", `{}`},
		{"path traversal", `{"revision":"../../outside","platformRevision":"` + base + `"}`},
		{"invalid base", `{"revision":"` + base + `","platformRevision":"invalid"}`},
		{"missing release", `{"revision":"` + base + `","platformRevision":"` + base + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updatesWrite(t, a.State, "delivery/current.json", tc.body)
			if err := a.RefreshDelivery(); err == nil || !strings.Contains(err.Error(), "источник доставки") {
				t.Fatalf("invalid source accepted: %v", err)
			}
		})
	}
	updatesWrite(t, a.State, "delivery/"+base+"/delivery", "not a directory")
	if err := a.RefreshDelivery(); err == nil {
		t.Fatal("regular file accepted as delivery root")
	}
}

func TestDeliverySourceAbsentOrUnqualifiedStateUsesPlatform(t *testing.T) {
	for _, state := range []string{"", "relative-state", t.TempDir()} {
		a := &App{Platform: t.TempDir(), State: state, DeliveryRoot: "previous"}
		if err := a.RefreshDelivery(); err != nil {
			t.Fatal(err)
		}
		if a.DeliveryRoot != "" || a.DeliveryPath("catalog.json") != filepath.Join(a.Platform, "delivery", "catalog.json") {
			t.Fatal("missing pointer must use platform without requiring Git")
		}
	}
}
