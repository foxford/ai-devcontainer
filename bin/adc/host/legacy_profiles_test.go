package host

import (
	"reflect"
	"testing"

	"github.com/foxford/ai-devcontainer/bin/adc/internal/legacy"
)

func TestLegacyProfilePlanKeepsOnlyImportedStores(t *testing.T) {
	full, err := legacyPlan([]legacyInspection{inspectFixture(t, "container-a", "app-a", "/projects/app-a"), inspectFixture(t, "container-b", "app-b", "/projects/app-b")})
	if err != nil {
		t.Fatal(err)
	}
	// A separate copy proves selecting stores cannot mutate the caller's full plan.
	want, err := legacyPlan([]legacyInspection{inspectFixture(t, "container-a", "app-a", "/projects/app-a"), inspectFixture(t, "container-b", "app-b", "/projects/app-b")})
	if err != nil {
		t.Fatal(err)
	}
	selected := legacyProfilePlan(full)
	if err := selected.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full, want) {
		t.Fatal("selection changed source plan")
	}
	if len(selected.Resources) != 3 {
		t.Fatalf("expected two profiles and deduplicated shared tools, got %d", len(selected.Resources))
	}
	for _, c := range selected.Containers {
		if c.ProjectPath == "" {
			t.Fatal("lost host checkout registration")
		}
		for _, m := range c.Mounts {
			prefix, _ := legacy.HomeMount(m.Destination)
			if prefix != "" || m.Destination == "/opt/ai-tools" {
				if m.Resource == "" {
					t.Fatal("lost profile")
				}
			} else if m.Resource != "" || m.Skip == "" {
				t.Fatal("selected checkout, DinD or temporary mount")
			}
		}
	}
}
