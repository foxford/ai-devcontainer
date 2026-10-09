// Package legacy describes immutable snapshots of the v0 devcontainer layout.
package legacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const Version = 1

type Mount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Resource    string `json:"resource,omitempty"`
	Skip        string `json:"skip,omitempty"`
}
type Container struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Project     string  `json:"project"`
	ProjectPath string  `json:"projectPath"`
	RuntimePath string  `json:"runtimePath"`
	Mounts      []Mount `json:"mounts"`
}
type Resource struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Source string `json:"source"`
}
type Plan struct {
	Version    int         `json:"version"`
	Containers []Container `json:"containers"`
	Resources  []Resource  `json:"resources"`
}
type Archive struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type Backup struct {
	Version  int                `json:"version"`
	Created  string             `json:"created"`
	Plan     Plan               `json:"plan"`
	Archives map[string]Archive `json:"archives"`
}

func Fingerprint(b Backup) string {
	data, _ := json.Marshal(b)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ReadJSON(file string, value any) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 8<<20))
	d.DisallowUnknownFields()
	if err = d.Decode(value); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("лишние данные в migration manifest")
	}
	return nil
}

func (p Plan) Validate() error {
	if p.Version != Version || len(p.Containers) == 0 {
		return errors.New("неподдерживаемый или пустой план миграции")
	}
	resources := map[string]bool{}
	for _, r := range p.Resources {
		if !SafeID(r.ID) || resources[r.ID] || r.Source == "" || strings.ContainsAny(r.Source, ",\r\n") || (r.Type != "bind" && r.Type != "volume") {
			return errors.New("некорректный ресурс миграции")
		}
		resources[r.ID] = true
	}
	projects, containers := map[string]bool{}, map[string]bool{}
	for _, c := range p.Containers {
		if !SafeID(c.Project) || projects[c.Project] || !SafeID(c.ID) || containers[c.ID] || c.ProjectPath == "" || !strings.HasPrefix(c.RuntimePath, "/") {
			return errors.New("некорректный или повторный проект миграции")
		}
		projects[c.Project], containers[c.ID] = true, true
		for _, m := range c.Mounts {
			if m.Resource != "" && !resources[m.Resource] {
				return errors.New("неизвестный ресурс маунта")
			}
			if m.Resource == "" && m.Skip == "" {
				return errors.New("маунт не включён в backup и не имеет причины пропуска")
			}
			if !strings.HasPrefix(m.Destination, "/") {
				return errors.New("неверный путь маунта")
			}
			if m.Resource != "" {
				for _, r := range p.Resources {
					if r.ID == m.Resource && (r.Type != m.Type || r.Source != m.Source) {
						return errors.New("маунт не соответствует ресурсу")
					}
				}
			}
		}
	}
	return nil
}
func SafeID(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// Verify checks every archive, including repository and DinD snapshots that the
// importer does not activate. No partially completed backup can be imported.
func Verify(dir string) (Backup, error) {
	var b Backup
	if err := ReadJSON(filepath.Join(dir, "backup.json"), &b); err != nil {
		return b, err
	}
	if b.Version != Version {
		return b, errors.New("неподдерживаемый backup")
	}
	if err := b.Plan.Validate(); err != nil {
		return b, err
	}
	keys := []string{}
	for _, r := range b.Plan.Resources {
		keys = append(keys, r.ID)
	}
	for _, c := range b.Plan.Containers {
		keys = append(keys, "container-"+c.ID)
		keys = append(keys, "metadata-"+c.ID)
	}
	if len(keys) != len(b.Archives) {
		return b, errors.New("неполный набор архивов")
	}
	for _, key := range keys {
		a, ok := b.Archives[key]
		extension := ".tar"
		if strings.HasPrefix(key, "metadata-") {
			extension = ".json"
		}
		if !ok || a.File != key+extension || len(a.SHA256) != 64 || a.Bytes < 0 {
			return b, errors.New("некорректный архив в manifest")
		}
		file := filepath.Join(dir, a.File)
		info, err := os.Lstat(file)
		if err != nil {
			return b, err
		}
		if !info.Mode().IsRegular() || info.Size() != a.Bytes {
			return b, fmt.Errorf("архив %s изменён или не является файлом", a.File)
		}
		f, err := os.Open(file)
		if err != nil {
			return b, err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		closeErr := f.Close()
		if err != nil {
			return b, err
		}
		if closeErr != nil {
			return b, closeErr
		}
		if hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
			return b, fmt.Errorf("SHA-256 не совпадает: %s", a.File)
		}
	}
	return b, nil
}

// CleanName checks names even for entries the importer does not select. It never
// extracts symlinks or hardlinks, so an archive cannot redirect a later write.
func CleanName(name string) (string, error) {
	if strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") {
		return "", errors.New("небезопасный путь в архиве")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", errors.New("выход из архива")
		}
	}
	return path.Clean(name), nil
}
