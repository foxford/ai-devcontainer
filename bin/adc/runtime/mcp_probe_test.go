package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Fixtures authenticate real HTTP requests and enforce MCP initialization order.
func probeFixture(t *testing.T, sse bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	toolsCalls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			w.WriteHeader(401)
			fmt.Fprint(w, "fixture-key")
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		var request struct {
			ID     int
			Method string
			Params map[string]any
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(400)
			return
		}
		result := map[string]any{}
		switch request.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "fixture-session")
			result = map[string]any{"protocolVersion": "2025-03-26", "serverInfo": map[string]string{"name": "fixture", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "notifications/initialized":
			if r.Header.Get("Mcp-Session-Id") != "fixture-session" || r.Header.Get("MCP-Protocol-Version") != "2025-03-26" {
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(202)
			return
		case "tools/list":
			toolsCalls.Add(1)
			result = map[string]any{"tools": []any{map[string]string{"name": "read_vault"}}}
			if request.Params["cursor"] == nil {
				result["nextCursor"] = "second"
			}
		default:
			t.Error("doctor invoked a tool or unexpected method")
			w.WriteHeader(400)
			return
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			data, _ := json.Marshal(response)
			fmt.Fprintf(w, ": keepalive\n\ndata: %s\n\n", data)
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		}
	}))
	t.Cleanup(server.Close)
	return server, toolsCalls
}

func TestMCPProbeHTTPJSONAndSSE(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(fmt.Sprint(sse), func(t *testing.T) {
			fixture, calls := probeFixture(t, sse)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			count, err := probeMCP(ctx, t.TempDir(), MCPServer{"url": fixture.URL, "headers": map[string]any{"Authorization": "Bearer fixture-key"}})
			if err != nil || count != 2 || calls.Load() != 2 {
				t.Fatalf("authenticated paginated discovery: count=%d calls=%d err=%v", count, calls.Load(), err)
			}
		})
	}
}

func TestMCPProbeFailuresAreBoundedAndDoNotLeakBodies(t *testing.T) {
	for _, code := range []int{401, 403, 404, 302, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://invalid.example/?fixture-key")
				w.WriteHeader(code)
				fmt.Fprint(w, "fixture-key")
			}))
			defer fixture.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := probeMCP(ctx, t.TempDir(), MCPServer{"url": fixture.URL})
			if err == nil || strings.Contains(err.Error(), "fixture-key") {
				t.Fatal("missing error or leaked server body")
			}
		})
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := probeMCP(ctx, t.TempDir(), MCPServer{"url": fixture.URL})
	if err == nil || !strings.Contains(err.Error(), "таймаут") {
		t.Fatal("unbounded HTTP probe")
	}
	for _, address := range []string{"file:///etc/passwd", "http://key:password@localhost/mcp/", "http://localhost/mcp/?key=secret", "not-a-url"} {
		if validMCPURL(address) == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
}

func TestMCPProbeStdioAndTimeout(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"normal", "hang", "bad"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			count, err := probeMCP(ctx, t.TempDir(), MCPServer{"command": binary, "args": []any{"-test.run=^TestMCPProbeHelper$"}, "env": map[string]any{"ADC_PROBE_HELPER": mode}})
			if mode == "normal" {
				if err != nil || count != 1 {
					t.Fatalf("stdio handshake: %d %v", count, err)
				}
			} else if err == nil {
				t.Fatal("failed stdio server passed")
			}
		})
	}
}
func TestMCPProbeHelper(t *testing.T) {
	mode := os.Getenv("ADC_PROBE_HELPER")
	if mode == "" {
		return
	}
	if mode == "hang" {
		time.Sleep(10 * time.Second)
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	for {
		var request struct {
			ID     int
			Method string
		}
		if decoder.Decode(&request) != nil {
			return
		}
		if request.ID == 0 {
			continue
		}
		if mode == "bad" {
			fmt.Println("unexpected secret-bearing log")
			continue
		}
		result := map[string]any{"tools": []any{map[string]any{"name": "read"}}}
		if request.Method == "initialize" {
			result = map[string]any{"protocolVersion": "2025-03-26", "serverInfo": map[string]string{"name": "test", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
}
