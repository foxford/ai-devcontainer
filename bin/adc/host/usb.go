package host

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type usbDevice struct {
	ID, Path    string
	Bus, Number int
}

type usbNode struct {
	Mode         os.FileMode
	UID, GID     uint32
	Major, Minor uint64
}

func usbDockerArgs(c shared.Config) ([]string, error) {
	if err := c.ValidateUSBDevices(); err != nil {
		return nil, err
	}
	if len(c.USBDevices) == 0 {
		return nil, nil
	}
	if runtime.GOOS != "linux" {
		return nil, errors.New("USB требует запуска adc на Linux или внутри WSL с локальным Docker Engine")
	}
	if err := checkUSBDocker(realDocker().output, os.Getenv("DOCKER_HOST"), os.Getenv("DOCKER_CONTEXT")); err != nil {
		return nil, err
	}
	devices, err := resolveUSBDevices(c.USBDevices, "/sys/bus/usb/devices")
	if err != nil {
		return nil, err
	}
	uid, gid := os.Getuid(), os.Getgid()
	if uid == 0 {
		uid, gid = 1000, 1000
	}
	return usbDeviceArgs(devices, uint32(uid), uint32(gid), statUSBNode)
}

// Query the same endpoint as the Docker CLI: DOCKER_CONTEXT overrides DOCKER_HOST.
// USB addresses are local to a kernel and cannot be resolved for a remote daemon.
func checkUSBDocker(output func(...string) (string, error), host, context string) error {
	endpoint := host
	if context != "" || endpoint == "" {
		args := []string{"context", "inspect", "--format", "{{json .Endpoints.docker.Host}}"}
		if context != "" {
			args = append(args, context)
		}
		data, err := output(args...)
		if err != nil {
			return fmt.Errorf("USB: Docker context: %w", err)
		}
		if err := json.Unmarshal([]byte(data), &endpoint); err != nil {
			return fmt.Errorf("USB: адрес Docker context: %w", err)
		}
	}
	if !strings.HasPrefix(endpoint, "unix:///") {
		return errors.New("USB поддерживается только с локальным Docker Engine через Unix socket; запустите adc на Linux-хосте Docker")
	}
	data, err := output("info", "--format", "{{json .}}")
	if err != nil {
		return fmt.Errorf("USB: Docker info: %w", err)
	}
	var info struct {
		OSType, OperatingSystem string
		SecurityOptions         []string
	}
	if err := json.Unmarshal([]byte(data), &info); err != nil {
		return fmt.Errorf("USB: Docker info: %w", err)
	}
	if info.OSType != "linux" || strings.Contains(strings.ToLower(info.OperatingSystem), "docker desktop") {
		return errors.New("USB требует локального Linux Docker Engine; Docker Desktop пока не поддерживается")
	}
	for _, option := range info.SecurityOptions {
		name := strings.SplitN(option, ",", 2)[0]
		if name == "name=rootless" || name == "name=userns" {
			return errors.New("USB пока не поддерживает rootless Docker и userns-remap: UID/GID устройства должны совпадать на хосте и в runtime")
		}
	}
	return nil
}

func resolveUSBDevices(selectors []string, sysfs string) ([]usbDevice, error) {
	entries, err := os.ReadDir(sysfs)
	if err != nil {
		return nil, fmt.Errorf("USB: прочитать %s: %w", sysfs, err)
	}
	wanted := make(map[string]bool, len(selectors))
	for _, selector := range selectors {
		wanted[strings.ToLower(selector)] = true
	}
	matches := make(map[string][]usbDevice)
	for _, entry := range entries {
		dir := filepath.Join(sysfs, entry.Name())
		vendor, err := os.ReadFile(filepath.Join(dir, "idVendor"))
		if os.IsNotExist(err) { // Interface entries have no device identity.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("USB %s: %w", entry.Name(), err)
		}
		product, err := os.ReadFile(filepath.Join(dir, "idProduct"))
		if os.IsNotExist(err) { // A device may disappear during enumeration.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("USB %s: %w", entry.Name(), err)
		}
		id := strings.ToLower(strings.TrimSpace(string(vendor)) + ":" + strings.TrimSpace(string(product)))
		if !wanted[id] {
			continue
		}
		readNumber := func(name string, max int) (int, error) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return 0, fmt.Errorf("USB %s: %s: %w", id, name, err)
			}
			n, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || n < 1 || n > max {
				return 0, fmt.Errorf("USB %s: некорректный %s", id, name)
			}
			return n, nil
		}
		bus, err := readNumber("busnum", 999)
		if err != nil {
			return nil, err
		}
		number, err := readNumber("devnum", 127)
		if err != nil {
			return nil, err
		}
		matches[id] = append(matches[id], usbDevice{ID: id, Path: fmt.Sprintf("/dev/bus/usb/%03d/%03d", bus, number), Bus: bus, Number: number})
	}
	var devices []usbDevice
	for _, selector := range selectors {
		id := strings.ToLower(selector)
		if len(matches[id]) == 0 {
			return nil, fmt.Errorf("USB %s не найден: подключите устройство к Linux/WSL или уберите выбор через adc workspace config --usb-clear", id)
		}
		if len(matches[id]) != 1 {
			return nil, fmt.Errorf("USB %s: найдено устройств: %d; выбор неоднозначен, оставьте подключённым одно устройство с этим VID:PID", id, len(matches[id]))
		}
		devices = append(devices, matches[id][0])
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })
	return devices, nil
}

func usbDeviceArgs(devices []usbDevice, uid, gid uint32, stat func(string) (usbNode, error)) ([]string, error) {
	nodes := make([]usbNode, len(devices))
	groups := map[uint32]bool{gid: true}
	for i, device := range devices {
		node, err := stat(device.Path)
		if err != nil {
			return nil, fmt.Errorf("USB %s: %s: %w; проверьте устройство на хосте и повторите adc workspace start", device.ID, device.Path, err)
		}
		if node.Mode&os.ModeCharDevice == 0 || node.Major != 189 || node.Minor != uint64((device.Bus-1)*128+device.Number-1) {
			return nil, fmt.Errorf("USB %s: %s не соответствует USB-устройству из sysfs", device.ID, device.Path)
		}
		nodes[i] = node
		if node.UID != uid && node.GID != 0 && node.GID != ^uint32(0) && node.Mode.Perm()&0060 == 0060 {
			groups[node.GID] = true
		}
	}
	var args []string
	for i, device := range devices {
		node := nodes[i]
		bits := node.Mode.Perm()
		switch {
		case node.UID == uid:
			bits >>= 6
		case groups[node.GID]:
			bits >>= 3
		}
		if bits&6 != 6 {
			return nil, fmt.Errorf("USB %s: пользователю node (UID %d) нужен доступ rw к %s; настройте на хосте правило udev с MODE=\"0660\" и GROUP вашего пользователя (ненулевой GID), затем переподключите устройство; см. docs/workspace.md#usb-устройства", device.ID, uid, device.Path)
		}
		args = append(args, "--device", device.Path+":"+device.Path+":rw")
	}
	var gids []uint32
	for group := range groups {
		if group != gid {
			gids = append(gids, group)
		}
	}
	sort.Slice(gids, func(i, j int) bool { return gids[i] < gids[j] })
	if len(gids) > 0 {
		values := make([]string, len(gids))
		for i, group := range gids {
			values[i] = strconv.FormatUint(uint64(group), 10)
		}
		args = append(args, "-e", "ADC_USB_GIDS="+strings.Join(values, ","))
	}
	// Docker copies device-node ownership and mode at creation. A host udev
	// permission change must recreate the container even when its USB path stays.
	metadata, err := json.Marshal(nodes)
	if err != nil {
		return nil, err
	}
	args = append(args, "--label", fmt.Sprintf("adc.usb-state=%x", sha256.Sum256(metadata)))
	return args, nil
}
