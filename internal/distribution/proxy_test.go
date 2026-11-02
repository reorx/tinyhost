package distribution

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Behavior: only files listed by the release manifest are served, verified before cache publication and rechecked on cache reads.
func TestDownloadProxyOnlyServesListedFilesAndVerifiesChecksum(t *testing.T) {
	const file = "tinyhost_1.2.3_linux_amd64.tar.gz"
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.HasPrefix(r.URL.Path, "/owner/repo/releases/download/v1.2.3/") {
			t.Errorf("path %s", r.URL.Path)
		}
		switch filepath.Base(r.URL.Path) {
		case "checksums.txt":
			fmt.Fprintf(w, "%x  %s\n%x  broken.tar.gz\n", sha256.Sum256([]byte("good")), file, sha256.Sum256([]byte("expected")))
		case file:
			io.WriteString(w, "good")
		case "broken.tar.gz":
			io.WriteString(w, "corrupt")
		default:
			t.Errorf("unlisted upstream request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	dir := t.TempDir()
	p := &Proxy{Version: "1.2.3", ReleaseRepo: "owner/repo", CacheDir: dir, BaseURL: upstream.URL}
	get := func(name string, want int) string {
		t.Helper()
		w := httptest.NewRecorder()
		p.ServeHTTP(w, httptest.NewRequest("GET", "http://apps.test/_/dl/"+name, nil))
		if w.Code != want {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	if get(file, 200) != "good" {
		t.Fatal("wrong artifact")
	}
	n := hits.Load()
	get(file, 200)
	if hits.Load() != n {
		t.Fatal("cache miss")
	}
	get("not-listed.tar.gz", 404)
	get("broken.tar.gz", 502)
	get("%2e%2e%2fsecret", 404)
	if err := os.WriteFile(filepath.Join(dir, file), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if get(file, 200) != "good" {
		t.Fatal("served corrupt cache")
	}
	get("checksums.txt", 200)
	// Local artifacts use exactly the same allowlist and checksum checks.
	local := t.TempDir()
	os.WriteFile(filepath.Join(local, "checksums.txt"), []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte("good")), file)), 0600)
	os.WriteFile(filepath.Join(local, file), []byte("bad"), 0600)
	p = &Proxy{Version: "1.2.3", LocalDir: local, CacheDir: t.TempDir()}
	get(file, 502)
	get("not-listed.tar.gz", 404)
}
