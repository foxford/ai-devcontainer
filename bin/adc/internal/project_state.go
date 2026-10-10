package shared

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const ProjectStateDir = ".adc"
const LegacyProjectStateDir = ".ai-devcontainer"

// MigrateProjectState publishes a verified private copy. The original remains
// available until the developer explicitly cleans up the legacy delivery.
func MigrateProjectState(root string) error {
	old := filepath.Join(root, LegacyProjectStateDir)
	next := filepath.Join(root, ProjectStateDir)
	for _, name := range []string{old, next} {
		info, err := os.Lstat(name)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && !info.IsDir() {
			return fmt.Errorf("%s должен быть обычным каталогом", name)
		}
	}
	if _, err := os.Lstat(old); os.IsNotExist(err) {
		return nil
	}
	if _, err := os.Lstat(next); err == nil {
		info, err := os.Lstat(filepath.Join(next, "legacy-state-imported"))
		if err == nil && info.Mode().IsRegular() {
			return nil
		}
		return errors.New("в проекте уже есть .adc и .ai-devcontainer; автоматическое объединение состояния отменено")
	}
	if err := filepath.WalkDir(old, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("ссылка в старом состоянии ADC требует проверки: %s", name)
		}
		return nil
	}); err != nil {
		return err
	}
	digest, err := TreeDigest(old)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, ".adc-state-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	copy := filepath.Join(stage, "state")
	if err = CopyPrivateTree(old, copy); err != nil {
		return err
	}
	copied, err := TreeDigest(copy)
	if err != nil || copied != digest {
		return errors.New("копия состояния ADC не прошла проверку SHA-256")
	}
	current, err := TreeDigest(old)
	if err != nil || current != digest {
		return errors.New("старое состояние ADC изменилось во время копирования")
	}
	if err = AtomicWrite(filepath.Join(copy, "legacy-state-imported"), []byte(digest+"\n"), 0600); err != nil {
		return err
	}
	if _, err = os.Lstat(next); !os.IsNotExist(err) {
		return errors.New(".adc появился во время переноса состояния")
	}
	return os.Rename(copy, next)
}
