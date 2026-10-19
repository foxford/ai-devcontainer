package runtime

import (
	"encoding/json"
	"errors"
	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type authBackup struct {
	Existed map[string]bool `json:"existed"`
}

func importAuth(source, home, backups string) (string, error) {
	files := map[string][]byte{}
	for _, rel := range authFiles {
		candidates := []string{rel, strings.TrimPrefix(rel, ".")}
		for _, candidate := range candidates {
			b, e := os.ReadFile(filepath.Join(source, filepath.FromSlash(candidate)))
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return "", e
			}
			files[rel] = b
			break
		}
	}
	if len(files) == 0 {
		return "", errors.New("в выбранном каталоге нет поддерживаемых файлов авторизации")
	}
	backup := filepath.Join(backups, time.Now().UTC().Format("20060102T150405.000000000"))
	manifest := authBackup{Existed: map[string]bool{}}
	// Complete the backup before changing any live credential.
	for rel := range files {
		b, e := os.ReadFile(filepath.Join(home, filepath.FromSlash(rel)))
		if os.IsNotExist(e) {
			manifest.Existed[rel] = false
			continue
		}
		if e != nil {
			return "", e
		}
		manifest.Existed[rel] = true
		if e = shared.AtomicWrite(filepath.Join(backup, filepath.FromSlash(rel)), b, 0600); e != nil {
			return "", e
		}
	}
	b, e := json.Marshal(manifest)
	if e != nil {
		return "", e
	}
	if e = shared.AtomicWrite(filepath.Join(backup, "auth-backup.json"), b, 0600); e != nil {
		return "", e
	}
	for rel, data := range files {
		if e = shared.AtomicWrite(filepath.Join(home, filepath.FromSlash(rel)), data, 0600); e != nil {
			return backup, e
		}
	}
	return backup, nil
}
func restoreAuth(backup, home string) error {
	b, e := os.ReadFile(filepath.Join(backup, "auth-backup.json"))
	if e != nil {
		return e
	}
	var manifest authBackup
	if e = json.Unmarshal(b, &manifest); e != nil {
		return e
	}
	data := map[string][]byte{}
	for rel, existed := range manifest.Existed {
		if !shared.Contains(authFiles, rel) {
			return errors.New("неизвестный путь в backup manifest")
		}
		if existed {
			b, e = os.ReadFile(filepath.Join(backup, filepath.FromSlash(rel)))
			if e != nil {
				return e
			}
			data[rel] = b
		}
	}
	for rel, existed := range manifest.Existed {
		target := filepath.Join(home, filepath.FromSlash(rel))
		if existed {
			if e = shared.AtomicWrite(target, data[rel], 0600); e != nil {
				return e
			}
		} else if e = os.Remove(target); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return nil
}
