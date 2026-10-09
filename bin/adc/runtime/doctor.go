package runtime

import (
	"errors"
	"fmt"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
)

func (a *App) Doctor() error {
	fmt.Println("Платформа:", a.Platform)
	fmt.Println("Состояние:", a.State)
	c, e := a.shared().Load()
	if e != nil {
		return e
	}
	if _, e = os.Stat("/tmp/adc-ready"); e != nil {
		return errors.New("runtime-init ещё не завершён")
	}
	for _, p := range c.Projects {
		if _, e = os.Stat(p.RuntimePath); e != nil {
			return e
		}
	}
	return shared.Run("", "orca", "--version")
}
