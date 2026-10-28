package host

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func usbSysfsFixture(t *testing.T, root, name, id, bus, number string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	vendor, product, _ := strings.Cut(id, ":")
	for key, value := range map[string]string{"idVendor": vendor, "idProduct": product, "busnum": bus, "devnum": number} {
		if err := os.WriteFile(filepath.Join(dir, key), []byte(value+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUSBResolvesSelectedDevicesAndReplug(t *testing.T) {
	root := t.TempDir()
	usbSysfsFixture(t, root, "1-1", "2104:0313", "1", "2")
	usbSysfsFixture(t, root, "2-1", "abcd:ef01", "2", "3")
	usbSysfsFixture(t, root, "usb1", "1d6b:0002", "1", "1")
	if err := os.Mkdir(filepath.Join(root, "1-1:1.0"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveUSBDevices([]string{"ABCD:EF01", "2104:0313"}, root)
	want := []usbDevice{{ID: "2104:0313", Path: "/dev/bus/usb/001/002", Bus: 1, Number: 2}, {ID: "abcd:ef01", Path: "/dev/bus/usb/002/003", Bus: 2, Number: 3}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("devices=%+v err=%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(root, "1-1", "devnum"), []byte("5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err = resolveUSBDevices([]string{"2104:0313"}, root)
	if err != nil || len(got) != 1 || got[0].Path != "/dev/bus/usb/001/005" {
		t.Fatalf("replug=%+v err=%v", got, err)
	}
}

func TestUSBResolutionRejectsMissingAmbiguousAndMalformed(t *testing.T) {
	for _, tc := range []struct{ name, bus, number, want string }{
		{"missing", "1", "2", "не найден"},
		{"ambiguous", "1", "2", "неоднозначен"},
		{"bad bus", "../1", "2", "busnum"},
		{"zero bus", "0", "2", "busnum"},
		{"bad number", "1", "128", "devnum"},
		{"negative number", "1", "-1", "devnum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.name != "missing" {
				usbSysfsFixture(t, root, "1-1", "2104:0313", tc.bus, tc.number)
			}
			if tc.name == "ambiguous" {
				usbSysfsFixture(t, root, "1-2", "2104:0313", "1", "3")
			}
			if _, err := resolveUSBDevices([]string{"2104:0313"}, root); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want=%s", err, tc.want)
			}
		})
	}
	if _, err := resolveUSBDevices([]string{"2104:0313"}, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing sysfs accepted")
	}
}

func TestUSBDevicePermissionsAndScope(t *testing.T) {
	device := usbDevice{ID: "2104:0313", Path: "/dev/bus/usb/001/002", Bus: 1, Number: 2}
	for _, tc := range []struct {
		name      string
		mode      os.FileMode
		uid, gid  uint32
		group     string
		wantError bool
	}{
		{"host owner", 0600, 1000, 0, "", false},
		{"primary group", 0660, 0, 1000, "", false},
		{"supplementary group", 0660, 0, 45678, "ADC_USB_GIDS=45678", false},
		{"world access", 0666, 0, 0, "", false},
		{"root group", 0660, 0, 0, "", true},
		{"read only", 0644, 0, 1000, "", true},
		{"owner overrides group and other", 0466, 1000, 45678, "", true},
		{"primary group overrides other", 0646, 0, 1000, "", true},
		{"invalid group", 0660, 0, ^uint32(0), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, err := usbDeviceArgs([]usbDevice{device}, 1000, 1000, func(path string) (usbNode, error) {
				if path != device.Path {
					t.Fatal("probed unselected device", path)
				}
				return usbNode{Mode: os.ModeDevice | os.ModeCharDevice | tc.mode, UID: tc.uid, GID: tc.gid, Major: 189, Minor: 1}, nil
			})
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "udev") || len(args) != 0 {
					t.Fatalf("args=%v err=%v", args, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "--device /dev/bus/usb/001/002:/dev/bus/usb/001/002:rw") {
				t.Fatal(args)
			}
			for _, bad := range []string{"--privileged", "--mount", "--device-cgroup-rule", "--group-add"} {
				if strings.Contains(joined, bad) {
					t.Fatal("broad access", args)
				}
			}
			if tc.group == "" && strings.Contains(joined, "ADC_USB_GIDS") || tc.group != "" && !strings.Contains(joined, tc.group) {
				t.Fatal(args)
			}
		})
	}
}

func TestUSBRejectsChangedOrNonDeviceNodes(t *testing.T) {
	device := usbDevice{ID: "2104:0313", Path: "/dev/bus/usb/003/002", Bus: 3, Number: 2}
	valid := usbNode{Mode: os.ModeDevice | os.ModeCharDevice | 0666, Major: 189, Minor: 257}
	for _, kind := range []string{"file", "symlink", "wrong major", "wrong minor", "disconnected"} {
		t.Run(kind, func(t *testing.T) {
			node := valid
			var statErr error
			switch kind {
			case "file":
				node.Mode = 0666
			case "symlink":
				node.Mode = os.ModeSymlink | 0777
			case "wrong major":
				node.Major = 1
			case "wrong minor":
				node.Minor = 1
			case "disconnected":
				statErr = os.ErrNotExist
			}
			if args, err := usbDeviceArgs([]usbDevice{device}, 1000, 1000, func(string) (usbNode, error) { return node, statErr }); err == nil || len(args) > 0 {
				t.Fatalf("args=%v err=%v", args, err)
			}
		})
	}
	if _, err := usbDeviceArgs([]usbDevice{device}, 1000, 1000, func(string) (usbNode, error) { return valid, nil }); err != nil {
		t.Fatal(err)
	}
}

func TestUSBPermissionChangeAffectsContainerArguments(t *testing.T) {
	device := usbDevice{ID: "2104:0313", Path: "/dev/bus/usb/001/002", Bus: 1, Number: 2}
	args := func(mode os.FileMode) []string {
		t.Helper()
		result, err := usbDeviceArgs([]usbDevice{device}, 1000, 1000, func(string) (usbNode, error) {
			return usbNode{Mode: os.ModeDevice | os.ModeCharDevice | mode, UID: 1000, Major: 189, Minor: 1}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if reflect.DeepEqual(args(0600), args(0660)) {
		t.Fatal("host mode change would reuse stale container device permissions")
	}
	if !reflect.DeepEqual(args(0600), args(0600)) {
		t.Fatal("unchanged device would recreate container")
	}
}

func TestUSBGroupCollectionChecksAllSelectedDevices(t *testing.T) {
	devices := []usbDevice{{ID: "2104:0313", Path: "/dev/bus/usb/001/002", Bus: 1, Number: 2}, {ID: "abcd:ef01", Path: "/dev/bus/usb/001/003", Bus: 1, Number: 3}}
	args, err := usbDeviceArgs(devices, 1000, 1000, func(path string) (usbNode, error) {
		node := usbNode{Mode: os.ModeDevice | os.ModeCharDevice | 0660, GID: 45678, Major: 189, Minor: 1}
		if path == devices[1].Path {
			node.Minor = 2
		}
		return node, nil
	})
	if err != nil || strings.Count(strings.Join(args, " "), "ADC_USB_GIDS=45678") != 1 {
		t.Fatalf("%v %v", args, err)
	}
	// Joining a group for one device can suppress the other-bits permission on
	// another device. Check the final membership set, not each grant separately.
	_, err = usbDeviceArgs(devices, 1000, 1000, func(path string) (usbNode, error) {
		node := usbNode{Mode: os.ModeDevice | os.ModeCharDevice | 0660, GID: 45678, Major: 189, Minor: 1}
		if path == devices[1].Path {
			node.Minor = 2
			node.Mode = os.ModeDevice | os.ModeCharDevice | 0606
		}
		return node, nil
	})
	if err == nil {
		t.Fatal("supplementary group unexpectedly removed access without a preflight error")
	}
}

func TestUSBDockerRejectsUnsupportedDaemonsAndHonorsContext(t *testing.T) {
	for _, tc := range []struct{ name, host, context, endpoint, info, want string }{
		{"default", "", "", `"unix:///var/run/docker.sock"`, `{"OSType":"linux","OperatingSystem":"Ubuntu","SecurityOptions":["name=seccomp,profile=builtin"]}`, ""},
		{"remote env", "ssh://remote", "", "", "", "локальным"},
		{"remote context", "", "remote", `"ssh://remote"`, "", "локальным"},
		{"context overrides host", "ssh://remote", "local", `"unix:///var/run/docker.sock"`, `{"OSType":"linux"}`, ""},
		{"host overrides default context", "unix:///var/run/docker.sock", "", "", `{"OSType":"linux"}`, ""},
		{"desktop", "unix:///var/run/docker.sock", "", "", `{"OSType":"linux","OperatingSystem":"Docker Desktop"}`, "Docker Desktop"},
		{"windows", "unix:///var/run/docker.sock", "", "", `{"OSType":"windows"}`, "Linux"},
		{"rootless", "unix:///run/user/1000/docker.sock", "", "", `{"OSType":"linux","SecurityOptions":["name=rootless"]}`, "rootless"},
		{"userns", "unix:///var/run/docker.sock", "", "", `{"OSType":"linux","SecurityOptions":["name=userns"]}`, "userns-remap"},
		{"invalid info", "unix:///var/run/docker.sock", "", "", "broken", "Docker info"},
		{"invalid context", "", "", "broken", "", "Docker context"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls [][]string
			err := checkUSBDocker(func(args ...string) (string, error) {
				calls = append(calls, args)
				switch args[0] {
				case "context":
					if tc.context != "" && args[len(args)-1] != tc.context {
						t.Fatal("wrong context", args)
					}
					return tc.endpoint, nil
				case "info":
					return tc.info, nil
				default:
					t.Fatal("unexpected Docker operation", args)
					return "", nil
				}
			}, tc.host, tc.context)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("error=%v want=%q", err, tc.want)
			}
			if tc.host != "" && tc.context == "" {
				for _, call := range calls {
					if call[0] == "context" {
						t.Fatal("DOCKER_HOST ignored")
					}
				}
			}
		})
	}
	for _, host := range []string{"", "unix:///var/run/docker.sock"} {
		expected := errors.New("unavailable")
		err := checkUSBDocker(func(...string) (string, error) { return "", expected }, host, "")
		if !errors.Is(err, expected) {
			t.Fatal(err)
		}
	}
}

func TestUSBDefaultAndInvalidSelectionDoNotInvokeDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	args, err := usbDockerArgs(shared.Config{})
	if err != nil || len(args) != 0 {
		t.Fatalf("args=%v err=%v", args, err)
	}
	_, err = usbDockerArgs(shared.Config{USBDevices: []string{"bad"}})
	if err == nil || !strings.Contains(err.Error(), "VID:PID") {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" {
		_, err = usbDockerArgs(shared.Config{USBDevices: []string{"2104:0313"}})
		if err == nil || !strings.Contains(err.Error(), "Linux") {
			t.Fatal(err)
		}
	}
}
