package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(nil, env(map[string]string{"HOME": "/home/ana"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:8080" {
		t.Errorf("Addr = %q, want the loopback default", cfg.Addr)
	}
	if cfg.DataDir != filepath.Join("/home/ana", ".leadership-journal") {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.ProgressDays != 14 {
		t.Errorf("ProgressDays = %d, want 14", cfg.ProgressDays)
	}
}

func TestLoad_Precedence_FlagsOverEnvOverDefaults(t *testing.T) {
	e := env(map[string]string{
		"HOME":                  "/home/ana",
		"JOURNAL_ADDR":          "127.0.0.1:9000",
		"JOURNAL_DATA_DIR":      "/from/env",
		"JOURNAL_LOG_LEVEL":     "debug",
		"JOURNAL_PROGRESS_DAYS": "21",
	})

	cfg, err := Load(nil, e)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9000" || cfg.DataDir != "/from/env" || cfg.LogLevel != slog.LevelDebug || cfg.ProgressDays != 21 {
		t.Errorf("env values not applied: %+v", cfg)
	}

	cfg, err = Load([]string{"-addr", "localhost:7000", "-data-dir", "/from/flag", "-log-level", "warn", "-progress-days", "7"}, e)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "localhost:7000" || cfg.DataDir != "/from/flag" || cfg.LogLevel != slog.LevelWarn || cfg.ProgressDays != 7 {
		t.Errorf("flags must win over env: %+v", cfg)
	}
}

func TestLoad_OnlyLoopbackAddressesAreAccepted(t *testing.T) {
	tests := []struct {
		addr string
		ok   bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:8080", true},
		{"[::1]:8080", true},
		{"127.0.0.1:0", true},
		{":8080", false},        // all interfaces
		{"0.0.0.0:8080", false}, // all interfaces
		{"[::]:8080", false},
		{"192.168.1.20:8080", false},
		{"example.com:8080", false},
		{"127.0.0.1", false}, // no port
		{"127.0.0.1:http", false},
		{"127.0.0.1:99999", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			_, err := Load([]string{"-addr", tc.addr}, env(map[string]string{"HOME": "/h"}))
			if tc.ok && err != nil {
				t.Errorf("Load(%q) = %v, want ok", tc.addr, err)
			}
			if !tc.ok && err == nil {
				t.Errorf("Load(%q) succeeded, want an error: the data must never be exposed beyond localhost", tc.addr)
			}
		})
	}
}

func TestLoad_Errors(t *testing.T) {
	home := env(map[string]string{"HOME": "/h"})
	tests := []struct {
		name string
		args []string
		env  func(string) string
		want string
	}{
		{"bad log level", []string{"-log-level", "loud"}, home, "log level"},
		{"unknown timezone", []string{"-timezone", "Mars/Base"}, home, "time zone"},
		{"zero progress days", []string{"-progress-days", "0"}, home, "progress interval"},
		{"non-numeric progress days env", nil, env(map[string]string{"HOME": "/h", "JOURNAL_PROGRESS_DAYS": "soon"}), "progress interval"},
		{"unknown flag", []string{"-nope"}, home, "nope"},
		{"no home and no data dir", nil, env(nil), "data-dir"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(tc.args, tc.env)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLocation(t *testing.T) {
	cfg := Config{Timezone: "Europe/Lisbon"}
	loc, err := cfg.Location()
	if err != nil || loc.String() != "Europe/Lisbon" {
		t.Errorf("Location() = %v, %v", loc, err)
	}

	auto, err := Config{}.Location()
	if err != nil || auto == nil {
		t.Errorf("automatic Location() = %v, %v; want a usable zone", auto, err)
	}
}

func TestEnsureDataDirAndDBPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	cfg := Config{DataDir: dir}
	if err := cfg.EnsureDataDir(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("data dir not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("data dir mode = %o, want 700", perm)
	}
	if cfg.DBPath() != filepath.Join(dir, "journal.db") {
		t.Errorf("DBPath() = %q", cfg.DBPath())
	}
	if err := cfg.EnsureDataDir(); err != nil {
		t.Errorf("EnsureDataDir must be repeatable: %v", err)
	}
}
