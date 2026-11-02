package skill

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Behavior: install scripts pin the server version, use only its downloads and match Go config paths on Darwin and Linux.
func TestInstallScriptRendersServerAndVersion(t *testing.T) {
	script := RenderInstall("https://apps.example", "1.2.3", true)
	for _, text := range []string{"https://apps.example", "1.2.3", "/_/dl/", "checksums.txt", "sha256sum", "shasum", "Library/Application Support", "XDG_CONFIG_HOME", "TINYHOST_INSTALL_DIR", "tinyhost skill"} {
		if !strings.Contains(script, text) {
			t.Errorf("missing %q", text)
		}
	}
	if strings.Contains(script, "{{") || strings.Contains(script, "github.com") {
		t.Fatal(script)
	}
	if s := RenderInstall("http://localhost:3471", "dev", false); !strings.Contains(s, "此服务器是开发版本，不提供安装") || strings.Contains(s, "/_/dl/") {
		t.Fatal(s)
	}
}

// Behavior: the rendered installer installs into an isolated HOME, writes Go-compatible platform config, and rejects corruption before overwriting existing files.
func TestInstallScriptUsesPlatformConfigAndRejectsCorruptArchive(t *testing.T) {
	for _, platform := range []string{"Darwin", "Linux"} {
		t.Run(platform, func(t *testing.T) {
			home := t.TempDir()
			tools := filepath.Join(home, "tools")
			os.Mkdir(tools, 0700)
			os.WriteFile(filepath.Join(tools, "uname"), []byte("#!/bin/sh\ncase $1 in -s) echo "+platform+";; -m) echo arm64;; esac\n"), 0700)
			var packed bytes.Buffer
			gz := gzip.NewWriter(&packed)
			tw := tar.NewWriter(gz)
			binary := []byte("#!/bin/sh\necho 1.2.3\n")
			tw.WriteHeader(&tar.Header{Name: "tinyhost", Mode: 0755, Size: int64(len(binary))})
			tw.Write(binary)
			tw.Close()
			gz.Close()
			var corrupt atomic.Bool
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch filepath.Base(r.URL.Path) {
				case "checksums.txt":
					fmt.Fprintf(w, "%x  tinyhost_1.2.3_%s_arm64.tar.gz\n", sha256.Sum256(packed.Bytes()), strings.ToLower(map[string]string{"Darwin": "darwin", "Linux": "linux"}[platform]))
				default:
					if corrupt.Load() {
						w.Write([]byte("corrupted"))
					} else {
						w.Write(packed.Bytes())
					}
				}
			}))
			defer remote.Close()
			script := filepath.Join(home, "install.sh")
			os.WriteFile(script, []byte(RenderInstall(remote.URL, "1.2.3", true)), 0600)
			run := func() ([]byte, error) {
				cmd := exec.Command("sh", script)
				cmd.Env = []string{"HOME=" + home, "PATH=" + tools + ":" + os.Getenv("PATH"), "XDG_CONFIG_HOME=" + filepath.Join(home, "xdg")}
				return cmd.CombinedOutput()
			}
			if out, err := run(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			config := filepath.Join(home, "Library", "Application Support", "tinyhost", "config.json")
			if platform == "Linux" {
				config = filepath.Join(home, "xdg", "tinyhost", "config.json")
			}
			data, err := os.ReadFile(config)
			if err != nil || string(data) != `{"server":"`+remote.URL+`"}`+"\n" {
				t.Fatalf("%s %s %v", config, data, err)
			}
			info, _ := os.Stat(config)
			if info.Mode().Perm() != 0600 {
				t.Fatal(info.Mode())
			}
			installed := filepath.Join(home, ".local", "bin", "tinyhost")
			content, err := os.ReadFile(installed)
			if err != nil || !bytes.Equal(content, binary) {
				t.Fatal(err, string(content))
			}
			corrupt.Store(true)
			if out, err := run(); err == nil || !strings.Contains(string(out), "SHA-256 校验失败") {
				t.Fatalf("%v %s", err, out)
			}
			content, _ = os.ReadFile(installed)
			if !bytes.Equal(content, binary) {
				t.Fatal("corruption overwrote executable")
			}
		})
	}
}
