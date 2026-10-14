package shared

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DeliverySource selects downloaded delivery without replacing the platform
// checkout. The base revision makes a later full platform update take priority.
type DeliverySource struct {
	Revision         string `json:"revision"`
	PlatformRevision string `json:"platformRevision"`
}

func (a *App) PlatformRevision() (string, error) {
	revision, err := Output("", "git", "-C", a.Platform, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать ревизию платформы: %w", err)
	}
	if !validUpdateRevision(revision) {
		return "", fmt.Errorf("некорректная ревизия платформы")
	}
	return revision, nil
}

// RefreshDelivery falls back to the platform until a delivery-only update is
// installed. Old release directories remain valid targets for existing links.
func (a *App) RefreshDelivery() error {
	a.DeliveryRoot = ""
	if a.State == "" || !filepath.IsAbs(a.State) {
		return nil
	}
	marker := filepath.Join(a.State, "delivery", "current.json")
	data, err := os.ReadFile(marker)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("источник доставки %s: %w", marker, err)
	}
	var source DeliverySource
	if err := json.Unmarshal(data, &source); err != nil {
		return fmt.Errorf("источник доставки %s: %w", marker, err)
	}
	if !validUpdateRevision(source.Revision) || !validUpdateRevision(source.PlatformRevision) {
		return fmt.Errorf("источник доставки %s содержит некорректные ревизии", marker)
	}
	base, err := a.PlatformRevision()
	if err != nil {
		return err
	}
	if base != source.PlatformRevision {
		return nil
	}
	root := filepath.Join(a.State, "delivery", source.Revision, "delivery")
	if err := validateDeliveryDirectories(filepath.Dir(root)); err != nil {
		return err
	}
	a.DeliveryRoot = root
	return nil
}

func validateDeliveryDirectories(release string) error {
	for _, path := range []string{release, filepath.Join(release, "delivery")} {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("источник доставки %s: %w", path, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("источник доставки %s должен быть каталогом, а не ссылкой", path)
		}
	}
	return nil
}
