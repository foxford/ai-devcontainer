package host

import (
	"fmt"
	"os"
	"path/filepath"
)

// ApplyDelivery never starts or replaces a workspace. Its next normal start
// delivers the selected source if no container is currently running.
func (a *App) ApplyDelivery() error {
	if err := a.shared().RequireHost(); err != nil {
		return err
	}
	return a.shared().LockedFile("lifecycle.lockfile", func() error {
		return a.applyDelivery(realDocker(), func() error {
			return a.Exec("", []string{"adc", "sync", "--all"}, false)
		})
	})
}

func (a *App) applyDelivery(docker dockerCommands, apply func() error) error {
	if _, err := os.Stat(filepath.Join(a.State, "workspace.json")); os.IsNotExist(err) {
		fmt.Println("Доставка сохранена. Она применится при первом запуске workspace.")
		return nil
	} else if err != nil {
		return err
	}
	c, err := a.shared().Load()
	if err != nil {
		return err
	}
	container, err := docker.container(a.shared().Container(c))
	if err != nil {
		return err
	}
	if container == nil || container.State.Status != "running" {
		fmt.Println("Доставка сохранена. Она применится при следующем запуске workspace или агента.")
		return nil
	}
	return apply()
}
