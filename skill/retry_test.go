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
	"sync"
	"testing"
)

func packCLI(t *testing.T, binary []byte) []byte {
	t.Helper()
	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "tinyhost", Mode: 0755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	tw.Write(binary)
	tw.Close()
	gz.Close()
	return packed.Bytes()
}

// flakyOrigin fails the first request to every path the way a CDN does when it
// cannot reach the origin (522) or drops the connection, then serves normally.
type flakyOrigin struct {
	mu    sync.Mutex
	seen  map[string]int
	serve http.HandlerFunc
}

func (o *flakyOrigin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	o.seen[r.URL.Path]++
	first := o.seen[r.URL.Path] == 1
	o.mu.Unlock()
	if first && strings.HasSuffix(r.URL.Path, ".tar.gz") {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
		return
	}
	if first {
		w.WriteHeader(522)
		return
	}
	o.serve(w, r)
}
func (o *flakyOrigin) count(path string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.seen[path]
}

// Behavior: the installer retries downloads that fail with a CDN 52x or a dropped connection, then installs normally.
func TestInstallScriptRetriesTransientDownloadFailures(t *testing.T) {
	home := t.TempDir()
	tools := t.TempDir()
	os.WriteFile(filepath.Join(tools, "uname"), []byte("#!/bin/sh\ncase $1 in -s) echo Linux;; -m) echo x86_64;; esac\n"), 0700)
	binary := []byte("#!/bin/sh\necho 1.2.3\n")
	packed := packCLI(t, binary)
	file := "tinyhost_1.2.3_linux_amd64.tar.gz"
	origin := &flakyOrigin{seen: map[string]int{}, serve: func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(packed), file)
			return
		}
		w.Write(packed)
	}}
	remote := httptest.NewServer(origin)
	defer remote.Close()
	script := filepath.Join(t.TempDir(), "install.sh")
	os.WriteFile(script, []byte(RenderInstall(remote.URL, "1.2.3", true)), 0600)
	cmd := exec.Command("sh", script)
	cmd.Env = []string{"HOME=" + home, "PATH=" + tools + ":" + os.Getenv("PATH"), "XDG_CONFIG_HOME=" + filepath.Join(home, "xdg")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	content, err := os.ReadFile(filepath.Join(home, ".local", "bin", "tinyhost"))
	if err != nil || !bytes.Equal(content, binary) {
		t.Fatal(err, string(content))
	}
	for _, path := range []string{"/_/dl/checksums.txt", "/_/dl/" + file} {
		if n := origin.count(path); n != 2 {
			t.Errorf("%s requested %d times, want 2", path, n)
		}
	}
}

// Behavior: an installer cut off at any line, as a dropped `curl | sh` stream delivers it, runs no command and writes nothing.
func TestTruncatedInstallScriptRunsNothing(t *testing.T) {
	tools := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	for _, name := range []string{"uname", "curl", "wget", "mktemp", "mkdir", "tar", "cp", "mv"} {
		os.WriteFile(filepath.Join(tools, name), []byte("#!/bin/sh\necho "+name+" >> "+marker+"\n"), 0700)
	}
	script := RenderInstall("http://127.0.0.1:1", "1.2.3", true)
	run := func(text string) {
		cmd := exec.Command("sh")
		cmd.Stdin = strings.NewReader(text)
		cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + tools + ":" + os.Getenv("PATH")}
		cmd.Run()
	}
	for i := 0; i < len(script)-1; i++ {
		if script[i] != '\n' {
			continue
		}
		run(script[:i+1])
		if ran, err := os.ReadFile(marker); err == nil {
			t.Fatalf("prefix ending at line %d ran %q:\n%s", strings.Count(script[:i+1], "\n"), ran, script[:i+1])
		}
	}
	run(script)
	if _, err := os.ReadFile(marker); err != nil {
		t.Fatal("the complete installer did not run")
	}
}

// installCommand is the install line from the rendered skill, as an Agent copies it.
func installCommand(t *testing.T, server string) string {
	t.Helper()
	for _, line := range strings.Split(Render(server), "\n") {
		if strings.HasPrefix(line, "curl ") && strings.Contains(line, server+"/install.sh") {
			return line
		}
	}
	t.Fatal("skill has no install command")
	return ""
}

// Behavior: the skill's documented install command retries a CDN 52x on install.sh instead of piping an empty script to sh.
func TestSkillInstallCommandRetriesTransientFailures(t *testing.T) {
	home := t.TempDir()
	origin := &flakyOrigin{seen: map[string]int{}, serve: func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "#!/bin/sh\ntouch \"$HOME/installed\"\n")
	}}
	remote := httptest.NewServer(origin)
	defer remote.Close()
	cmd := exec.Command("sh", "-c", installCommand(t, remote.URL))
	cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, "installed")); err != nil {
		t.Fatalf("install.sh did not run after a 522: %v (requests %d)", err, origin.count("/install.sh"))
	}
}
