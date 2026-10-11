package host

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"path/filepath"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

// Docker's default profile with the additional calls needed by an unprivileged
// nested bubblewrap sandbox. It does not grant host capabilities.
//
//go:embed security/seccomp.json
var dockerSeccompProfile []byte

func (a *App) dockerSeccompPath() string {
	sum := sha256.Sum256(dockerSeccompProfile)
	return filepath.Join(a.State, "security", fmt.Sprintf("seccomp-%x.json", sum))
}

func (a *App) prepareDockerSecurity() error {
	// The content-addressed path participates in DockerArgs/container-key, so a
	// profile update recreates the container even when the image is unchanged.
	return shared.AtomicWrite(a.dockerSeccompPath(), dockerSeccompProfile, 0600)
}
