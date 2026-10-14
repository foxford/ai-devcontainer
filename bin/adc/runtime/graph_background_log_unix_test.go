//go:build linux || darwin

package runtime

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestGraphLogPlatformOpenDoesNotFollowSymlink(t *testing.T) {
	root := t.TempDir()
	protected := filepath.Join(root, "protected")
	path := filepath.Join(root, "graphify.log")
	writeFixture(t, protected, "ADC_FOREIGN_CONTENT")
	if err := os.Symlink(protected, path); err != nil {
		t.Fatal(err)
	}
	file, err := openGraphLogFile(path)
	if file != nil {
		_ = file.Close()
	}
	if err == nil {
		t.Fatal("platform log open followed a symlink after the Lstat check")
	}
}

func TestGraphBackgroundRejectsFIFOWithoutWaitingForReader(t *testing.T) {
	path := graphLogPath(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	assertGraphLogRejected(t, path)
	// Exercise the platform open even when replacement occurs after the common
	// Lstat check: opening a FIFO without a reader must still return immediately.
	done := make(chan error, 1)
	go func() {
		file, err := openGraphLogFile(path)
		if file != nil {
			_ = file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("opening a FIFO without a reader succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("platform log open blocked on a FIFO")
	}
}
