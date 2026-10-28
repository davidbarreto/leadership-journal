// Package config loads the application settings. Precedence, highest first:
// command-line flags, environment variables, defaults. No secrets live here.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"leadership-journal/internal/domain"
)

const (
	defaultAddr = "127.0.0.1:8080"
	dbFileName  = "journal.db"
)

// Config holds the settings of one run.
type Config struct {
	// Addr is the listen address; only loopback addresses are accepted.
	Addr     string
	DataDir  string
	LogLevel slog.Level
	// Timezone is the IANA zone the owner works in. Empty means detect it
	// from the operating system.
	Timezone string
	// ProgressDays is how many days may pass between progress checks before
	// a person is flagged overdue.
	ProgressDays int
}

// Load builds the configuration from args (without the program name) and the
// environment, validating it.
func Load(args []string, getenv func(string) string) (Config, error) {
	defaultDir := ""
	if home := getenv("HOME"); home != "" {
		defaultDir = filepath.Join(home, ".leadership-journal")
	}

	fs := flag.NewFlagSet("journal", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // the caller reports the returned error
	addr := fs.String("addr", firstNonEmpty(getenv("JOURNAL_ADDR"), defaultAddr), "listen address (loopback only) [JOURNAL_ADDR]")
	dir := fs.String("data-dir", firstNonEmpty(getenv("JOURNAL_DATA_DIR"), defaultDir), "directory holding the database [JOURNAL_DATA_DIR]")
	level := fs.String("log-level", firstNonEmpty(getenv("JOURNAL_LOG_LEVEL"), "info"), "debug, info, warn or error [JOURNAL_LOG_LEVEL]")
	tz := fs.String("timezone", firstNonEmpty(getenv("JOURNAL_TIMEZONE"), getenv("TZ")), "IANA time zone, e.g. Europe/Lisbon [JOURNAL_TIMEZONE]")
	days := fs.Int("progress-days", defaultProgressDays(getenv), "days allowed between progress checks [JOURNAL_PROGRESS_DAYS]")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if *dir == "" {
		return Config{}, errors.New("cannot determine the data directory: set -data-dir or JOURNAL_DATA_DIR")
	}
	if err := validateLoopback(*addr); err != nil {
		return Config{}, err
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(*level)); err != nil {
		return Config{}, fmt.Errorf("invalid log level %q: use debug, info, warn or error", *level)
	}

	if *days < 1 {
		return Config{}, fmt.Errorf("invalid progress interval %d: must be at least 1 day", *days)
	}

	cfg := Config{Addr: *addr, DataDir: *dir, LogLevel: lvl, Timezone: *tz, ProgressDays: *days}
	if _, err := cfg.Location(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// defaultProgressDays reads JOURNAL_PROGRESS_DAYS. An unparsable value yields
// 0, which Load then rejects, instead of silently falling back to 14.
func defaultProgressDays(getenv func(string) string) int {
	v := getenv("JOURNAL_PROGRESS_DAYS")
	if v == "" {
		return domain.DefaultProgressDays
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// validateLoopback refuses any address that could expose the journal beyond
// this machine. The data is sensitive, so this is a hard rule, not a default.
func validateLoopback(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: want host:port on 127.0.0.1, localhost or [::1]", addr)
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return fmt.Errorf("refusing to listen on %q: only 127.0.0.1, localhost and ::1 are allowed, so the data never leaves this machine", host)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("invalid port %q in address %q", port, addr)
	}
	return nil
}

// Location returns the owner's time zone, detecting it when not configured.
func (c Config) Location() (*time.Location, error) {
	if c.Timezone == "" {
		return detectLocation(), nil
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, fmt.Errorf("unknown time zone %q: use an IANA name such as Europe/Lisbon", c.Timezone)
	}
	return loc, nil
}

// detectLocation finds the system zone's IANA name by following the
// /etc/localtime symlink. Go's own time.Local reports just "Local", which
// would not survive being stored and read back elsewhere.
func detectLocation() *time.Location {
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, found := strings.Cut(target, "zoneinfo/"); found {
			if loc, err := time.LoadLocation(name); err == nil {
				return loc
			}
		}
	}
	return time.UTC
}

// DBPath is the SQLite file inside the data directory.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, dbFileName) }

// EnsureDataDir creates the data directory, private to the current user.
func (c Config) EnsureDataDir() error {
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	return nil
}
