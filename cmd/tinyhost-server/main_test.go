package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reorx/tinyhost/internal/store"
	"github.com/reorx/tinyhost/internal/token"
)

// Behavior: offline token creation prints one secret and persists only its hash with TTL.
func TestTokenCreateStoresHashAndExpiry(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"token", "create", "--data-dir", dir, "--name", "test", "--ttl", "30d"}, &out); err != nil {
		t.Fatal(err)
	}
	key := strings.TrimSpace(out.String())
	db, err := store.Open(filepath.Join(dir, "tinyhost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, err := db.ClientByTokenHash(token.Hash(key))
	if err != nil || c == nil || c.Name != "test" || c.ExpiresAt == nil || !strings.HasPrefix(key, "th_") {
		t.Fatalf("%v %v %q", c, err, key)
	}
}

// Behavior: command validation rejects missing names, invalid durations and invalid host configuration.
func TestInvalidCommandArgumentsAreRejected(t *testing.T) {
	for _, args := range [][]string{
		{"token", "create", "--data-dir", t.TempDir()},
		{"token", "create", "--name", "test", "--ttl", "-1d"},
		{"serve"},
		{"serve", "--apps-url", "http://same.test", "--admin-url", "http://same.test"},
		{"serve", "--apps-url", "ftp://apps.test"},
		{"serve", "--apps-url", "http://apps.test", "--max-files", "-1"},
		{"unknown"},
	} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

// Behavior: version and garbage collection commands run without starting a listener.
func TestVersionAndGCCommands(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"version"}, &out); err != nil || strings.TrimSpace(out.String()) == "" {
		t.Fatal(out.String(), err)
	}
	if err := run([]string{"gc", "--data-dir", t.TempDir()}, &out); err != nil {
		t.Fatal(err)
	}
}

// Behavior: relative data directories work for token creation and offline GC.
func TestRelativeDataDirectoryWorks(t *testing.T) {
	absolute := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, absolute)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"token", "create", "--name", "relative", "--data-dir", relative},
		{"gc", "--data-dir", relative},
	} {
		if err := run(args, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}
