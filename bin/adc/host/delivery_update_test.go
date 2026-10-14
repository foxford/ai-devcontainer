package host

import (
	"errors"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func TestApplyDeliveryPreservesWorkspaceLifecycle(t *testing.T) {
	for _, state := range []string{"uninitialized", "absent", "created", "exited", "paused", "restarting", "running"} {
		t.Run(state, func(t *testing.T) {
			a := &App{State: t.TempDir()}
			c := shared.Config{Version: shared.Schema, Name: "delivery", Port: 6768}
			if state != "uninitialized" {
				if err := a.shared().Save(c); err != nil {
					t.Fatal(err)
				}
			}
			fixture := replacementDocker{containers: map[string]*workspaceContainer{}}
			if state != "absent" && state != "uninitialized" {
				container := &workspaceContainer{ID: "same-id", Image: "same-image"}
				container.State.Status = state
				fixture.containers["adc-delivery"] = container
			}
			called := false
			err := a.applyDelivery(fixture.commands(), func() error { called = true; return nil })
			if err != nil || called != (state == "running") || len(fixture.calls) != 0 {
				t.Fatalf("unexpected lifecycle action: state=%s apply=%v calls=%v err=%v", state, called, fixture.calls, err)
			}
			if container := fixture.containers["adc-delivery"]; container != nil && (container.ID != "same-id" || container.Image != "same-image" || container.State.Status != state) {
				t.Fatal("workspace was changed")
			}
		})
	}
}

func TestApplyDeliveryReportsDockerAndApplicationErrors(t *testing.T) {
	a := &App{State: t.TempDir()}
	if err := a.shared().Save(shared.Config{Version: shared.Schema, Name: "delivery", Port: 6768}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("fixture failure")
	called := false
	err := a.applyDelivery(dockerCommands{output: func(...string) (string, error) { return "", failure }}, func() error { called = true; return nil })
	if !errors.Is(err, failure) || called {
		t.Fatal("Docker failure reported as deferred success", err)
	}
	container := &workspaceContainer{ID: "same-id"}
	container.State.Status = "running"
	fixture := replacementDocker{containers: map[string]*workspaceContainer{"adc-delivery": container}}
	if err := a.applyDelivery(fixture.commands(), func() error { return failure }); !errors.Is(err, failure) {
		t.Fatal("application failure ignored", err)
	}
}
