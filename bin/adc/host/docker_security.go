package host

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// Docker's default profile with the additional calls needed by an unprivileged
// nested bubblewrap sandbox. It does not grant host capabilities.
//
//go:embed security/seccomp.json
var dockerSeccompProfile []byte

//go:embed security/apparmor.profile
var dockerAppArmorTemplate string

func dockerAppArmorName() string {
	sum := sha256.Sum256([]byte(dockerAppArmorTemplate))
	return fmt.Sprintf("adc-runtime-%x", sum)
}

func (a *App) prepareDockerAppArmor() (string, error) {
	options, err := shared.Output("", "docker", "info", "--format", "{{json .SecurityOptions}}")
	if err != nil {
		return "", err
	}
	return a.prepareAppArmor(options, "/sys/kernel/security/apparmor/profiles", func(path string) error {
		enabled, err := os.ReadFile("/sys/module/apparmor/parameters/enabled")
		if runtime.GOOS != "linux" || err != nil || strings.TrimSpace(string(enabled)) != "Y" {
			return fmt.Errorf("Docker использует AppArmor; загрузка профиля требует локального Linux Docker host с AppArmor")
		}
		parser, err := exec.LookPath("apparmor_parser")
		if err != nil {
			return fmt.Errorf("для Docker AppArmor нужен установленный на хосте apparmor_parser: %w", err)
		}
		fmt.Println("Загрузка профиля AppArmor ADC; могут потребоваться права sudo")
		return adminRun(parser, "-r", "-K", path)
	})
}

func (a *App) prepareAppArmor(options, profilesPath string, load func(string) error) (string, error) {
	var securityOptions []string
	if err := json.Unmarshal([]byte(options), &securityOptions); err != nil {
		return "", fmt.Errorf("Docker SecurityOptions: %w", err)
	}
	enabled := false
	for _, option := range securityOptions {
		if strings.SplitN(option, ",", 2)[0] == "name=apparmor" {
			enabled = true
		}
	}
	if !enabled {
		return "", nil
	}
	name := dockerAppArmorName()
	path := filepath.Join(a.State, "security", name+".profile")
	policy := strings.ReplaceAll(dockerAppArmorTemplate, "ADC_PROFILE_NAME", name)
	if err := shared.AtomicWrite(path, []byte(policy), 0600); err != nil {
		return "", err
	}
	// Consult kernel state rather than a disk marker: profiles disappear after
	// reboot. A content-addressed name never replaces another runtime's policy.
	profiles, _ := os.ReadFile(profilesPath)
	for _, profile := range strings.Split(string(profiles), "\n") {
		if profile == name+" (enforce)" {
			return name, nil
		}
	}
	if err := load(path); err != nil {
		return "", fmt.Errorf("загрузка AppArmor %s: %w", name, err)
	}
	return name, nil
}

func (a *App) dockerSeccompPath() string {
	sum := sha256.Sum256(dockerSeccompProfile)
	return filepath.Join(a.State, "security", fmt.Sprintf("seccomp-%x.json", sum))
}

func (a *App) prepareDockerSecurity() error {
	// The content-addressed path participates in DockerArgs/container-key, so a
	// profile update recreates the container even when the image is unchanged.
	return shared.AtomicWrite(a.dockerSeccompPath(), dockerSeccompProfile, 0600)
}
