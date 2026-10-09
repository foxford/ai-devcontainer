package host

import (
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type App shared.App

func (a *App) shared() *shared.App { return (*shared.App)(a) }
