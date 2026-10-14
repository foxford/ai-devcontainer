package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// UpdateDelivery installs only delivery files. The callback applies them in the
// runtime (or defers application for a stopped workspace). Keep the source lock
// until application finishes so a full update cannot replace its base midway.
func (a *App) UpdateDelivery(ctx context.Context, apply func() error) (changes []DeliveryChange, err error) {
	if !filepath.IsAbs(a.State) {
		return nil, fmt.Errorf("ADC_HOME должен быть абсолютным путём для обновления доставки")
	}
	err = a.LockedFile("update.lockfile", func() error {
		if err := a.RefreshDelivery(); err != nil {
			return err
		}
		before, err := a.DeliveryVersions()
		if err != nil {
			return err
		}
		base, err := a.PlatformRevision()
		if err != nil {
			return err
		}
		source, err := a.updateSource(ctx)
		if err != nil {
			return err
		}
		directory := filepath.Join(a.State, "cache", "updates")
		// Share the checker's repository lock: fetch and archive must see the
		// same objects even while a shell is checking versions in parallel.
		cacheLock := &App{State: directory}
		var next updateCache
		err = cacheLock.LockedFile("check.lockfile", func() error {
			cached, file := a.readUpdateCache(source)
			cached.RemoteRevision = "" // Explicit installation always fetches.
			var err error
			next, err = a.fetchUpdate(ctx, source, cached, directory)
			if err != nil {
				return err
			}
			for key, unit := range next.Versions {
				if current, ok := before[key]; ok && compareDeliveryVersions(unit.Version, current.Version) < 0 {
					return fmt.Errorf("опубликованная версия %s (%s) старее установленной (%s); доставка не изменена", key, unit.Version, current.Version)
				}
			}
			if err = a.prepareDelivery(ctx, directory, next.RemoteRevision); err != nil {
				return err
			}
			next.CheckedAt, next.AttemptedAt, next.Error = time.Now().UTC(), time.Now().UTC(), ""
			data, err := json.MarshalIndent(next, "", "  ")
			if err != nil {
				return err
			}
			return AtomicWrite(file, append(data, '\n'), 0600)
		})
		if err != nil {
			return err
		}
		err = a.Locked(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			currentBase, err := a.PlatformRevision()
			if err != nil || currentBase != base {
				return fmt.Errorf("платформа изменилась во время загрузки; повторите adc update --delivery")
			}
			data, err := json.MarshalIndent(DeliverySource{Revision: next.RemoteRevision, PlatformRevision: base}, "", "  ")
			if err != nil {
				return err
			}
			if err = AtomicWrite(filepath.Join(a.State, "delivery", "current.json"), append(data, '\n'), 0600); err != nil {
				return err
			}
			return a.RefreshDelivery()
		})
		if err != nil {
			return err
		}
		changes = DiffDeliveryVersions(before, next.Versions)
		if err := apply(); err != nil {
			return fmt.Errorf("доставка загружена, но применена не во всех проектах: %w; исправьте причину и повторите adc update --delivery", err)
		}
		return nil
	})
	return changes, err
}

func (a *App) prepareDelivery(ctx context.Context, cache, revision string) error {
	root := filepath.Join(a.State, "delivery")
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	destination := filepath.Join(root, revision)
	if _, err := os.Stat(destination); err == nil {
		return validateDeliveryRelease(destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	staging, err := os.MkdirTemp(root, ".download-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	cmd := updateGitCommand(ctx, cache, "--git-dir", filepath.Join(cache, "remote.git"), "archive", "--format=tar", revision, "--", "delivery")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	extractErr := extractDeliveryFiles(ctx, staging, stdout, false)
	if extractErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if extractErr != nil {
		return extractErr
	}
	if waitErr != nil {
		return fmt.Errorf("не удалось получить файлы доставки: %w", updateCheckError(ctx, waitErr))
	}
	if err = validateDeliveryRelease(staging); err != nil {
		return err
	}
	if err = os.Chmod(staging, 0755); err != nil {
		return err
	}
	return os.Rename(staging, destination)
}

func validateDeliveryRelease(root string) error {
	if err := validateDeliveryDirectories(root); err != nil {
		return err
	}
	candidate := &App{Platform: root}
	if _, err := candidate.DeliveryCatalog(); err != nil {
		return fmt.Errorf("доставка несовместима с установленным ADC: %w; обновите ADC на хосте командой adc update", err)
	}
	_, err := candidate.DeliveryVersions()
	return err
}
