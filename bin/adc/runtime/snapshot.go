package runtime

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type transcriptBackup struct {
	path string
	time time.Time
	size int64
}

func transcriptArchives(dir string) ([]transcriptBackup, error) {
	files, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	archives := []transcriptBackup{}
	for _, file := range files {
		if file.IsDir() || !strings.HasPrefix(file.Name(), "transcripts-") || !strings.HasSuffix(file.Name(), ".tar.gz") {
			continue
		}
		info, err := file.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		archives = append(archives, transcriptBackup{filepath.Join(dir, file.Name()), info.ModTime(), info.Size()})
	}
	sort.Slice(archives, func(i, j int) bool {
		if archives[i].time.Equal(archives[j].time) {
			return archives[i].path > archives[j].path
		}
		return archives[i].time.After(archives[j].time)
	})
	return archives, nil
}

func ClaudeSnapshot(args []string, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(home, ".claude")
	}
	action := "snapshot"
	if len(args) > 0 {
		action = args[0]
	}
	archiveDir := filepath.Join(dir, "transcript-backups")
	archives, err := transcriptArchives(archiveDir)
	if err != nil {
		return err
	}
	switch action {
	case "list":
		for _, archive := range archives {
			fmt.Fprintf(out, "%s %10d %s\n", archive.time.Format("2006-01-02 15:04"), archive.size, archive.path)
		}
		return nil
	case "restore":
		if len(args) != 2 {
			return errors.New("требуется путь архива")
		}
		path, err := restoreTranscripts(dir, args[1])
		if err == nil {
			fmt.Fprintln(out, "Архив распакован в", path, "; текущие сессии сохранены")
		}
		return err
	case "retention":
		path := filepath.Join(dir, "settings.json")
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		settings := map[string]any{}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &settings); err != nil {
				return err
			}
			if settings == nil {
				return errors.New("settings.json должен быть объектом")
			}
		}
		if _, ok := settings["cleanupPeriodDays"]; ok {
			return nil
		}
		settings["cleanupPeriodDays"] = 3650
		data, err = json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return err
		}
		return shared.AtomicWrite(path, append(data, '\n'), 0600)
	case "snapshot", "daily":
		if action == "daily" && len(archives) > 0 && time.Since(archives[0].time) < 24*time.Hour {
			return nil
		}
		path, err := snapshotTranscripts(dir, 30)
		if err == nil && path != "" && action != "daily" {
			fmt.Fprintln(out, "Снапшот:", path)
		}
		return err
	default:
		return errors.New("adc tools claude-snapshot --help")
	}
}

func snapshotTranscripts(dir string, keep int) (string, error) {
	source := filepath.Join(dir, "projects")
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	hasTranscripts := false
	if err := filepath.WalkDir(source, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			hasTranscripts = true
		}
		return nil
	}); err != nil {
		return "", err
	}
	if !hasTranscripts {
		return "", nil
	}
	dest := filepath.Join(dir, "transcript-backups")
	if err := os.MkdirAll(dest, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dest, ".adc-transcripts-")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	compressed := gzip.NewWriter(file)
	archive := tar.NewWriter(compressed)
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(archive, input)
		closeErr := input.Close()
		return errors.Join(err, closeErr)
	})
	err = errors.Join(err, archive.Close(), compressed.Close(), file.Sync(), file.Close())
	if err != nil {
		return "", err
	}
	path := filepath.Join(dest, "transcripts-"+time.Now().UTC().Format("20060102-150405.000000000")+".tar.gz")
	if err := os.Rename(file.Name(), path); err != nil {
		return "", err
	}
	archives, err := transcriptArchives(dest)
	if err != nil {
		return "", err
	}
	for index := keep; index < len(archives); index++ {
		if err := os.Remove(archives[index].path); err != nil {
			return "", err
		}
	}
	return path, nil
}

func restoreTranscripts(dir, path string) (result string, err error) {
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	compressed, err := gzip.NewReader(input)
	if err != nil {
		return "", err
	}
	defer compressed.Close()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(dir, ".adc-restore-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	archive := tar.NewReader(compressed)
	var total int64
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if header.Name != "projects" && !strings.HasPrefix(header.Name, "projects/") {
			return "", errors.New("архив должен содержать projects/")
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(header.Name)))
		if clean != "projects" && !strings.HasPrefix(clean, "projects/") {
			return "", errors.New("архив выходит за projects/")
		}
		target, err := childPath(staging, header.Name)
		if err != nil {
			return "", err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0700); err != nil {
				return "", err
			}
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			if header.Size < 0 || header.Size > 512<<20 || total > 2<<30 {
				return "", errors.New("архив превышает лимит распаковки")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return "", err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return "", err
			}
			_, err = io.CopyN(file, archive, header.Size)
			err = errors.Join(err, file.Close())
			if err != nil {
				return "", err
			}
		default:
			return "", errors.New("ссылки и специальные файлы в архиве запрещены")
		}
	}
	result = filepath.Join(dir, "restored-"+time.Now().UTC().Format("20060102-150405.000000000"))
	return result, os.Rename(staging, result)
}
