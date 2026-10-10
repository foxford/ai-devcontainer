package shared

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// CopyPrivateTree copies a tree without following links, with private permissions.
func CopyPrivateTree(source, dest string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.Mkdir(out, 0700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, out)
		case info.Mode().IsRegular():
			input, err := os.Open(path)
			if err != nil {
				return err
			}
			defer input.Close()
			output, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600|info.Mode().Perm()&0100)
			if err != nil {
				return err
			}
			_, err = io.Copy(output, input)
			if err == nil {
				err = output.Sync()
			}
			closeErr := output.Close()
			if err != nil {
				return err
			}
			return closeErr
		default:
			return fmt.Errorf("неподдерживаемый тип файла: %s", rel)
		}
	})
}

// TreeDigest hashes names, contents and link targets without following links.
func TreeDigest(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%q\x00", filepath.ToSlash(rel))
		switch {
		case entry.IsDir():
			io.WriteString(hash, "dir\x00")
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(hash, "link:%q\x00", link)
		case info.Mode().IsRegular():
			fmt.Fprintf(hash, "file:%d:%d\x00", info.Size(), info.Mode().Perm()&0100)
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(hash, f)
			f.Close()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("неподдерживаемый тип файла: %s", rel)
		}
		io.WriteString(hash, "\x00")
		return nil
	})
	return hex.EncodeToString(hash.Sum(nil)), err
}
