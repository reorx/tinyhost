package bundle

import (
	"archive/zip"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

type zipEntry struct {
	file *zip.File
	path string
}

func zipHidden(p string) bool {
	if hidden(p) {
		return true
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "node_modules" {
			return true
		}
	}
	return false
}

// collectZip validates every entry name and splits entries into kept files and
// skipped names. __MACOSX entries are dropped without a trace.
func collectZip(zr *zip.Reader) (kept []zipEntry, skipped []string, err error) {
	for _, f := range zr.File {
		name := f.Name
		if strings.HasSuffix(name, "/") || f.FileInfo().IsDir() {
			continue
		}
		if name == "__MACOSX" || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		if f.Flags&0x1 != 0 {
			return nil, nil, ErrEncrypted
		}
		if !f.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("unsupported entry: %s", name)
		}
		if !utf8.ValidString(name) {
			return nil, nil, fmt.Errorf("file name is not UTF-8: %q", name)
		}
		p, err := normalizePath(name)
		if err != nil {
			return nil, nil, err
		}
		if zipHidden(p) {
			skipped = append(skipped, p)
			continue
		}
		kept = append(kept, zipEntry{file: f, path: p})
	}
	return kept, skipped, nil
}

// stripShell removes a single wrapping folder X when every kept entry lives in
// it and X/index.html exists. It returns the prefix it removed, if any.
func stripShell(kept []zipEntry) string {
	has := map[string]bool{}
	first := ""
	for i, e := range kept {
		has[e.path] = true
		head, _, nested := strings.Cut(e.path, "/")
		if !nested || (i > 0 && head != first) {
			return ""
		}
		first = head
	}
	if first == "" || has["index.html"] || !has[first+"/index.html"] {
		return ""
	}
	return first + "/"
}

func indexCandidates(paths []string) []string {
	var dirs []string
	for _, p := range paths {
		if strings.HasSuffix(p, "/index.html") {
			dirs = append(dirs, strings.TrimSuffix(p, "index.html"))
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		di, dj := strings.Count(dirs[i], "/"), strings.Count(dirs[j], "/")
		if di != dj {
			return di < dj
		}
		return dirs[i] < dirs[j]
	})
	if len(dirs) > 3 {
		dirs = dirs[:3]
	}
	return dirs
}

// UnpackZip validates a zip archive and streams its regular files to sink. It
// mirrors Unpack, and additionally ignores __MACOSX and node_modules, and strips
// one wrapping folder when that makes index.html appear at the root.
func UnpackZip(r io.ReaderAt, size int64, limits Limits, sink func(string, io.Reader) error) (Summary, error) {
	var summary Summary
	limits = defaults(limits)
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return summary, err
	}
	kept, skipped, err := collectZip(zr)
	if err != nil {
		return summary, err
	}
	if limits.RequireIndex {
		prefix := stripShell(kept)
		for i := range kept {
			kept[i].path = strings.TrimPrefix(kept[i].path, prefix)
		}
	}
	summary.Skipped = skipped
	seen := map[string]bool{}
	var total int64
	for _, e := range kept {
		if seen[e.path] {
			return summary, fmt.Errorf("duplicate path: %s", e.path)
		}
		seen[e.path] = true
		declared := e.file.UncompressedSize64
		if declared > uint64(limits.MaxSize-total) {
			return summary, ErrTooLarge
		}
		total += int64(declared)
	}
	if len(kept) > limits.MaxFiles {
		return summary, ErrTooLarge
	}
	index := false
	for _, e := range kept {
		rc, err := e.file.Open()
		if err != nil {
			return summary, err
		}
		n, err := readZipEntry(rc, e, sink, &summary)
		rc.Close()
		if err != nil {
			return summary, err
		}
		if e.path == "tinyhost.json" {
			continue
		}
		summary.Files++
		summary.Size += n
		if e.path == "index.html" {
			index = true
		}
	}
	if limits.RequireIndex && !index {
		paths := make([]string, 0, len(kept))
		for _, e := range kept {
			paths = append(paths, e.path)
		}
		return summary, &MissingIndexError{Candidates: indexCandidates(paths)}
	}
	return summary, nil
}

// readZipEntry hands one entry to the manifest reader or the sink and drains the
// rest so archive/zip can verify the size and CRC. It returns the bytes read.
func readZipEntry(rc io.Reader, e zipEntry, sink func(string, io.Reader) error, summary *Summary) (int64, error) {
	counter := &countReader{r: rc}
	if e.path == "tinyhost.json" {
		manifest, err := readManifest(counter)
		if err != nil {
			return 0, fmt.Errorf("tinyhost.json: %w", err)
		}
		summary.Manifest = manifest
	} else if err := sink(e.path, counter); err != nil {
		return 0, err
	}
	if _, err := io.Copy(io.Discard, counter); err != nil {
		return 0, err
	}
	return counter.n, nil
}

type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
