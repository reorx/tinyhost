// Package bundle packs local sites and validates untrusted tar.gz uploads.
package bundle

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const ManifestLimit = 64 << 10

var ErrTooLarge = errors.New("bundle exceeds limits")

// ErrNoIndex is returned when a bundle that must have a root index.html has none.
var ErrNoIndex = errors.New("bundle requires index.html")

// ErrEncrypted is returned for zip archives with encrypted entries.
var ErrEncrypted = errors.New("encrypted zip entries are not supported")

// MissingIndexError is an ErrNoIndex that also names directories holding an index.html.
type MissingIndexError struct{ Candidates []string }

func (e *MissingIndexError) Error() string { return ErrNoIndex.Error() }
func (e *MissingIndexError) Unwrap() error { return ErrNoIndex }

type Manifest struct {
	App   string            `json:"app,omitempty"`
	Title string            `json:"title,omitempty"`
	Data  map[string]string `json:"data,omitempty"`
}

type Limits struct {
	MaxSize      int64
	MaxFiles     int
	RequireIndex bool
}

type Options struct {
	Data   bool
	Limits Limits
}

type Summary struct {
	Files        int
	Size         int64
	Manifest     Manifest
	Skipped      []string
	Warnings     []string
	PathWarnings []Warning
}

func defaults(l Limits) Limits {
	if l.MaxSize <= 0 {
		l.MaxSize = 50 << 20
	}
	if l.MaxFiles <= 0 {
		l.MaxFiles = 1000
	}
	return l
}

func normalizePath(p string) (string, error) {
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	if p == "" || len(p) > 512 || strings.ContainsAny(p, "\\\x00") {
		return "", fmt.Errorf("invalid path: %q", p)
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == ".." || segment == "." {
			return "", fmt.Errorf("invalid path: %q", p)
		}
	}
	return p, nil
}

func hidden(p string) bool {
	for _, segment := range strings.Split(p, "/") {
		if strings.HasPrefix(segment, ".") && segment != ".well-known" {
			return true
		}
	}
	return false
}

func readManifest(r io.Reader) (Manifest, error) {
	var manifest Manifest
	b, err := io.ReadAll(io.LimitReader(r, ManifestLimit+1))
	if err != nil {
		return manifest, err
	}
	if len(b) > ManifestLimit {
		return manifest, fmt.Errorf("tinyhost.json exceeds 64KB")
	}
	var object map[string]json.RawMessage
	if err = json.Unmarshal(b, &object); err != nil {
		return manifest, err
	}
	if object == nil {
		return manifest, fmt.Errorf("tinyhost.json must be an object")
	}
	err = json.Unmarshal(b, &manifest)
	return manifest, err
}

// Unpack streams validated regular files to sink. A failed upload may leave
// unreferenced blobs; callers must only commit a layer after success.
func Unpack(r io.Reader, limits Limits, sink func(string, io.Reader) error) (Summary, error) {
	var summary Summary
	limits = defaults(limits)
	gz, err := gzip.NewReader(r)
	if err != nil {
		return summary, err
	}
	defer gz.Close()
	// Bound metadata and trailing padding too: huge PAX headers or data after
	// the tar terminator must not bypass file payload counters.
	expanded := &io.LimitedReader{R: gz, N: limits.MaxSize + int64(limits.MaxFiles)*2048 + (1 << 20)}
	tr := tar.NewReader(expanded)
	seen := map[string]bool{}
	var total int64
	count := 0
	index := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return summary, err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return summary, fmt.Errorf("unsupported entry: %s", header.Name)
		}
		p, err := normalizePath(header.Name)
		if err != nil {
			return summary, err
		}
		if seen[p] {
			return summary, fmt.Errorf("duplicate path: %s", p)
		}
		seen[p] = true
		count++
		if header.Size < 0 || header.Size > limits.MaxSize-total || count > limits.MaxFiles {
			return summary, ErrTooLarge
		}
		total += header.Size
		if hidden(p) {
			summary.Skipped = append(summary.Skipped, p)
			continue
		}
		if p == "tinyhost.json" {
			summary.Manifest, err = readManifest(tr)
			if err != nil {
				return summary, fmt.Errorf("tinyhost.json: %w", err)
			}
			continue
		}
		if err = sink(p, tr); err != nil {
			return summary, err
		}
		// Drain in case the sink reads less than the entire file.
		if _, err = io.Copy(io.Discard, tr); err != nil {
			return summary, err
		}
		summary.Files++
		summary.Size += header.Size
		if p == "index.html" {
			index = true
		}
	}
	// Consume the trailer so CRC errors cannot be hidden by tar EOF.
	if _, err = io.Copy(io.Discard, expanded); err != nil {
		return summary, err
	}
	if expanded.N == 0 {
		return summary, ErrTooLarge
	}
	if limits.RequireIndex && !index {
		return summary, ErrNoIndex
	}
	return summary, nil
}
