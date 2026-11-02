package scripts

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Behavior: a release contains six CLI packages and three server packages, with portable names, injected versions and verified checksums.
func TestReleasePackagesAllSupportedPlatformsAndChecksums(t *testing.T) {
	work := t.TempDir()
	// The fake compiler makes cross-platform packaging testable without nine builds.
	compiler := `#!/bin/sh
set -eu
while [ "$1" != "-o" ]; do
  if [ "$1" = "-ldflags" ]; then flags=$2; fi
  shift
done
printf '%s|%s|%s|%s|%s\n' "$CGO_ENABLED" "$GOOS" "$GOARCH" "$flags" "$3" > "$2"
`
	if err := os.WriteFile(filepath.Join(work, "go"), []byte(compiler), 0755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(work, "dist")
	cmd := exec.Command("sh", "dist.sh", "v0.0.0-test", out)
	cmd.Env = append(os.Environ(), "PATH="+work+string(os.PathListSeparator)+os.Getenv("PATH"))
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dist: %v\n%s", err, data)
	}
	want := map[string]string{}
	for _, target := range []struct{ binary, os, arch string }{
		{"tinyhost", "linux", "amd64"}, {"tinyhost", "linux", "arm64"},
		{"tinyhost", "darwin", "amd64"}, {"tinyhost", "darwin", "arm64"},
		{"tinyhost", "windows", "amd64"}, {"tinyhost", "windows", "arm64"},
		{"tinyhost-server", "linux", "amd64"}, {"tinyhost-server", "linux", "arm64"},
		{"tinyhost-server", "darwin", "arm64"},
	} {
		ext := ".tar.gz"
		binary := target.binary
		if target.os == "windows" {
			ext = ".zip"
			binary += ".exe"
		}
		name := fmt.Sprintf("%s_0.0.0-test_%s_%s%s", target.binary, target.os, target.arch, ext)
		path := filepath.Join(out, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("missing release package: %v", err)
			continue
		}
		want[name] = fmt.Sprintf("%x", sha256.Sum256(data))
		payload := releasePayload(t, path, binary)
		for _, text := range []string{"0|" + target.os + "|" + target.arch + "|", "github.com/reorx/tinyhost/internal/version.Version=v0.0.0-test", "./cmd/" + target.binary} {
			if !strings.Contains(payload, text) {
				t.Errorf("%s: missing build contract %q in %q", name, text, payload)
			}
		}
	}
	manifest, err := os.ReadFile(filepath.Join(out, "checksums.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(manifest)), "\n")
	if len(lines) != 9 {
		t.Errorf("want nine checksum entries, got %d", len(lines))
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 || want[fields[1]] != fields[0] {
			t.Errorf("unexpected checksum entry %q", line)
			continue
		}
		delete(want, fields[1])
	}
	if len(want) != 0 {
		t.Errorf("packages missing from checksums: %v", want)
	}
}

func releasePayload(t *testing.T, path, binary string) string {
	t.Helper()
	if strings.HasSuffix(path, ".zip") {
		z, err := zip.OpenReader(path)
		if err != nil {
			t.Fatal(err)
		}
		defer z.Close()
		if len(z.File) != 1 || z.File[0].Name != binary {
			t.Fatalf("%s: expected only %s", path, binary)
		}
		r, err := z.File[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	if err != nil || header.Name != binary {
		t.Fatalf("%s: expected %s, got %v (%v)", path, binary, header, err)
	}
	data, err := io.ReadAll(tr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Fatalf("%s: expected only one binary", path)
	}
	return string(data)
}
