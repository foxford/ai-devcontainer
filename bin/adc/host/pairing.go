package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

func dockerPairing(ctx context.Context, container string, started time.Time) (string, error) {
	if started.IsZero() {
		return "", errors.New("неизвестно время запуска контейнера")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "logs", "--since", started.UTC().Format(time.RFC3339Nano), container)
	return pairingFromCommand(cmd, cancel)
}

func pairingFromCommand(cmd *exec.Cmd, cancel context.CancelFunc) (string, error) {
	output, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	// Docker forwards both container streams. Use one pipe and drain it before Wait.
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return "", err
	}
	pairingURL, readErr := readOrcaPairing(output)
	if readErr != nil {
		cancel()
	}
	err = cmd.Wait()
	if readErr != nil {
		return "", readErr
	}
	if err != nil {
		return "", err
	}
	return pairingURL, nil
}

func readOrcaPairing(input io.Reader) (string, error) {
	// Ignore oversized unrelated log lines without buffering the whole journal.
	reader := bufio.NewReaderSize(input, 1024*1024)
	pairingURL, oversized := "", false
	for {
		line, partial, err := reader.ReadLine()
		if err == io.EOF {
			return pairingURL, nil
		}
		if err != nil {
			return "", err
		}
		if partial {
			oversized = true
			continue
		}
		if oversized {
			oversized = false
			continue
		}
		var event struct {
			Type    string          `json:"type"`
			Pairing json.RawMessage `json:"pairing"`
		}
		if json.Unmarshal(line, &event) != nil || event.Type != "orca_server_ready" {
			continue
		}
		// The latest readiness event is authoritative, including unavailable pairing.
		pairingURL = ""
		var pairing struct {
			Available bool   `json:"available"`
			URL       string `json:"url"`
		}
		if json.Unmarshal(event.Pairing, &pairing) == nil && pairing.Available && validPairingURL(pairing.URL) {
			pairingURL = pairing.URL
		}
	}
}

func validPairingURL(value string) bool {
	if strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "orca" && u.Host == "pair" && u.User == nil && u.Path == "" && u.Fragment == "" && u.Query().Get("code") != ""
}

func printWorkspacePairing(out io.Writer, port int, pairingURL string) {
	fmt.Fprintf(out, "Orca → Add Server → 127.0.0.1:%d\n", port)
	if pairingURL == "" {
		fmt.Fprintln(out, "Не удалось получить pairing URL. Откройте adc workspace logs и найдите pairing.url в последнем событии orca_server_ready.")
		return
	}
	fmt.Fprintf(out, "Access link: %s\n", pairingURL)
}
