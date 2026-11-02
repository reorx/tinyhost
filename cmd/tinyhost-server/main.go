package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/reorx/tinyhost/internal/blob"
	"github.com/reorx/tinyhost/internal/server"
	"github.com/reorx/tinyhost/internal/store"
	"github.com/reorx/tinyhost/internal/token"
	"github.com/reorx/tinyhost/internal/version"
)

type command struct {
	name string
	run  func([]string, io.Writer) error
}

var commands = map[string]*command{}

func register(c *command) { commands[c.name] = c }
func init() {
	register(&command{name: "serve", run: serve})
	register(&command{name: "token", run: createToken})
	register(&command{name: "gc", run: gc})
	register(&command{name: "version", run: func(args []string, out io.Writer) error {
		if len(args) > 0 {
			return fmt.Errorf("version accepts no arguments")
		}
		_, err := fmt.Fprintln(out, version.Version)
		return err
	}})
}
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(os.Args[1:], os.Stdout); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, err := fmt.Fprintln(out, "tinyhost-server: serve | token create --name NAME [--ttl 30d] | gc | version")
		return err
	}
	c := commands[args[0]]
	if c == nil {
		return fmt.Errorf("unknown command %q", args[0])
	}
	return c.run(args[1:], out)
}
func env(key, fallback string) string {
	if value, ok := os.LookupEnv("TINYHOST_" + key); ok {
		return value
	}
	return fallback
}

// Use FlagSet.Set so malformed environment defaults fail like malformed flags.
func flags(name string, args []string, c *server.Config) (string, string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dir, listen string
	fs.StringVar(&dir, "data-dir", "./data", "")
	fs.StringVar(&listen, "listen", "127.0.0.1:3471", "")
	fs.StringVar(&c.AppsURL, "apps-url", "", "")
	fs.StringVar(&c.AdminURL, "admin-url", "", "")
	fs.IntVar(&c.MaxUploadMB, "max-upload-mb", 50, "")
	fs.IntVar(&c.MaxFiles, "max-files", 1000, "")
	fs.IntVar(&c.DataVersionsKeep, "data-versions-keep", 20, "")
	fs.IntVar(&c.TrashDays, "trash-days", 7, "")
	fs.StringVar(&c.TrustedProxyHeader, "trusted-proxy-header", "", "")
	fs.BoolVar(&c.InsecureCookies, "insecure-cookies", false, "")
	var envErr error
	fs.VisitAll(func(f *flag.Flag) {
		key := "TINYHOST_" + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
		if value, ok := os.LookupEnv(key); ok {
			if err := fs.Set(f.Name, value); err != nil {
				envErr = fmt.Errorf("%s: %w", key, err)
			}
		}
	})
	if envErr != nil {
		return "", "", envErr
	}
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	if fs.NArg() != 0 {
		return "", "", fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if c.MaxUploadMB <= 0 || c.MaxFiles <= 0 || c.DataVersionsKeep <= 0 || c.TrashDays <= 0 {
		return "", "", fmt.Errorf("limits must be positive")
	}
	return dir, listen, nil
}
func openServer(dir string, c server.Config) (*server.Server, error) {
	// store.Open constructs a file URI; an absolute path avoids a relative
	// directory being interpreted as the URI authority by SQLite.
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := store.Open(filepath.Join(dir, "tinyhost.db"))
	if err != nil {
		return nil, err
	}
	blobs, err := blob.New(dir)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &server.Server{Store: db, Blobs: blobs, Config: c}, nil
}
func serve(args []string, out io.Writer) error {
	var c server.Config
	dir, listen, err := flags("serve", args, &c)
	if err != nil {
		return err
	}
	if err = c.Validate(); err != nil {
		return err
	}
	s, err := openServer(dir, c)
	if err != nil {
		return err
	}
	defer s.Store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	gcDone := make(chan struct{})
	go func() {
		defer close(gcDone)
		s.RunGC(ctx)
	}()
	defer func() {
		stop()
		<-gcDone
	}()
	httpServer := &http.Server{Addr: listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown failed", "error", err)
			_ = httpServer.Close()
		}
	}()
	slog.Info("listening", "address", listen, "apps_url", c.AppsURL)
	err = httpServer.ListenAndServe()
	stop()
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func createToken(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "create" {
		return fmt.Errorf("usage: token create --name NAME [--ttl 30d]")
	}
	fs := flag.NewFlagSet("token create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("data-dir", env("DATA_DIR", "./data"), "")
	name := fs.String("name", "", "")
	ttl := fs.String("ttl", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *name == "" || fs.NArg() != 0 {
		return fmt.Errorf("token create requires --name")
	}
	expires, err := expiry(*ttl)
	if err != nil {
		return err
	}
	s, err := openServer(*dir, server.Config{})
	if err != nil {
		return err
	}
	defer s.Store.Close()
	plain := token.New()
	if _, err = s.Store.CreateClient(*name, token.Hash(plain), expires); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, plain)
	return err
}
func expiry(value string) (*int64, error) {
	if value == "" {
		return nil, nil
	}
	var duration time.Duration
	var err error
	if strings.HasSuffix(value, "d") {
		var days int64
		days, err = strconv.ParseInt(strings.TrimSuffix(value, "d"), 10, 64)
		if err == nil && (days <= 0 || days > 106751) {
			return nil, fmt.Errorf("ttl days out of range")
		}
		duration = time.Duration(days) * 24 * time.Hour
	} else {
		duration, err = time.ParseDuration(value)
	}
	if err != nil {
		return nil, err
	}
	if duration <= 0 {
		return nil, fmt.Errorf("ttl must be positive")
	}
	n := time.Now().Add(duration).Unix()
	return &n, nil
}
func gc(args []string, out io.Writer) error {
	var c server.Config
	dir, _, err := flags("gc", args, &c)
	if err != nil {
		return err
	}
	s, err := openServer(dir, c)
	if err != nil {
		return err
	}
	defer s.Store.Close()
	if err = s.GC(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "GC 完成")
	return err
}
