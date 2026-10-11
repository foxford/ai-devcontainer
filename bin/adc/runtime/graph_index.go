package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

func graphRun(dir, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = replaceEnv(os.Environ(), map[string]string{"GRAPHIFY_NO_AUTO_REFRESH": "1", "GRAPHIFY_NO_TIPS": "1"})
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

// Content hashes cover tracked and untracked source files, including dirty worktrees.
// Git supplies the ignore policy; generated outputs are excluded before indexing.
func graphFingerprint(root string) (string, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = root
	files, err := cmd.Output()
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	for _, name := range strings.Split(string(files), "\x00") {
		if name == "" || strings.HasPrefix(name, "graphify-out/") || strings.HasPrefix(name, ".ai-devcontainer/") || strings.HasPrefix(name, ".adc/") || strings.HasPrefix(name, ".graphify") && name != ".graphifyignore" {
			continue
		}
		// Git reports native source names, not archive or delivery targets.
		// Colons (for example :events route directories) are valid on Unix.
		relative := filepath.FromSlash(name)
		if !filepath.IsLocal(relative) {
			return "", fmt.Errorf("некорректный относительный путь исходника Git: %q", name)
		}
		path := filepath.Join(root, relative)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", name, info.Size())
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		// Very large assets cannot be useful AST inputs. Still invalidate on changes.
		if info.Size() > 8<<20 {
			fmt.Fprint(sum, info.ModTime().UnixNano())
		} else {
			_, err = io.Copy(sum, file)
		}
		closeErr := file.Close()
		if err = errors.Join(err, closeErr); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func EnsureGraph(root string, force bool, run commandRun, out io.Writer, versions ...string) error {
	if err := shared.MigrateProjectState(root); err != nil {
		return err
	}
	output := filepath.Join(root, "graphify-out")
	if info, err := os.Lstat(output); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return errors.New("graphify-out должен быть обычным каталогом checkout")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	version := ""
	if len(versions) > 0 {
		version = versions[0]
	}
	fingerprint, err := graphFingerprint(root)
	if err != nil {
		return err
	}
	state := filepath.Join(root, shared.ProjectStateDir, "graphify-state.json")
	var previous struct {
		Fingerprint string
		Version     string
	}
	data, _ := os.ReadFile(state)
	_ = json.Unmarshal(data, &previous)
	graph := filepath.Join(output, "graph.json")
	if !force && previous.Fingerprint == fingerprint && previous.Version == version {
		if info, err := os.Lstat(graph); err == nil && info.Mode().IsRegular() {
			return nil
		}
	}
	fmt.Fprintln(out, "Graphify: обновление локального индекса", root)
	if err = run(root, "graphify", "update", root, "--force"); err != nil {
		return fmt.Errorf("индекс не обновлён: %w", err)
	}
	info, err := os.Lstat(graph)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("индекс Graphify должен быть обычным файлом")
	}
	data, err = os.ReadFile(graph)
	if err != nil {
		return err
	}
	var result struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	if err = json.Unmarshal(data, &result); err != nil || result.Nodes == nil {
		return errors.New("Graphify вернула некорректный индекс")
	}
	data, err = json.Marshal(struct {
		Fingerprint string
		Version     string
	}{fingerprint, version})
	if err != nil {
		return err
	}
	return shared.AtomicWrite(state, append(data, '\n'), 0600)
}

func (a *App) ensureGraph(root string, force bool, out io.Writer) error {
	catalog, err := a.shared().DeliveryCatalog()
	if err != nil {
		return err
	}
	tools := catalog.Capabilities["graphify"].Tools
	if len(tools) != 1 {
		return errors.New("catalog: отсутствует поставка Graphify")
	}
	return a.graphLocked(root, func() error {
		return EnsureGraph(root, force, graphRun, out, tools[0].Version)
	})
}

func (a *App) graphLocked(root string, fn func() error) error {
	// A dedicated checkout lock serializes index writers without blocking shared
	// agent settings or indexing in other repositories and worktrees.
	locker := &shared.App{State: filepath.Join(a.State, "locks", "graphify-"+shared.CheckoutID(root))}
	return locker.Locked(fn)
}
