package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer lets the test read what run prints while run is still going.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var urlLine = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestRun_ServesOnLoopbackAndShutsDownCleanly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-addr", "127.0.0.1:0", "-data-dir", dir, "-timezone", "Europe/Lisbon", "-log-level", "error"},
			env(map[string]string{"HOME": t.TempDir()}), out)
	}()

	var base string
	for i := 0; i < 200 && base == ""; i++ {
		base = urlLine.FindString(out.String())
		time.Sleep(25 * time.Millisecond)
	}
	if base == "" {
		cancel()
		t.Fatalf("the server never announced its address; output: %q", out.String())
	}

	// The first run shows the password setup, with the security headers.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/setup" {
		t.Errorf("GET / = %d -> %q, want a redirect to /setup", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, err = client.Get(base + "/setup")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Choose the password") {
		t.Errorf("GET /setup = %d, body %q", resp.StatusCode, body)
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Errorf("missing CSP header over the real socket")
	}

	// DNS rebinding: right socket, hostile Host header.
	req, _ := http.NewRequest(http.MethodGet, base+"/setup", nil)
	req.Host = "evil.example.com"
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("hostile Host header got %d, want 403", resp.StatusCode)
	}

	// The database was created privately.
	info, err := os.Stat(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("database file missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("database mode = %o, want 600", info.Mode().Perm())
	}
	dirInfo, _ := os.Stat(dir)
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("data directory mode = %o, want 700", dirInfo.Mode().Perm())
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned %v after a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after the context was cancelled")
	}
}

func TestRun_RefusesToExposeTheJournal(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.0.10:8080"} {
		err := run(context.Background(), []string{"-addr", addr, "-data-dir", t.TempDir()}, env(map[string]string{"HOME": "/h"}), io.Discard)
		if err == nil || !strings.Contains(err.Error(), "refusing") && !strings.Contains(err.Error(), "invalid") {
			t.Errorf("run(-addr %s) = %v, want a refusal", addr, err)
		}
	}
}

func TestRun_ReportsBadFlags(t *testing.T) {
	err := run(context.Background(), []string{"-nope"}, env(map[string]string{"HOME": "/h"}), io.Discard)
	if err == nil {
		t.Error("an unknown flag must be an error")
	}
}
