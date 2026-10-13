package shared

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Each unit owns its release version. Content changes become visible to users
// when its maintainer increments this version; files are never hashed here.
type DeliveryUnit struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type DeliveryVersions map[string]DeliveryUnit

type DeliveryChange struct {
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Action        string `json:"action"`
	BeforeVersion string `json:"before_version,omitempty"`
	AfterVersion  string `json:"after_version,omitempty"`
}

func IsDeliveryMetadata(path string) bool {
	name := filepath.Base(path)
	return name == "meta.json" || strings.HasSuffix(name, ".meta.json")
}

func (a *App) DeliveryVersions() (DeliveryVersions, error) {
	units := DeliveryVersions{}
	err := filepath.WalkDir(a.DeliveryPath(""), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !IsDeliveryMetadata(path) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("метаданные доставки должны быть обычным файлом: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var unit DeliveryUnit
		if err := json.Unmarshal(data, &unit); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !Contains([]string{"skill", "mcp", "bundle"}, unit.Kind) || !ValidID.MatchString(unit.Name) {
			return fmt.Errorf("неверные kind/name в %s", path)
		}
		if _, err := parseDeliveryVersion(unit.Version); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		key := unit.Kind + "/" + unit.Name
		if _, exists := units[key]; exists {
			return fmt.Errorf("повтор метаданных доставки: %s", key)
		}
		units[key] = unit
		return nil
	})
	if err == nil && len(units) == 0 {
		err = fmt.Errorf("метаданные версий доставки отсутствуют")
	}
	return units, err
}

// Only a higher published version (or a newly published unit) is an update.
// Equal versions, older remote versions and removed units produce no notice.
func DiffDeliveryVersions(local, published DeliveryVersions) []DeliveryChange {
	changes := []DeliveryChange{}
	for key, next := range published {
		if _, err := parseDeliveryVersion(next.Version); err != nil {
			continue
		}
		current, exists := local[key]
		if exists && compareDeliveryVersions(next.Version, current.Version) <= 0 {
			continue
		}
		action := "updated"
		if !exists {
			action = "added"
		}
		changes = append(changes, DeliveryChange{Kind: next.Kind, Name: next.Name, Action: action, BeforeVersion: current.Version, AfterVersion: next.Version})
	}
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Kind+"/"+changes[i].Name < changes[j].Kind+"/"+changes[j].Name
	})
	return changes
}

var deliveryVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

type deliveryVersion struct {
	numbers [3]uint64
	pre     []string
}

func parseDeliveryVersion(value string) (deliveryVersion, error) {
	var version deliveryVersion
	parts := deliveryVersionPattern.FindStringSubmatch(value)
	if parts == nil {
		return version, fmt.Errorf("версия %q должна соответствовать SemVer, например 1.0.0", value)
	}
	for index := range version.numbers {
		number, err := strconv.ParseUint(parts[index+1], 10, 64)
		if err != nil {
			return version, fmt.Errorf("неверная версия %q", value)
		}
		version.numbers[index] = number
	}
	if parts[4] != "" {
		version.pre = strings.Split(parts[4], ".")
		for _, part := range version.pre {
			if numericVersionPart(part) && len(part) > 1 && part[0] == '0' {
				return version, fmt.Errorf("ведущий ноль в prerelease версии %q", value)
			}
		}
	}
	return version, nil
}

func numericVersionPart(part string) bool {
	return strings.Trim(part, "0123456789") == ""
}

func compareDeliveryVersions(left, right string) int {
	a, errA := parseDeliveryVersion(left)
	b, errB := parseDeliveryVersion(right)
	if errA != nil || errB != nil {
		return 0
	}
	for index, number := range a.numbers {
		if number < b.numbers[index] {
			return -1
		}
		if number > b.numbers[index] {
			return 1
		}
	}
	if len(a.pre) == 0 && len(b.pre) > 0 {
		return 1
	}
	if len(a.pre) > 0 && len(b.pre) == 0 {
		return -1
	}
	for index := 0; index < len(a.pre) && index < len(b.pre); index++ {
		x, y := a.pre[index], b.pre[index]
		if x == y {
			continue
		}
		xNumber, yNumber := numericVersionPart(x), numericVersionPart(y)
		if xNumber != yNumber {
			if xNumber {
				return -1
			}
			return 1
		}
		if xNumber && len(x) != len(y) {
			if len(x) < len(y) {
				return -1
			}
			return 1
		}
		return strings.Compare(x, y)
	}
	if len(a.pre) < len(b.pre) {
		return -1
	}
	if len(a.pre) > len(b.pre) {
		return 1
	}
	return 0
}
