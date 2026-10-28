// Command journal runs the leadership journal: a local web app for keeping
// notes about the people you manage. It only ever listens on loopback.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"leadership-journal/internal/config"
	"leadership-journal/internal/store"
	"leadership-journal/internal/web"
)

const shutdownTimeout = 10 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run starts the server and blocks until ctx is cancelled, then shuts down
// gracefully. It prints the address to stdout once it is listening.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.Load(args, getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	loc, err := cfg.Location()
	if err != nil {
		return err
	}
	now := func() time.Time { return time.Now().In(loc) }

	if err := cfg.EnsureDataDir(); err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath(), now)
	if err != nil {
		return err
	}
	defer st.Close()

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	// The listener's real address matters when the port was 0 (any free port).
	srv, err := web.New(web.Options{Store: st, Addr: ln.Addr().String(), Location: loc, Now: now, Logger: logger, ProgressDays: cfg.ProgressDays})
	if err != nil {
		ln.Close()
		return err
	}

	httpServer := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 16,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(ln) }()

	url := "http://" + ln.Addr().String()
	logger.Info("leadership journal started", "addr", ln.Addr().String(), "data_dir", cfg.DataDir, "timezone", loc.String())
	fmt.Fprintf(stdout, "Leadership Journal is running at %s (press Ctrl+C to stop)\n", url)

	select {
	case err := <-serveErr:
		return fmt.Errorf("server stopped: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
