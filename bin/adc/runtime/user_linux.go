package runtime

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func runtimeUser() error {
	if os.Geteuid() != 0 {
		return os.Setenv("HOME", "/home/node")
	}
	identity, err := user.Lookup("node")
	if err != nil {
		return err
	}
	parse := func(key string) (int, error) {
		value := os.Getenv(key)
		if value == "" {
			value = "1000"
		}
		id, err := strconv.ParseUint(value, 10, 32)
		if err != nil || id == 0 {
			return 0, fmt.Errorf("%s должен быть положительным UID/GID", key)
		}
		return int(id), nil
	}
	uid, err := parse("ADC_UID")
	if err != nil {
		return err
	}
	gid, err := parse("ADC_GID")
	if err != nil {
		return err
	}
	if identity.Gid != strconv.Itoa(gid) {
		if err := shared.Run("", "groupmod", "-o", "-g", strconv.Itoa(gid), "node"); err != nil {
			return err
		}
	}
	if identity.Uid != strconv.Itoa(uid) {
		if err := shared.Run("", "usermod", "-o", "-u", strconv.Itoa(uid), "node"); err != nil {
			return err
		}
	}
	info, err := os.Stat("/home/node")
	if err != nil {
		return err
	}
	owner := int(info.Sys().(*syscall.Stat_t).Uid)
	for _, root := range []string{"/home/node", "/opt/ai-tools", "/workspace", "/var/lib/adc"} {
		if err := os.MkdirAll(root, 0755); err != nil {
			return err
		}
		if owner != uid && (root == "/home/node" || root == "/opt/ai-tools") {
			if err := reown(root, owner, uid, gid); err != nil {
				return err
			}
		}
		if err := os.Chown(root, uid, gid); err != nil {
			return err
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	env := replaceEnv(os.Environ(), map[string]string{"HOME": "/home/node"})
	return syscall.Exec("/usr/sbin/runuser", []string{"runuser", "-u", "node", "--", exe, "runtime-serve"}, env)
}

// Never follow symlinks or descend into mounted filesystems (including projects).
func reown(root string, previous, uid, gid int) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	device := info.Sys().(*syscall.Stat_t).Dev
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		if stat.Dev != device && entry.IsDir() {
			return fs.SkipDir
		}
		if int(stat.Uid) == previous {
			return os.Lchown(path, uid, gid)
		}
		return nil
	})
}
