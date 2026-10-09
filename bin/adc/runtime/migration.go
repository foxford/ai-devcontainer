package runtime

import (
	"errors"
	"fmt"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
)

func (a *App) Migrate(args []string) error {
	if len(args) != 2 {
		return errors.New("adc migrate <project> <old-agent-home>")
	}

	return a.shared().Locked(func() error {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		backup, err := importAuth(args[1], home, filepath.Join(a.State, "backups"))
		if err != nil {
			return err
		}
		fmt.Println("Авторизации импортированы. Backup:", backup)
		return nil
	})
}

func (a *App) Restore(args []string) error {
	if len(args) != 1 {
		return errors.New("adc restore <backup-directory>")
	}
	return a.shared().Locked(func() error {
		path, e := shared.Abs(args[0])
		if e != nil {
			return e
		}
		root, e := shared.Abs(filepath.Join(a.State, "backups"))
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil || rel == ".." || len(rel) > 2 && rel[:3] == "../" {
			return errors.New("backup должен быть внутри ADC_HOME/backups")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		return restoreAuth(path, home)
	})
}
