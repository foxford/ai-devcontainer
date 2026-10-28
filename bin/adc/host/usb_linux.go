package host

import (
	"os"
	"syscall"
)

func statUSBNode(path string) (usbNode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return usbNode{}, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	dev := uint64(stat.Rdev)
	// Linux dev_t packs the low 8 minor bits and low 12 major bits separately.
	major := (dev>>8)&0xfff | (dev>>32)&0xfffff000
	minor := dev&0xff | (dev>>12)&0xffffff00
	return usbNode{Mode: info.Mode(), UID: stat.Uid, GID: stat.Gid, Major: major, Minor: minor}, nil
}
