package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatUSBNodeReadsActualDeviceWithoutFollowingSymlinks(t *testing.T) {
	node, err := statUSBNode("/dev/null")
	if err != nil || node.Mode&os.ModeCharDevice == 0 || node.Major != 1 || node.Minor != 3 {
		t.Fatalf("%+v %v", node, err)
	}
	link := filepath.Join(t.TempDir(), "device")
	if err := os.Symlink("/dev/null", link); err != nil {
		t.Fatal(err)
	}
	node, err = statUSBNode(link)
	if err != nil || node.Mode&os.ModeCharDevice != 0 {
		t.Fatalf("followed symlink: %+v %v", node, err)
	}
	if _, err := statUSBNode(filepath.Join(t.TempDir(), "missing")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
