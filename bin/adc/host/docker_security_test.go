package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func TestDockerSeccompProfile(t *testing.T) {
	var profile struct {
		DefaultAction string `json:"defaultAction"`
		Syscalls      []struct {
			Names  []string `json:"names"`
			Action string   `json:"action"`
			Args   []struct {
				Index    int    `json:"index"`
				Value    uint64 `json:"value"`
				ValueTwo uint64 `json:"valueTwo"`
				Op       string `json:"op"`
			} `json:"args"`
			Includes json.RawMessage `json:"includes"`
			Excludes json.RawMessage `json:"excludes"`
		} `json:"syscalls"`
	}
	if err := json.Unmarshal(dockerSeccompProfile, &profile); err != nil {
		t.Fatal(err)
	}
	if profile.DefaultAction != "SCMP_ACT_ERRNO" {
		t.Fatal("seccomp must deny unspecified syscalls")
	}
	n := len(profile.Syscalls)
	if n < 2 {
		t.Fatal("missing upstream profile")
	}
	namespace := profile.Syscalls[n-2]
	if strings.Join(namespace.Names, ",") != "unshare,mount,umount2,pivot_root" || namespace.Action != "SCMP_ACT_ALLOW" || len(namespace.Args) != 0 {
		t.Fatal("unexpected namespace exceptions")
	}
	clone := profile.Syscalls[n-1]
	if strings.Join(clone.Names, ",") != "clone" || clone.Action != "SCMP_ACT_ALLOW" || len(clone.Args) != 1 {
		t.Fatal("unexpected clone exception")
	}
	arg := clone.Args[0]
	if arg.Index != 0 || arg.Value != 0x10000000 || arg.ValueTwo != 0x10000000 || arg.Op != "SCMP_CMP_MASKED_EQ" {
		t.Fatal("clone exception must require CLONE_NEWUSER")
	}
	for _, rule := range profile.Syscalls[:n-2] {
		for _, name := range rule.Names {
			if shared.Contains([]string{"bpf", "setns", "ptrace", "perf_event_open"}, name) && rule.Action == "SCMP_ACT_ALLOW" && len(rule.Includes) == 0 && len(rule.Excludes) == 0 && len(rule.Args) == 0 {
				t.Fatalf("lost upstream restriction for %s", name)
			}
		}
	}
}

func TestDockerAppArmorPreparation(t *testing.T) {
	a := &App{State: t.TempDir()}
	profiles := filepath.Join(t.TempDir(), "profiles")
	loads := 0
	load := func(path string) error {
		loads++
		policy, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, rule := range []string{"profile \"" + dockerAppArmorName() + "\"", "  mount,", "  pivot_root,", "deny @{PROC}/sysrq-trigger rwklx,", "deny /sys/kernel/security/** rwklx,"} {
			if !strings.Contains(string(policy), rule) {
				t.Fatalf("missing AppArmor rule %q", rule)
			}
		}
		if strings.Contains(string(policy), "ADC_PROFILE_NAME") || strings.Contains(string(policy), "deny mount,") {
			t.Fatal("unresolved or unusable AppArmor profile")
		}
		return nil
	}
	for _, options := range []string{`null`, `[]`, `["name=seccomp,profile=builtin","name=cgroupns"]`} {
		name, err := a.prepareAppArmor(options, profiles, load)
		if err != nil || name != "" || loads != 0 {
			t.Fatalf("AppArmor disabled: %q %v loads=%d", name, err, loads)
		}
	}
	options := `["name=apparmor","name=seccomp,profile=builtin"]`
	name, err := a.prepareAppArmor(options, profiles, load)
	if err != nil || name != dockerAppArmorName() || loads != 1 {
		t.Fatalf("AppArmor not loaded: %q %v loads=%d", name, err, loads)
	}
	if err := os.WriteFile(profiles, []byte(name+" (enforce)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.prepareAppArmor(options, profiles, load); err != nil || loads != 1 {
		t.Fatalf("already enforced profile reloaded: %v loads=%d", err, loads)
	}
	// Complain mode and kernel state lost after reboot both require loading.
	for _, contents := range []string{name + " (complain)\n", ""} {
		if err := os.WriteFile(profiles, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		failure := errors.New("fixture denied")
		if _, err := a.prepareAppArmor(options, profiles, func(string) error { return failure }); !errors.Is(err, failure) {
			t.Fatalf("profile loading failure ignored: %v", err)
		}
	}
	if _, err := a.prepareAppArmor("invalid JSON", profiles, load); err == nil {
		t.Fatal("invalid Docker security information ignored")
	}
}

func TestDockerAppArmorSyntax(t *testing.T) {
	parser, err := exec.LookPath("apparmor_parser")
	if err != nil {
		t.Skip("AppArmor parser is unavailable on this host")
	}
	path := filepath.Join(t.TempDir(), "adc.profile")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(dockerAppArmorTemplate, "ADC_PROFILE_NAME", dockerAppArmorName())), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(parser, "-Q", "-K", "-j", "1", path).CombinedOutput(); err != nil {
		t.Fatalf("invalid AppArmor policy: %v\n%s", err, output)
	}
}

func TestDockerSeccompPreparation(t *testing.T) {
	a := &App{State: t.TempDir(), Platform: filepath.Join(t.TempDir(), "platform")}
	if err := a.prepareDockerSecurity(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(a.dockerSeccompPath())
	if err != nil || !bytes.Equal(data, dockerSeccompProfile) {
		t.Fatalf("profile not prepared: %v", err)
	}
	args, err := a.DockerArgs(shared.Config{Name: "test", Port: 6768})
	if err != nil {
		t.Fatal(err)
	}
	if !shared.Contains(args, "seccomp="+a.dockerSeccompPath()) {
		t.Fatal("profile absent from Docker arguments/container identity")
	}
	for _, arg := range args {
		if strings.Contains(arg, "unconfined") || strings.Contains(arg, "SYS_ADMIN") || arg == "--privileged" {
			t.Fatalf("unsafe sandbox workaround: %s", arg)
		}
	}
}
