// Package distribution serves checksum-verified release artifacts.
package distribution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var filename = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Proxy caches one server release. LocalDir bypasses GitHub but not verification.
// CacheDir must be dedicated to Version. BaseURL is only overridden by tests.
type Proxy struct {
	Version, ReleaseRepo, CacheDir, LocalDir, BaseURL string
	mu                                                sync.Mutex
}

func safeName(s string) bool {
	return filename.MatchString(s) && !strings.Contains(s, "..")
}
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/_/dl/")
	if !safeName(name) {
		http.NotFound(w, r)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	data, err := p.manifest(r)
	if err != nil {
		http.Error(w, "release checksums unavailable", 502)
		return
	}
	sums, err := parseChecksums(data)
	if err != nil {
		http.Error(w, "invalid release checksums", 502)
		return
	}
	if name == "checksums.txt" {
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
		return
	}
	sum, ok := sums[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	path, err := p.artifact(r, name, sum)
	if err != nil {
		http.Error(w, "release artifact unavailable or checksum mismatch", 502)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "release artifact unavailable", 502)
		return
	}
	defer f.Close()
	http.ServeContent(w, r, name, time.Time{}, f)
}
func parseChecksums(data []byte) (map[string]string, error) {
	sums := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid checksum line")
		}
		name := strings.TrimPrefix(fields[1], "*")
		hash, err := hex.DecodeString(fields[0])
		if err != nil || len(hash) != 32 || !safeName(name) || name == "checksums.txt" {
			return nil, fmt.Errorf("invalid checksum entry")
		}
		if _, exists := sums[name]; exists {
			return nil, fmt.Errorf("duplicate checksum")
		}
		sums[name] = strings.ToLower(fields[0])
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("empty checksums")
	}
	return sums, nil
}
func (p *Proxy) manifest(r *http.Request) ([]byte, error) {
	dir := p.CacheDir
	if p.LocalDir != "" {
		dir = p.LocalDir
	}
	path := filepath.Join(dir, "checksums.txt")
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if p.LocalDir != "" || !os.IsNotExist(err) {
		return nil, err
	}
	body, err := p.download(r, "checksums.txt")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err = io.ReadAll(io.LimitReader(body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("checksums too large")
	}
	if _, err = parseChecksums(data); err != nil {
		return nil, err
	}
	if err = p.cache("checksums.txt", bytes.NewReader(data), ""); err != nil {
		return nil, err
	}
	return data, nil
}
func (p *Proxy) artifact(r *http.Request, name, sum string) (string, error) {
	dir := p.CacheDir
	if p.LocalDir != "" {
		dir = p.LocalDir
	}
	path := filepath.Join(dir, name)
	if err := verifyFile(path, sum); err == nil {
		return path, nil
	} else if p.LocalDir != "" {
		return "", err
	}
	body, err := p.download(r, name)
	if err != nil {
		return "", err
	}
	defer body.Close()
	if err = p.cache(name, body, sum); err != nil {
		return "", err
	}
	return path, nil
}
func verifyFile(path, sum string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != sum {
		return fmt.Errorf("checksum mismatch")
	}
	return nil
}
func (p *Proxy) cache(name string, body io.Reader, sum string) error {
	if err := os.MkdirAll(p.CacheDir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(p.CacheDir, ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, (512<<20)+1))
	if err != nil {
		return err
	}
	if n > 512<<20 {
		return fmt.Errorf("artifact too large")
	}
	if sum != "" && hex.EncodeToString(h.Sum(nil)) != sum {
		return fmt.Errorf("checksum mismatch")
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(p.CacheDir, name))
}
func (p *Proxy) download(r *http.Request, name string) (io.ReadCloser, error) {
	base := p.BaseURL
	if base == "" {
		base = "https://github.com"
	}
	repo := p.ReleaseRepo
	if repo == "" {
		repo = "reorx/tinyhost"
	}
	req, err := http.NewRequestWithContext(r.Context(), "GET", base+"/"+repo+"/releases/download/v"+strings.TrimPrefix(p.Version, "v")+"/"+name, nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		return nil, fmt.Errorf("upstream HTTP %d", response.StatusCode)
	}
	return response.Body, nil
}
