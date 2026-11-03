package blob

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("broken") }

// Behavior: equal content deduplicates, refreshes mtime, opens intact and can be walked and removed.
func TestBlobsDeduplicateAndRefreshMtime(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hash, n, err := s.Put(strings.NewReader("hello"))
	if err != nil || n != 5 {
		t.Fatal(hash, n, err)
	}
	f, err := s.Open(hash)
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	old := time.Now().Add(-2 * time.Hour)
	if err = os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	hash2, _, err := s.Put(strings.NewReader("hello"))
	if err != nil || hash2 != hash {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().After(old) {
		t.Fatal("mtime", err)
	}
	f, err = s.Open(hash)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(content) != "hello" {
		t.Fatal(string(content), err)
	}
	count := 0
	err = s.Walk(func(h string, info os.FileInfo) error {
		count++
		if h != hash || info.Size() != 5 {
			t.Fatal(h)
		}
		return nil
	})
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err = s.Remove(hash); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open(hash); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// Behavior: failed writes publish no partial blobs or temporary files and invalid hashes cannot escape storage.
func TestFailedWritesAreAtomicAndHashesAreValidated(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Put(brokenReader{}); err == nil {
		t.Fatal("accepted")
	}
	entries, err := os.ReadDir(filepath.Join(root, "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	for _, hash := range []string{"../secret", "", strings.Repeat("z", 64)} {
		if _, err = s.Open(hash); err == nil {
			t.Fatal(hash)
		}
		if err = s.Remove(hash); err == nil {
			t.Fatal(hash)
		}
	}
}

// Behavior: the final blob is invisible until a streaming write succeeds.
func TestReadersNeverSeePartialUploads(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, _, err := s.Put(r)
		r.Close()
		done <- err
	}()
	if _, err = w.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err = s.Walk(func(string, os.FileInfo) error { count++; return nil }); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	w.CloseWithError(errors.New("aborted"))
	if err = <-done; err == nil {
		t.Fatal("aborted upload succeeded")
	}
	if err = s.Walk(func(string, os.FileInfo) error { count++; return nil }); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

// Behavior: TempFile creates a writable file under the data dir's tmp folder, on the blobs filesystem.
func TestTempFileLivesUnderDataTmp(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.TempFile()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if filepath.Dir(f.Name()) != filepath.Join(dir, "tmp") || !strings.HasPrefix(filepath.Base(f.Name()), "upload-") {
		t.Fatal(f.Name())
	}
	if _, err = f.WriteString("x"); err != nil {
		t.Fatal(err)
	}
}
