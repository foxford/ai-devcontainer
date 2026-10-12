package shared

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagedBlocksPreserveUserTextAndMtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	original := "# Our project\n\nKeep these rules.\n"
	if e := os.WriteFile(path, []byte(original), 0644); e != nil {
		t.Fatal(e)
	}
	if e := ManagedBlock(path, "<!-- start -->", "<!-- end -->", "Read core"); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), original) {
		t.Fatal("user rules overwritten")
	}
	before, _ := os.Stat(path)
	time.Sleep(time.Millisecond)
	if e := ManagedBlock(path, "<!-- start -->", "<!-- end -->", "Read core"); e != nil {
		t.Fatal(e)
	}
	after, _ := os.Stat(path)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("identical delivery changed mtime")
	}
	if e := ManagedBlock(path, "<!-- start -->", "<!-- end -->", "Updated core"); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(path)
	if strings.Count(string(b), "<!-- start -->") != 1 || !strings.Contains(string(b), "Updated core") {
		t.Fatal("block was duplicated or not updated")
	}
	if e := os.WriteFile(path, []byte("user\n<!-- start -->\nbroken"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := ManagedBlock(path, "<!-- start -->", "<!-- end -->", "overwrite"); e == nil {
		t.Fatal("accepted incomplete block")
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "overwrite") {
		t.Fatal("damaged file changed on failure")
	}
}

func TestWorkspaceRoundTripAndLock(t *testing.T) {
	a := &App{State: t.TempDir()}
	c := Config{Version: Schema, Name: "test", Port: 6768, Projects: []Project{}}
	if e := a.Save(c); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := a.Locked(func() error {
				v, e := a.Load()
				if e != nil {
					return e
				}
				v.Knowledge = append(v.Knowledge, Knowledge{Origin: "test"})
				return a.Save(v)
			}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	c, e := a.Load()
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Knowledge) != 8 {
		t.Fatal("concurrent writes lost updates")
	}
}

func TestPortableHostPathsInRuntimeRegistry(t *testing.T) {
	a := &App{State: t.TempDir()}
	c := Config{Version: Schema, Name: "windows", Port: 6768, Projects: []Project{{ID: "app", Path: `C:\Users\dev\project`, RuntimePath: "/workspace/projects/app"}}}
	if e := a.Save(c); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Load(); e != nil {
		t.Fatal("Linux runtime must accept persisted Windows host paths:", e)
	}
}

func TestProjectCapabilitiesComposeTeamRules(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, ".agents", "project.json")
	a := &App{Platform: testutil.Root(t)}
	p := Project{RuntimePath: root, Capabilities: []string{"changesets"}}
	if err := os.WriteFile(file, []byte(`{"capabilities":["pnpm-nx","changesets"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	caps, err := a.ProjectCapabilities(p)
	if err != nil || strings.Join(caps, ",") != "changesets,pnpm-nx" {
		t.Fatalf("composition: %v, %v", caps, err)
	}
	for _, invalid := range []string{`{"capabilities":["unknown"]}`, `{broken`} {
		if err := os.WriteFile(file, []byte(invalid), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := a.ProjectCapabilities(p); err == nil {
			t.Fatalf("accepted invalid team config %s", invalid)
		}
	}
}

func TestLockReleasedAfterProcessDeath(t *testing.T) {
	if os.Getenv("ADC_LOCK_CHILD") == "1" {
		a := &App{State: os.Getenv("ADC_HOME")}
		if err := a.Locked(func() error {
			fmt.Println("locked")
			for {
				time.Sleep(time.Hour)
			}
		}); err != nil {
			t.Fatal(err)
		}
		return
	}
	state := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestLockReleasedAfterProcessDeath$")
	cmd.Env = append(os.Environ(), "ADC_LOCK_CHILD=1", "ADC_HOME="+state)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "locked" {
		t.Fatalf("child: %q %v", line, err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	a := &App{State: state}
	if err = a.Locked(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestHostLifecycleLockAllowsRuntimeDelivery(t *testing.T) {
	a := &App{State: t.TempDir()}
	if err := a.LockedFile("update.lockfile", func() error {
		return a.LockedFile("lifecycle.lockfile", func() error {
			return a.Locked(func() error { return nil })
		})
	}); err != nil {
		t.Fatal("host preparation blocked runtime config delivery", err)
	}
}

func TestCatalogSelectsMCPAndAllowsNewCapabilities(t *testing.T) {
	root := testutil.Delivery(t)
	a := &App{Platform: root}
	p := Project{RuntimePath: t.TempDir(), Capabilities: []string{"playwright"}}
	env, err := a.DeliveryEnv(p)
	if err != nil {
		t.Fatal(err)
	}
	if !Contains(env, `ADC_PLATFORM_MCP_SERVERS=["playwright"]`) {
		t.Fatalf("wrong selection %v", env)
	}
	p.Capabilities = []string{"chrome-devtools"}
	env, err = a.DeliveryEnv(p)
	if err != nil || !Contains(env, `ADC_PLATFORM_MCP_SERVERS=["chrome-devtools"]`) {
		t.Fatalf("independent DevTools selection: %v %v", env, err)
	}
	p.Capabilities = []string{"frontend", "playwright", "chrome-devtools"}
	env, err = a.DeliveryEnv(p)
	if err != nil || !Contains(env, `ADC_PLATFORM_MCP_SERVERS=["playwright","chrome-devtools"]`) {
		t.Fatalf("bundle deduplication: %v %v", env, err)
	}
	p.Capabilities = nil
	env, err = a.DeliveryEnv(p)
	if err != nil || !Contains(env, `ADC_PLATFORM_MCP_SERVERS=[]`) {
		t.Fatalf("unexpected base MCP: %v %v", env, err)
	}
	catalog, err := a.DeliveryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Capabilities["custom"] = DeliveryCapability{Instructions: []string{"custom.md"}, MCP: []string{"figma"}}
	if err = AtomicWrite(a.DeliveryPath("custom.md"), []byte("Custom instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "delivery", "catalog.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	p.Capabilities = []string{"custom"}
	env, err = a.DeliveryEnv(p)
	if err != nil || !Contains(env, `ADC_PLATFORM_MCP_SERVERS=["figma"]`) {
		t.Fatalf("catalog-driven capability: %v %v", env, err)
	}
	catalog.Capabilities["custom"] = DeliveryCapability{Instructions: []string{"custom.md"}, Conflicts: []string{"playwright"}}
	data, _ = json.Marshal(catalog)
	_ = os.WriteFile(filepath.Join(root, "delivery", "catalog.json"), data, 0644)
	p.Capabilities = []string{"playwright", "custom"}
	if _, err = a.DeliveryEnv(p); err == nil {
		t.Fatal("accepted conflicting capabilities")
	}
	catalog.Capabilities["custom"] = DeliveryCapability{Instructions: []string{"custom.md"}, Documents: []DeliveryDocument{{Source: "other.md", Target: "MONOREPO.md", Mode: "link"}}}
	if err = AtomicWrite(a.DeliveryPath("other.md"), []byte("Different monorepo rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(catalog)
	_ = os.WriteFile(filepath.Join(root, "delivery", "catalog.json"), data, 0644)
	p.Capabilities = []string{"pnpm-nx", "custom"}
	if _, err = a.DeliveryEnv(p); err == nil {
		t.Fatal("accepted conflicting document destinations")
	}
	catalog.Capabilities["custom"] = DeliveryCapability{Instructions: []string{"../escape"}}
	data, _ = json.Marshal(catalog)
	_ = os.WriteFile(filepath.Join(root, "delivery", "catalog.json"), data, 0644)
	if _, err = a.DeliveryCatalog(); err == nil {
		t.Fatal("accepted escaping instruction path")
	}
}

func TestBundleInstructionsDeduplicated(t *testing.T) {
	a := &App{Platform: testutil.Root(t)}
	text, err := a.InstructionText([]string{"frontend", "chrome-devtools"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(text), "## Chrome DevTools") != 1 {
		t.Fatal("duplicate instructions from overlapping capabilities")
	}
}
