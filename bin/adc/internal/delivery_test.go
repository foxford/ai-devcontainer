package shared

import (
	"encoding/json"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCatalogRejectsBrokenSourcesBeforeSelection(t *testing.T) {
	for _, kind := range []string{"instruction", "document", "skills", "mcp"} {
		t.Run(kind, func(t *testing.T) {
			a := &App{Platform: testutil.Delivery(t)}
			catalog, err := a.DeliveryCatalog()
			if err != nil {
				t.Fatal(err)
			}
			var source string
			switch kind {
			case "instruction":
				source = catalog.Capabilities["figma"].Instructions[0]
			case "document":
				source = catalog.Capabilities["pnpm-nx"].Documents[0].Source
			case "skills":
				source = catalog.Base.Skills
			case "mcp":
				source = catalog.Base.MCP.Source
			}
			if err = os.Remove(a.DeliveryPath(source)); err != nil {
				t.Fatal(err)
			}
			// Even an unselected capability must not point to nonexistent payloads.
			if _, err = a.InstructionText(nil); err == nil || !strings.Contains(err.Error(), source) {
				t.Fatalf("missing catalog source was accepted: %v", err)
			}
		})
	}
}

func TestCatalogRejectsUnknownMCPInUnselectedCapability(t *testing.T) {
	a := &App{Platform: testutil.Delivery(t)}
	catalog, err := a.DeliveryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	capability := catalog.Capabilities["figma"]
	capability.MCP = []string{"missing-server"}
	catalog.Capabilities["figma"] = capability
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err = AtomicWrite(a.DeliveryPath("catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = a.InstructionText(nil); err == nil || !strings.Contains(err.Error(), "missing-server") {
		t.Fatalf("unknown MCP was accepted: %v", err)
	}
}

func TestDeliverySourceCannotEscapeThroughSymlink(t *testing.T) {
	a := &App{Platform: testutil.Delivery(t)}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := a.DeliveryPath("instructions/AGENTS.platform.md")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("Windows symlink privilege is unavailable:", err)
		}
		t.Fatal(err)
	}
	if _, err := a.DeliveryCatalog(); err == nil || !strings.Contains(err.Error(), "за пределы") {
		t.Fatalf("external symlink accepted: %v", err)
	}
}
