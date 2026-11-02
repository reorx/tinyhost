// Package blob implements immutable, SHA-256 addressed file storage.
package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type Store struct {
	root string
}

func New(dataDir string) (*Store, error) {
	for _, dir := range []string{"blobs", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dataDir, dir), 0700); err != nil {
			return nil, err
		}
	}
	return &Store{root: dataDir}, nil
}

func valid(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *Store) path(hash string) (string, error) {
	if !valid(hash) {
		return "", fmt.Errorf("invalid blob hash: %q", hash)
	}
	return filepath.Join(s.root, "blobs", hash[:2], hash), nil
}

func (s *Store) Put(r io.Reader) (string, int64, error) {
	f, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "blob-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, digest), r)
	if err != nil {
		return "", 0, err
	}
	if err = f.Sync(); err != nil {
		return "", 0, err
	}
	if err = f.Close(); err != nil {
		return "", 0, err
	}
	hash := hex.EncodeToString(digest.Sum(nil))
	dest, _ := s.path(hash)
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return "", 0, err
	}
	// Link atomically installs only if absent, even when concurrent writers race.
	// Unlike rename it never replaces an existing immutable inode.
	if err = os.Link(f.Name(), dest); err != nil && !os.IsExist(err) {
		return "", 0, err
	}
	now := time.Now()
	if err = os.Chtimes(dest, now, now); err != nil {
		return "", 0, err
	}
	return hash, n, nil
}

func (s *Store) Open(hash string) (*os.File, error) {
	p, err := s.path(hash)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (s *Store) Remove(hash string) error {
	p, err := s.path(hash)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func (s *Store) Walk(fn func(string, os.FileInfo) error) error {
	return filepath.WalkDir(filepath.Join(s.root, "blobs"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || !valid(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return fn(d.Name(), info)
	})
}
