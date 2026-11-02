package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Behavior: a real M2 database upgrades idempotently to M3 without losing clients or settings.
func TestM2DatabaseUpgradesWithPreviews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema + authSchema + "; PRAGMA user_version=2;"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO settings(key,value) VALUES('owner_password_hash','existing')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if value, err := s.Setting("owner_password_hash"); err != nil || value != "existing" {
			t.Fatal(value, err)
		}
		rows, err := s.Previews("", time.Now().Unix())
		if err != nil || len(rows) != 0 {
			t.Fatal(rows, err)
		}
		s.Close()
	}
}

// Behavior: active previews protect their layers against both orphan listing and stale GC deletion; replacement releases only the old layer.
func TestPreviewLayersProtectGCReferences(t *testing.T) {
	s := openTest(t)
	p, _, err := s.CommitPreview("client", "key", "site", layer("index.html", "one"), 100, 3700)
	if err != nil {
		t.Fatal(err)
	}
	old := *p.Layer
	if err = s.DeleteOrphanLayer(old); err != nil {
		t.Fatal(err)
	}
	if file, err := s.LookupFile(old, "index.html"); err != nil || file == nil {
		t.Fatal(file, err)
	}
	if rows, err := s.OrphanLayers(); err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	p, _, err = s.CommitPreview("client", "key", "site", layer("index.html", "two"), 200, 3800)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.OrphanLayers()
	if err != nil || len(rows) != 1 || rows[0].ID != old {
		t.Fatal(rows, err)
	}
	if err = s.PrunePreviews(3800); err != nil {
		t.Fatal(err)
	}
	p, err = s.PreviewByToken(p.Token)
	if err != nil || p.Layer != nil {
		t.Fatal(p, err)
	}
	rows, err = s.OrphanLayers()
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
}

// Behavior: a failed preview layer transaction preserves the old link, contents and expiry.
func TestInvalidPreviewReplacementRollsBack(t *testing.T) {
	s := openTest(t)
	p, _, err := s.CommitPreview("c", "k", "s", layer("index.html", "old"), 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	broken := &Layer{Files: []File{{Path: "index.html", Blob: "a", Size: 1}, {Path: "index.html", Blob: "b", Size: 1}}}
	if _, _, err = s.CommitPreview("c", "k", "s", broken, 150, 300); err == nil {
		t.Fatal("accepted duplicate files")
	}
	got, err := s.PreviewByToken(p.Token)
	if err != nil || *got.Layer != *p.Layer || got.ExpiresAt != 200 {
		t.Fatal(got, err)
	}
	if rows, err := s.OrphanLayers(); err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

// Behavior: concurrent updates to the same client/key commit one stable link without orphan preview rows.
func TestConcurrentPreviewUpdatesKeepOneLink(t *testing.T) {
	s := openTest(t)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if _, _, err := s.CommitPreview("c", "k", "site", layer("index.html", "x"), 100, 200); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	rows, err := s.Previews("c", 100)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
}

// Behavior: the database containing public preview capabilities is owner-readable and writable only, including on reopen.
func TestDatabasePermissionsProtectPreviewTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	info, err = os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
}
