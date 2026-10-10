package runtime

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	shared "github.com/foxford/ai-devcontainer/bin/adc/internal"
)

const networkAttempts = 5

func retryNetwork(ctx context.Context, label string, run func() error, wait func(context.Context, time.Duration) error) error {
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := run()
		if err == nil || attempt == networkAttempts || !transientNetworkError(err) {
			return err
		}
		delay := time.Second << (attempt - 1)
		message := fmt.Sprintf("%s: сетевой сбой; повтор %d/%d через %s: %v", label, attempt+1, networkAttempts, delay, err)
		if os.Getenv("ADC_STARTUP_STATUS") == "" {
			fmt.Fprintln(os.Stderr, message)
		} else {
			shared.StartupProgress(message)
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
}

func waitNetworkRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type downloadStatusError struct {
	status int
}

func (e downloadStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.status) }

func transientNetworkError(err error) bool {
	var certificate x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &certificate) || errors.As(err, &invalid) {
		return false
	}
	var status downloadStatusError
	if errors.As(err, &status) {
		return status.status == 408 || status.status == 429 || status.status == 500 || status.status == 502 || status.status == 503 || status.status == 504
	}
	var dns *net.DNSError
	var op *net.OpError
	var install installNetworkError
	return errors.As(err, &install) || errors.As(err, &dns) || errors.As(err, &op) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ETIMEDOUT) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, context.DeadlineExceeded)
}

// Only recognized transport failures allow repeating an install command.
// Other exits (permissions, incompatible packages, scripts) stay immediate.
func commandNetworkError(output string) bool {
	text := strings.ToLower(output)
	for _, permanent := range []string{"eacces", "eperm", "permission denied", "e404", "e401", "e403", "no matching version", "command not found"} {
		if strings.Contains(text, permanent) {
			return false
		}
	}
	for _, signature := range []string{"eai_again", "enotfound", "etimedout", "econnreset", "econnrefused", "socket hang up", "could not resolve host", "temporary failure in name resolution", "connection timed out", "connection reset by peer", "curl: (6)", "curl: (7)", "curl: (18)", "curl: (28)", "curl: (35)", "curl: (52)", "curl: (56)", "http 429", "http 502", "http 503", "http 504"} {
		if strings.Contains(text, signature) {
			return true
		}
	}
	return false
}

type installNetworkError struct{ error }

func (e installNetworkError) Unwrap() error {
	return e.error
}
