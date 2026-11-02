package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name string
	body string
	kind byte
}

func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	for _, item := range entries {
		kind := item.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{Name: item.name, Size: int64(len(item.body)), Mode: 0600, Typeflag: kind}
		if kind != tar.TypeReg {
			header.Size = 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := io.WriteString(tw, item.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func unpack(t *testing.T, data []byte, limits Limits) (Summary, map[string]string, error) {
	t.Helper()
	files := map[string]string{}
	summary, err := Unpack(bytes.NewReader(data), limits, func(path string, reader io.Reader) error {
		content, err := io.ReadAll(reader)
		files[path] = string(content)
		return err
	})
	return summary, files, err
}

func write(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func readBundle(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Behavior: packing and unpacking both discard dotfiles except .well-known and never expose tinyhost.json.
func TestDotfilesAreNeverServed(t *testing.T) {
	t.Run("pack", func(t *testing.T) {
		root := t.TempDir()
		for _, path := range []string{
			"index.html", ".env", ".git/config", "x/.secret", ".well-known/info",
			"node_modules/pkg/a", "x/node_modules/b",
		} {
			write(t, root, path, "ok")
		}
		write(t, root, "tinyhost.json", `{"app":"abc","title":"Title","data":{"data/":"sales"},"future":1}`)
		if err := os.Symlink("index.html", filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}

		path, packed, err := Pack(root)
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(path)
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		unpacked, err := Unpack(file, Limits{RequireIndex: true}, func(_ string, reader io.Reader) error {
			_, err := io.Copy(io.Discard, reader)
			return err
		})

		if err != nil {
			t.Fatal(err)
		}
		if unpacked.Files != 2 || packed.Files != 2 || len(packed.Warnings) != 1 || unpacked.Manifest.Title != "Title" {
			t.Fatal(unpacked, packed)
		}
	})

	t.Run("unpack", func(t *testing.T) {
		data := archive(t,
			entry{name: ".env", body: "secret"},
			entry{name: "x/.git/config", body: "secret"},
			entry{name: ".well-known/info", body: "ok"},
			entry{name: "tinyhost.json", body: `{"title":"Title"}`},
		)

		summary, files, err := unpack(t, data, Limits{})

		if err != nil || len(files) != 1 || len(summary.Skipped) != 2 || summary.Manifest.Title != "Title" {
			t.Fatal(summary, files, err)
		}
	})
}

// Behavior: traversal, absolute, ambiguous, overlong and duplicate paths and all nonregular files reject the entire upload.
func TestUnpackRejectsTraversalAndSymlinks(t *testing.T) {
	for _, path := range []string{"../x", "x/../y", "/x", "x\\y", "x//y", "", strings.Repeat("x", 513)} {
		t.Run(path, func(t *testing.T) {
			data := archive(t, entry{name: path, body: "x"})

			if _, _, err := unpack(t, data, Limits{}); err == nil {
				t.Fatal(path)
			}
		})
	}
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeFifo, tar.TypeChar, tar.TypeBlock} {
		data := archive(t, entry{name: "bad", kind: kind})

		if _, _, err := unpack(t, data, Limits{}); err == nil {
			t.Fatal(kind)
		}
	}

	duplicate := archive(t, entry{name: "x", body: "a"}, entry{name: "./x", body: "b"})
	if _, _, err := unpack(t, duplicate, Limits{}); err == nil {
		t.Fatal("duplicate")
	}

	directory := archive(t, entry{name: "folder/", kind: tar.TypeDir}, entry{name: "./index.html", body: "ok"})
	if _, _, err := unpack(t, directory, Limits{RequireIndex: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizePath("a\x00b"); err == nil {
		t.Fatal("NUL")
	}
}

// Behavior: limits count decompressed bytes and entries even when discarded; manifest is bounded and invalid JSON fails.
func TestUnpackEnforcesLimitsAndManifest(t *testing.T) {
	cases := []struct {
		name    string
		entries []entry
		limits  Limits
	}{
		{
			name:    "file exceeds size limit",
			entries: []entry{{name: "x", body: "1234"}},
			limits:  Limits{MaxSize: 3},
		},
		{
			name:    "discarded file exceeds size limit",
			entries: []entry{{name: ".secret", body: "1234"}},
			limits:  Limits{MaxSize: 3},
		},
		{
			name:    "too many files",
			entries: []entry{{name: "x", body: "a"}, {name: "y", body: "b"}},
			limits:  Limits{MaxFiles: 1},
		},
		{
			name:    "manifest exceeds size limit",
			entries: []entry{{name: "tinyhost.json", body: strings.Repeat(" ", 65537)}},
		},
		{
			name:    "malformed manifest",
			entries: []entry{{name: "tinyhost.json", body: "broken"}},
		},
		{
			name:    "null manifest",
			entries: []entry{{name: "tinyhost.json", body: "null"}},
		},
		{
			name:    "manifest has invalid data type",
			entries: []entry{{name: "tinyhost.json", body: `{"data":{"data/":42}}`}},
		},
		{
			name:    "missing required index",
			entries: []entry{{name: "x", body: "a"}},
			limits:  Limits{RequireIndex: true},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			data := archive(t, test.entries...)

			if _, _, err := unpack(t, data, test.limits); err == nil {
				t.Fatal(test)
			}
		})
	}

	corrupt := archive(t, entry{name: "index.html", body: "ok"})
	corrupt[len(corrupt)-5] ^= 0xff
	if _, _, err := unpack(t, corrupt, Limits{}); err == nil {
		t.Fatal("corrupt gzip")
	}
	if _, _, err := unpack(t, []byte("invalid"), Limits{}); err == nil {
		t.Fatal("invalid gzip")
	}

	data := archive(t, entry{name: "x", body: "a"})
	_, err := Unpack(bytes.NewReader(data), Limits{}, func(string, io.Reader) error {
		return io.ErrClosedPipe
	})
	if err == nil {
		t.Fatal("sink failure")
	}
}

// Behavior: single HTML deploys become index.html, data files retain names, and only deploy requires an index.
func TestPackSupportsSingleFilesAndDataBundles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "report.html", "html")

	path, summary, err := Pack(filepath.Join(root, "report.html"))
	if err != nil || summary.Files != 1 {
		t.Fatal(summary, err)
	}
	defer os.Remove(path)
	_, files, err := unpack(t, readBundle(t, path), Limits{})
	if err != nil || files["index.html"] != "html" {
		t.Fatal(files, err)
	}

	write(t, root, "sales.csv", "csv")
	path, _, err = Pack(filepath.Join(root, "sales.csv"), Options{Data: true})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	_, files, err = unpack(t, readBundle(t, path), Limits{})
	if err != nil || files["sales.csv"] != "csv" {
		t.Fatal(files, err)
	}

	if _, _, err = Pack(filepath.Join(root, "sales.csv")); err == nil {
		t.Fatal("non-html deploy")
	}
	if _, _, err = Pack(root); err == nil {
		t.Fatal("missing index")
	}
	if _, _, err = Pack(t.TempDir(), Options{Data: true}); err == nil {
		t.Fatal("empty")
	}

	write(t, root, "index.html", "1234")
	if _, _, err = Pack(root, Options{Limits: Limits{MaxSize: 3}}); err == nil {
		t.Fatal("size")
	}
	if _, _, err = Pack(root, Options{Limits: Limits{MaxFiles: 1}}); err == nil {
		t.Fatal("count")
	}
}

// Behavior: trailing compressed data is bounded and gzip integrity is checked beyond the tar end marker.
func TestUnpackBoundsAndValidatesTrailingData(t *testing.T) {
	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(gz, strings.NewReader(strings.Repeat("x", 3<<20))); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := unpack(t, raw.Bytes(), Limits{MaxSize: 10, MaxFiles: 1}); err == nil {
		t.Fatal("accepted trailing decompression bomb")
	}
	if _, err := normalizePath("x/"); err == nil {
		t.Fatal("empty trailing segment")
	}
}

// Behavior: skipped files count towards upload limits and manifests are limited during local packing too.
func TestSkippedEntriesAndManifestsCannotBypassLimits(t *testing.T) {
	data := archive(t, entry{name: ".a", body: "a"}, entry{name: ".b", body: "b"})
	if _, _, err := unpack(t, data, Limits{MaxFiles: 1}); err == nil {
		t.Fatal("skipped count")
	}

	root := t.TempDir()
	write(t, root, "index.html", "ok")
	write(t, root, "tinyhost.json", strings.Repeat(" ", ManifestLimit+1))
	if _, _, err := Pack(root); err == nil {
		t.Fatal("oversized manifest")
	}

	data = archive(t, entry{name: "index.html", body: "ab"})
	if _, _, err := unpack(t, data, Limits{MaxSize: 2, MaxFiles: 1, RequireIndex: true}); err != nil {
		t.Fatal(err)
	}
}
