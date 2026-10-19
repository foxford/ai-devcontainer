package runtime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"github.com/foxford/ai-devcontainer/bin/adc/internal/testutil"
	"go.yaml.in/yaml/v3"
)

func scopedAgentFixture(t *testing.T) (agentDelivery, agentDelivery, string) {
	t.Helper()
	home := testutil.Home(t)
	t.Setenv("HERMES_HOME", filepath.Join(home, ".hermes"))
	t.Setenv("DSH_HOME", filepath.Join(home, ".dsh"))
	policy := t.TempDir()
	t.Setenv("HERMES_MANAGED_DIR", policy)
	newDelivery := func(name string) agentDelivery {
		root := t.TempDir()
		return agentDelivery{
			Home: home, Namespace: name,
			Project:  shared.Project{ID: name, RuntimePath: root},
			Servers:  map[string]MCPServer{name + "-service": {"command": "fixture", "args": []any{root}}},
			Previous: agentState{Servers: []string{name + "-service"}},
		}
	}
	return newDelivery("alpha"), newDelivery("beta"), policy
}

func commitScopedAgent(t *testing.T, driver agentDriver, delivery agentDelivery) {
	t.Helper()
	commit, err := driver.Prepare(delivery)
	if err != nil {
		t.Fatal(err)
	}
	if err := commit.Commit(); err != nil {
		t.Fatal(err)
	}
}

func readScopedYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	var config map[string]any
	if err := yaml.Unmarshal([]byte(readFixture(t, path)), &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestHermesDriverScopesCheckoutsAndPreservesPolicy(t *testing.T) {
	a, b, policy := scopedAgentFixture(t)
	homeConfig := filepath.Join(a.Home, ".hermes", "config.yaml")
	writeFixture(t, homeConfig, "# personal\nmodel: personal-model\nmcp_servers:\n  personal:\n    command: mine\n  alpha-service:\n    command: stale-alpha\n  beta-service:\n    command: stale-beta\n")
	policyConfig := "model:\n  default: managed-model\nmcp_servers:\n  organization:\n    command: organization-cli\n  alpha-service:\n    env:\n      POLICY: required\n"
	writeFixture(t, filepath.Join(policy, "config.yaml"), policyConfig)
	environment := "# organization environment\nORG_TOKEN=fixture-secret\n"
	writeFixture(t, filepath.Join(policy, ".env"), environment)
	a.Servers["alpha-service"]["env"] = map[string]any{"POLICY": "overridden", "LOCAL": "kept"}
	for _, delivery := range []agentDelivery{a, b, a} {
		commitScopedAgent(t, hermesDriver{}, delivery)
	}
	home := readScopedYAML(t, homeConfig)
	homeServers := home["mcp_servers"].(map[string]any)
	if home["model"] != "personal-model" || len(homeServers) != 1 || homeServers["personal"] == nil {
		t.Fatal("legacy migration lost personal preferences or retained project servers")
	}
	for _, delivery := range []agentDelivery{a, b} {
		path := hermesManagedPath(delivery.Project.RuntimePath)
		config := readScopedYAML(t, filepath.Join(path, "config.yaml"))
		servers := config["mcp_servers"].(map[string]any)
		if servers[delivery.Namespace+"-service"] == nil || servers["organization"] == nil || servers["personal"] != nil {
			t.Fatal("scoped config lost project/policy servers or duplicated the home config")
		}
		if delivery.Namespace == "alpha" && servers["beta-service"] != nil {
			t.Fatal("another checkout's MCP leaked into the managed overlay")
		}
		if config["model"].(map[string]any)["default"] != "managed-model" || readFixture(t, filepath.Join(path, ".env")) != environment {
			t.Fatal("existing managed policy or environment changed")
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(filepath.Join(path, ".env"))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("copied managed environment is not private")
			}
		}
	}
	configA := readScopedYAML(t, filepath.Join(hermesManagedPath(a.Project.RuntimePath), "config.yaml"))
	server := configA["mcp_servers"].(map[string]any)["alpha-service"].(map[string]any)
	if !reflect.DeepEqual(server["env"], map[string]any{"POLICY": "required", "LOCAL": "kept"}) || server["command"] != "fixture" {
		t.Fatal("managed policy did not retain precedence over the ADC server fields")
	}
	if readFixture(t, filepath.Join(policy, "config.yaml")) != policyConfig || readFixture(t, filepath.Join(policy, ".env")) != environment {
		t.Fatal("source managed policy was modified")
	}
}

func TestHermesDriverNestedLaunchDoesNotInheritParentMCP(t *testing.T) {
	a, b, policy := scopedAgentFixture(t)
	writeFixture(t, filepath.Join(policy, "config.yaml"), "model:\n  default: organization\n")
	writeFixture(t, filepath.Join(policy, ".env"), "TOKEN=old\n")
	commitScopedAgent(t, hermesDriver{}, a)
	args := []string{"chat", "--query", "hello"}
	gotArgs, env, err := (hermesDriver{}).Launch(a.Project.RuntimePath, args, false)
	if err != nil || !reflect.DeepEqual(args, gotArgs) || len(env) != 1 || env["HERMES_MANAGED_DIR"] != hermesManagedPath(a.Project.RuntimePath) {
		t.Fatalf("unexpected Hermes launch: %v %v %v", gotArgs, env, err)
	}
	t.Setenv("HERMES_MANAGED_DIR", env["HERMES_MANAGED_DIR"])
	commitScopedAgent(t, hermesDriver{}, b)
	content := readFixture(t, filepath.Join(hermesManagedPath(b.Project.RuntimePath), "config.yaml"))
	if strings.Contains(content, "alpha-service") || !strings.Contains(content, "beta-service") || !strings.Contains(content, "organization") {
		t.Fatal("nested launch inherited another checkout's MCP or lost the source policy")
	}
	if err := os.Remove(filepath.Join(policy, ".env")); err != nil {
		t.Fatal(err)
	}
	commitScopedAgent(t, hermesDriver{}, b)
	if _, err := os.Stat(filepath.Join(hermesManagedPath(b.Project.RuntimePath), ".env")); !os.IsNotExist(err) {
		t.Fatal("removed source environment left stale secrets in the overlay")
	}
	a.Servers = map[string]MCPServer{}
	commitScopedAgent(t, hermesDriver{}, a)
	if strings.Contains(readFixture(t, filepath.Join(hermesManagedPath(a.Project.RuntimePath), "config.yaml")), "alpha-service") {
		t.Fatal("removed MCP remains in the checkout overlay")
	}
}

func TestHermesDriverValidatesPolicyBeforeWriting(t *testing.T) {
	for _, invalid := range []string{"invalid-yaml", "source-cycle"} {
		t.Run(invalid, func(t *testing.T) {
			a, _, policy := scopedAgentFixture(t)
			path := filepath.Join(a.Home, ".hermes", "config.yaml")
			original := "mcp_servers:\n  alpha-service:\n    command: keep-until-valid\n"
			writeFixture(t, path, original)
			if invalid == "invalid-yaml" {
				writeFixture(t, filepath.Join(policy, "config.yaml"), "model: [\n")
			} else {
				metadata, err := json.Marshal(hermesManagedSource{Marker: hermesManagedMarker, Source: policy})
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(policy, "adc-source.json"), string(metadata))
			}
			if _, err := (hermesDriver{}).Prepare(a); err == nil {
				t.Fatal("invalid managed source accepted")
			}
			if readFixture(t, path) != original {
				t.Fatal("validation changed the existing home configuration")
			}
		})
	}
}

func TestDSHDriverScopesCheckoutsAndPreservesHomePatches(t *testing.T) {
	a, b, _ := scopedAgentFixture(t)
	homePatch := filepath.Join(a.Home, ".dsh", "cordis.patch.yml")
	writeFixture(t, homePatch, "# personal patches\n- insert:\n    - id: personal\n      name: personal-plugin\n")
	for _, delivery := range []agentDelivery{a, b} {
		legacy, err := dshMCP(homePatch, delivery.Namespace, delivery.Project.RuntimePath, delivery.Servers, nil)
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, homePatch, string(legacy))
	}
	for _, delivery := range []agentDelivery{a, b, a} {
		commitScopedAgent(t, dshDriver{}, delivery)
	}
	home := readFixture(t, homePatch)
	if !strings.Contains(home, "# personal patches") || !strings.Contains(home, "personal-plugin") || strings.Contains(home, "alpha-service") || strings.Contains(home, "beta-service") {
		t.Fatal("home migration lost user patches or retained project MCP")
	}
	for _, pair := range [][2]agentDelivery{{a, b}, {b, a}} {
		config := readFixture(t, dshScopedPath(pair[0].Project.RuntimePath))
		if !strings.Contains(config, pair[0].Namespace+"-service") || strings.Contains(config, pair[1].Namespace+"-service") {
			t.Fatal("checkout patch contains another project's MCP")
		}
	}
	a.Servers = map[string]MCPServer{}
	commitScopedAgent(t, dshDriver{}, a)
	var patches []any
	data := readFixture(t, dshScopedPath(a.Project.RuntimePath))
	if err := yaml.Unmarshal([]byte(data), &patches); err != nil || len(patches) != 0 {
		t.Fatalf("empty configuration is not a valid empty patch list: %q %v", data, err)
	}
}

func TestDSHDriverLaunchPreservesNativeArguments(t *testing.T) {
	a, _, _ := scopedAgentFixture(t)
	commitScopedAgent(t, dshDriver{}, a)
	for _, test := range []struct {
		args, want []string
	}{
		{[]string{"web", "--port", "8080"}, []string{"--profile", "web", "--port", "8080"}},
		{[]string{"--profile", "headless", "job"}, []string{"--profile", "headless", "job"}},
		{[]string{"--patch", "personal.yml", "--profile", "web"}, []string{"--patch", "personal.yml", "--profile", "web"}},
	} {
		got, env, err := (dshDriver{}).Launch(a.Project.RuntimePath, test.args, false)
		want := append([]string{"--patch", dshScopedPath(a.Project.RuntimePath)}, test.want...)
		if err != nil || !reflect.DeepEqual(got, want) || len(env) != 0 {
			t.Fatalf("DSH launch = %v %v %v; want %v", got, env, err, want)
		}
	}
}

func TestScopedAgentMigratesAllKnownLegacyCheckouts(t *testing.T) {
	a, b, _ := scopedAgentFixture(t)
	a.Registry = []agentCheckout{{Namespace: a.Namespace, State: a.Previous}, {Namespace: b.Namespace, State: b.Previous}}
	homeConfig := filepath.Join(a.Home, ".hermes", "config.yaml")
	writeFixture(t, homeConfig, "mcp_servers:\n  alpha-service:\n    command: old-alpha\n  beta-service:\n    command: old-beta\n  personal:\n    command: mine\n")
	homePatch := filepath.Join(a.Home, ".dsh", "cordis.patch.yml")
	writeFixture(t, homePatch, "# personal\n- insert:\n    - id: personal\n      name: personal-plugin\n")
	for _, delivery := range []agentDelivery{a, b} {
		legacy, err := dshMCP(homePatch, delivery.Namespace, delivery.Project.RuntimePath, delivery.Servers, nil)
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, homePatch, string(legacy))
	}
	commitScopedAgent(t, hermesDriver{}, a)
	commitScopedAgent(t, dshDriver{}, a)
	for _, path := range []string{homeConfig, homePatch} {
		content := readFixture(t, path)
		if strings.Contains(content, "alpha-service") || strings.Contains(content, "beta-service") || !strings.Contains(content, "personal") {
			t.Fatal("one checkout sync did not clean all known legacy entries or removed personal settings")
		}
	}
}

func TestScopedAgentAdministrativeLaunchDoesNotRequireDelivery(t *testing.T) {
	for _, driver := range []agentDriver{hermesDriver{}, dshDriver{}} {
		args := []string{"--version"}
		got, env, err := driver.Launch(t.TempDir(), args, true)
		if err != nil || !reflect.DeepEqual(got, args) || len(env) != 0 {
			t.Fatalf("%s administrative launch changed: %v %v %v", driver.Name(), got, env, err)
		}
	}
	args := []string{"plugin", "--profile", "web", "list"}
	got, env, err := (dshDriver{}).Launch(t.TempDir(), args, false)
	if err != nil || !reflect.DeepEqual(got, args) || len(env) != 0 {
		t.Fatal("DSH plugin command received agent options")
	}
	args = []string{"web", "--dump-default-config"}
	got, env, err = (dshDriver{}).Launch(t.TempDir(), args, false)
	if err != nil || !reflect.DeepEqual(got, args) || len(env) != 0 {
		t.Fatal("DSH bundle defaults dump received a project patch")
	}
}

func TestHermesDriverNativeConfig(t *testing.T) {
	python, source := os.Getenv("ADC_HERMES_TEST_PYTHON"), os.Getenv("ADC_HERMES_TEST_SOURCE")
	if python == "" || source == "" {
		t.Skip("set ADC_HERMES_TEST_PYTHON and ADC_HERMES_TEST_SOURCE for the installed Hermes runtime")
	}
	a, b, policy := scopedAgentFixture(t)
	writeFixture(t, filepath.Join(a.Home, ".hermes", "config.yaml"), "mcp_servers:\n  personal:\n    command: mine\n")
	writeFixture(t, filepath.Join(policy, "config.yaml"), "model:\n  default: managed-model\nhooks:\n  organization:\n    enabled: true\n")
	writeFixture(t, filepath.Join(policy, ".env"), "ORG_TOKEN=fixture-policy-token\n")
	for _, delivery := range []agentDelivery{a, b} {
		commitScopedAgent(t, hermesDriver{}, delivery)
		_, overrides, err := (hermesDriver{}).Launch(delivery.Project.RuntimePath, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		// Load only native configuration; no model requests or MCP connections.
		cmd := exec.Command(python, "-I", "-B", "-c", `
import sys
sys.path.insert(0, sys.argv[1])
from hermes_cli.config import load_config
from hermes_cli.managed_scope import load_managed_env
config = load_config()
assert set(config["mcp_servers"]) == {"personal", sys.argv[2] + "-service"}
assert config["model"]["default"] == "managed-model"
assert config["hooks"]["organization"]["enabled"] is True
assert load_managed_env()["ORG_TOKEN"] == "fixture-policy-token"
print("native Hermes configuration isolated")
`, source, delivery.Namespace)
		cmd.Dir = delivery.Project.RuntimePath
		cmd.Env = []string{
			"HOME=" + a.Home,
			"HERMES_HOME=" + filepath.Join(a.Home, ".hermes"),
			"HERMES_MANAGED_DIR=" + overrides["HERMES_MANAGED_DIR"],
			"PATH=" + os.Getenv("PATH"),
			"LANG=C.UTF-8",
		}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("native Hermes configuration failed: %v\n%s", err, output)
		}
	}
}

func TestDSHDriverNativeConfig(t *testing.T) {
	binary := os.Getenv("ADC_DSH_TEST_BINARY")
	if binary == "" {
		t.Skip("set ADC_DSH_TEST_BINARY for the installed DSH runtime")
	}
	a, b, _ := scopedAgentFixture(t)
	writeFixture(t, filepath.Join(a.Home, ".dsh", "cordis.patch.yml"), "- insert:\n    - id: personal\n      name: personal-plugin\n")
	for _, pair := range [][2]agentDelivery{{a, b}, {b, a}} {
		delivery := pair[0]
		commitScopedAgent(t, dshDriver{}, delivery)
		args, _, err := (dshDriver{}).Launch(delivery.Project.RuntimePath, []string{"web", "--dump-config"}, false)
		if err != nil {
			t.Fatal(err)
		}
		// Native dump composes patches without booting plugins or making model calls.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = delivery.Project.RuntimePath
		cmd.Env = []string{
			"HOME=" + a.Home,
			"DSH_HOME=" + filepath.Join(a.Home, ".dsh"),
			"PATH=" + os.Getenv("PATH"),
			"LANG=C.UTF-8",
		}
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("native DSH configuration failed: %v\n%s", err, output)
		}
		text := string(output)
		if !strings.Contains(text, delivery.Namespace+"-service") || strings.Contains(text, pair[1].Namespace+"-service") || !strings.Contains(text, "personal-plugin") {
			t.Fatal("native DSH configuration leaked another checkout or lost personal patches")
		}
	}
}
