package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

type commandRun func(dir, name string, args ...string) error

func pathPrefix(path string, n int) string {
	parts := strings.FieldsFunc(strings.ReplaceAll(path, "\\", "/"), func(c rune) bool { return c == '/' })
	if len(parts) > n {
		parts = parts[:n]
	}
	return strings.Join(parts, "/")
}
func commonPrefixes(paths []string, n int) []string {
	counts := map[string]int{}
	for _, path := range paths {
		counts[pathPrefix(path, n)]++
	}
	keys := sortedKeys(counts)
	sort.SliceStable(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
	return keys
}
func communityLabel(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	for _, n := range []int{3, 2} {
		keys := commonPrefixes(paths, n)
		top := keys[0]
		count := 0
		for _, path := range paths {
			if pathPrefix(path, n) == top {
				count++
			}
		}
		if top != "" && float64(count)/float64(len(paths)) >= 0.55 {
			if n == 3 && strings.Contains(filepath.Base(top), ".") {
				continue
			}
			return strings.TrimPrefix(top, "packages/")
		}
	}
	keys := commonPrefixes(paths, 2)
	if len(keys) > 2 {
		keys = keys[:2]
	}
	for index, key := range keys {
		keys[index] = strings.TrimPrefix(key, "packages/")
	}
	return strings.Join(keys, " · ")
}
func graphLabels(data []byte) (map[string]string, error) {
	var graph struct {
		Nodes []struct {
			Community  json.RawMessage `json:"community"`
			SourceFile string          `json:"source_file"`
		}
	}
	if err := json.Unmarshal(data, &graph); err != nil {
		return nil, err
	}
	groups := map[string][]string{}
	for _, node := range graph.Nodes {
		if len(node.Community) == 0 || string(node.Community) == "null" {
			continue
		}
		key := strings.Trim(string(node.Community), "\"")
		if _, err := strconv.Atoi(key); err != nil {
			return nil, fmt.Errorf("неверный community: %s", key)
		}
		if _, ok := groups[key]; !ok {
			groups[key] = nil
		}
		if node.SourceFile != "" {
			groups[key] = append(groups[key], node.SourceFile)
		}
	}
	labels := map[string]string{}
	for key, paths := range groups {
		label := communityLabel(paths)
		if label == "" {
			label = "Community " + key
		}
		labels[key] = label
	}
	return labels, nil
}
func GraphLabels(root string, run commandRun, out io.Writer) error {
	dir := filepath.Join(root, "graphify-out")
	data, err := os.ReadFile(filepath.Join(dir, "graph.json"))
	if err != nil {
		return err
	}
	labels, err := graphLabels(data)
	if err != nil {
		return err
	}
	data, err = json.MarshalIndent(labels, "", "  ")
	if err != nil {
		return err
	}
	if err := shared.AtomicWrite(filepath.Join(dir, ".graphify_labels.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	limit := os.Getenv("GRAPHIFY_VIZ_NODE_LIMIT")
	if limit == "" {
		limit = "50000"
	}
	if n, err := strconv.Atoi(limit); err != nil || n < 1 {
		return errors.New("GRAPHIFY_VIZ_NODE_LIMIT должен быть положительным числом")
	}
	if err := run(root, "graphify", "export", "html", "--graph", filepath.Join(dir, "graph.json"), "--node-limit", limit); err != nil {
		return err
	}
	fmt.Fprintln(out, "Подписи сообществ и graph.html обновлены")
	return nil
}
func GraphView(root string, port int) error {
	if port < 1 || port > 65535 {
		return errors.New("порт должен быть от 1 до 65535")
	}
	dir := filepath.Join(root, "graphify-out")
	if _, err := os.Stat(filepath.Join(dir, "graph.html")); os.IsNotExist(err) {
		if err := shared.Run(root, "graphify", "update", "."); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := GraphLabels(root, shared.Run, os.Stdout); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return fmt.Errorf("порт %d: %w", port, err)
	}
	fmt.Fprintf(os.Stdout, "Граф: http://localhost:%d/graph.html\nCtrl+C останавливает сервер\n", port)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server := &http.Server{Handler: graphHandler(dir), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		return server.Shutdown(shutdown)
	}
}
func graphHandler(root string) http.Handler {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return http.NotFoundHandler()
	}
	root, err = filepath.Abs(canonical)
	if err != nil {
		return http.NotFoundHandler()
	}
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, part := range strings.Split(r.URL.Path, "/") {
			if strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		path := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
		real, err := filepath.EvalSymlinks(path)
		if err == nil {
			rel, err := filepath.Rel(root, real)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				http.NotFound(w, r)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}
