package shared

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
)

func TestDeliveryVersionsCompareExplicitReleases(t *testing.T) {
	unit := func(version string) DeliveryUnit {
		return DeliveryUnit{Kind: "skill", Name: "example", Version: version}
	}
	for _, test := range []struct {
		local, remote string
		newer         bool
	}{
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", true},
		{"1.9.0", "1.10.0", true},
		{"2.0.0", "1.99.0", false},
		{"1.0.0+one", "1.0.0+two", false},
		{"3.31.4-adc.1", "3.31.4-adc.2", true},
		{"3.31.4-adc.9", "3.31.4-adc.10", true},
		{"3.31.4-adc.1", "3.31.4", true},
		{"3.31.4", "3.31.4-adc.1", false},
		{"1.0.0-alpha", "1.0.0-alpha.1", true},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", true},
		{"1.0.0", "invalid", false},
	} {
		t.Run(test.local+"_"+test.remote, func(t *testing.T) {
			changes := DiffDeliveryVersions(DeliveryVersions{"skill/example": unit(test.local)}, DeliveryVersions{"skill/example": unit(test.remote)})
			if (len(changes) == 1) != test.newer {
				t.Fatalf("unexpected changes: %#v", changes)
			}
			if test.newer && (changes[0].BeforeVersion != test.local || changes[0].AfterVersion != test.remote) {
				t.Fatal("version labels missing", changes)
			}
		})
	}
	if changes := DiffDeliveryVersions(nil, DeliveryVersions{"skill/example": unit("1.0.0")}); len(changes) != 1 || changes[0].Action != "added" {
		t.Fatal("new unit was not reported", changes)
	}
	if changes := DiffDeliveryVersions(DeliveryVersions{"skill/example": unit("1.0.0")}, nil); len(changes) != 0 {
		t.Fatal("unit removal is not a newer version", changes)
	}
}

func TestEveryPublishedDeliveryUnitHasMetadata(t *testing.T) {
	a := &App{Platform: testutil.Root(t)}
	versions, err := a.DeliveryVersions()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := a.DeliveryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	documents := map[string]bool{}
	for _, source := range catalog.Base.Instructions {
		documents[source] = true
	}
	for _, document := range catalog.Base.Documents {
		documents[document.Source] = true
	}
	entries, err := os.ReadDir(a.DeliveryPath(catalog.Base.Skills))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(a.DeliveryPath(catalog.Base.Skills + "/" + entry.Name() + "/SKILL.md")); err == nil {
				want["skill/"+entry.Name()] = true
			}
		}
	}
	for name := range catalog.MCPDefinitions {
		want["mcp/"+name] = true
	}
	for name, capability := range catalog.Capabilities {
		if capability.Bundle != "" {
			want["bundle/"+name] = true
		}
		for _, skill := range capability.Skills {
			want["skill/"+filepath.Base(skill)] = true
		}
		for _, source := range capability.Instructions {
			documents[source] = true
		}
		for _, document := range capability.Documents {
			documents[document.Source] = true
		}
	}
	for source := range documents {
		path := source + ".meta.json"
		if err := a.ValidateDeliverySource(path, false); err != nil {
			t.Fatalf("document %s must have its own sidecar metadata: %v", source, err)
		}
		data, err := os.ReadFile(a.DeliveryPath(path))
		if err != nil {
			t.Fatal(err)
		}
		var unit DeliveryUnit
		if err := json.Unmarshal(data, &unit); err != nil || unit.Kind != "document" {
			t.Fatalf("document sidecar %s must declare kind document: %+v, %v", path, unit, err)
		}
		key := "document/" + unit.Name
		if versions[key] != unit {
			t.Fatalf("document sidecar was not loaded: %s", path)
		}
		want[key] = true
	}
	got := map[string]bool{}
	for key := range versions {
		got[key] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata must correspond exactly to catalog units: got %v, want %v", got, want)
	}
}

func TestDeliveryMetadataValidationAndNoContentInspection(t *testing.T) {
	a := &App{Platform: t.TempDir()}
	path := a.DeliveryPath("skills/example/meta.json")
	write := func(body string) {
		t.Helper()
		if err := AtomicWrite(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{
		`{"kind":"skill","name":"example","version":"1.0.0"}`,
		`{"kind":"skill","name":"example","version":"1.0.0-adc.1"}`,
		`{"kind":"document","name":"platform-rules","version":"1.0.0"}`,
	} {
		write(body)
		if _, err := a.DeliveryVersions(); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := a.DeliveryVersions()
	// A changed or unreadable payload does not change a declared release.
	if err := AtomicWrite(filepath.Join(filepath.Dir(path), "SKILL.md"), []byte("Changed payload"), 0000); err != nil {
		t.Fatal(err)
	}
	after, err := a.DeliveryVersions()
	if err != nil || len(DiffDeliveryVersions(before, after)) != 0 {
		t.Fatal("payload was inspected instead of metadata", err)
	}
	for _, body := range []string{
		`{`, `{"kind":"skill","name":"../example","version":"1.0.0"}`,
		`{"kind":"invalid","name":"example","version":"1.0.0"}`,
		`{"kind":"skill","name":"example","version":"1"}`,
		`{"kind":"skill","name":"example","version":"01.0.0"}`,
		`{"kind":"skill","name":"example","version":"1.0.0-adc.01"}`,
	} {
		write(body)
		if _, err := a.DeliveryVersions(); err == nil {
			t.Fatal("invalid metadata accepted:", body)
		}
	}
	data, _ := json.Marshal(DeliveryUnit{Kind: "skill", Name: "example", Version: "1.0.0"})
	write(string(data))
	if err := AtomicWrite(a.DeliveryPath("duplicate.meta.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeliveryVersions(); err == nil {
		t.Fatal("duplicate unit accepted")
	}
}
