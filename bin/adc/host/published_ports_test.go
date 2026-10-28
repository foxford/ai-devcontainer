package host

import (
	"path/filepath"
	"reflect"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func TestDockerPublishedPortsKeepBuiltinsAndUseExplicitAddresses(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // Argument generation needs no Docker daemon.
	a := &App{State: filepath.Join(t.TempDir(), "state"), Platform: filepath.Join(t.TempDir(), "platform")}
	c := shared.Config{Name: "test", Port: 6770, PublishedPorts: []shared.PortMapping{
		{HostIP: "0.0.0.0", HostPort: 8080, ContainerPort: 8000},
		{HostIP: "127.0.0.1", HostPort: 3000, ContainerPort: 3000},
		{HostIP: "::1", HostPort: 9090, ContainerPort: 9000},
	}}
	before := append([]shared.PortMapping(nil), c.PublishedPorts...)
	args, err := a.DockerArgs(c)
	if err != nil {
		t.Fatal(err)
	}
	var mappings []string
	for i, arg := range args {
		if arg == "-p" {
			if i+1 >= len(args)-1 {
				t.Fatal("mapping after image", args)
			}
			mappings = append(mappings, args[i+1])
		}
	}
	want := []string{"127.0.0.1:6770:6768", "127.0.0.1:20128:20128", "127.0.0.1:4242:4242", "127.0.0.1:3000:3000", "0.0.0.0:8080:8000", "[::1]:9090:9000"}
	if !reflect.DeepEqual(mappings, want) {
		t.Fatalf("mappings=%v want=%v", mappings, want)
	}
	if args[len(args)-1] != "adc-runtime:local" {
		t.Fatal("image must remain last", args)
	}
	if !reflect.DeepEqual(c.PublishedPorts, before) {
		t.Fatal("argument generation mutated saved mapping order")
	}

	c.PublishedPorts = []shared.PortMapping{before[2], before[1], before[0]}
	reordered, err := a.DockerArgs(c)
	if err != nil || !reflect.DeepEqual(args, reordered) {
		t.Fatalf("reordering would recreate workspace: %v", err)
	}
}

func TestDockerPublishedPortChangesAffectLaunchAndClearRestoresDefaults(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a := &App{State: filepath.Join(t.TempDir(), "state"), Platform: filepath.Join(t.TempDir(), "platform")}
	c := shared.Config{Name: "test", Port: 6770}
	baseline, err := a.DockerArgs(c)
	if err != nil {
		t.Fatal(err)
	}
	c.PublishedPorts = []shared.PortMapping{{HostIP: "127.0.0.1", HostPort: 3000, ContainerPort: 3000}}
	first, err := a.DockerArgs(c)
	if err != nil || reflect.DeepEqual(first, baseline) {
		t.Fatalf("publication did not affect launch: %v", err)
	}
	for _, change := range []shared.PortMapping{
		{HostIP: "0.0.0.0", HostPort: 3000, ContainerPort: 3000},
		{HostIP: "127.0.0.1", HostPort: 3001, ContainerPort: 3000},
		{HostIP: "127.0.0.1", HostPort: 3000, ContainerPort: 3001},
	} {
		c.PublishedPorts = []shared.PortMapping{change}
		changed, err := a.DockerArgs(c)
		if err != nil || reflect.DeepEqual(first, changed) {
			t.Fatalf("mapping change did not affect launch: %+v %v", change, err)
		}
	}
	c.PublishedPorts = nil
	cleared, err := a.DockerArgs(c)
	if err != nil || !reflect.DeepEqual(baseline, cleared) {
		t.Fatalf("clear changed built-in arguments: %v", err)
	}
}

func TestDockerRejectsInvalidPublishedPortsBeforeHardwareOrDockerAccess(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a := &App{State: t.TempDir(), Platform: t.TempDir()}
	for _, mapping := range []shared.PortMapping{
		{HostIP: "127.0.0.1", HostPort: 6770, ContainerPort: 3000},
		{HostIP: "127.0.0.1", HostPort: 0, ContainerPort: 3000},
		{HostIP: "127.0.0.1", HostPort: 3000, ContainerPort: 65536},
		{HostIP: "invalid", HostPort: 3000, ContainerPort: 3000},
	} {
		c := shared.Config{Name: "test", Port: 6770, USBDevices: []string{"2104:0313"}, PublishedPorts: []shared.PortMapping{mapping}}
		args, err := a.DockerArgs(c)
		if err == nil || len(args) != 0 {
			t.Fatalf("accepted mapping %+v: %v", mapping, args)
		}
		if err := c.ValidatePorts(); err == nil {
			t.Fatal("failure was not port validation")
		}
	}
}
