package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/reorx/tinyhost/internal/bundle"
)

// Behavior: rollback reuses the target package (even from a data version) with main's latest data and never allocates failed attempts.
func TestRollbackReplacesOnlyThePackage(t *testing.T) {
	s := openTest(t)
	a := appTest(t, s)
	v1 := deploy(t, s, a, "good", true)
	push := func(blob string) *Version {
		t.Helper()
		v, err := s.CommitDataVersion(a.ID, "data/", layer("data/x", blob), "", "agent")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v2 := push("old")
	deploy(t, s, a, "bad", true)
	v4 := push("latest")
	if _, err := s.CommitRollback(a.ID, 99, "", "agent"); !errors.Is(err, ErrVersionNotFound) {
		t.Fatal(err)
	}
	v, err := s.CommitRollback(a.ID, v2.Number, "restore", "agent")
	if err != nil || v.Number != 5 || v.Kind != "app" || v.BaseLayer != v1.BaseLayer || !slices.Equal(v.Overlays, v4.Overlays) || *v.RollbackOf != 2 {
		t.Fatal(v, err)
	}
	got, err := s.AppBySlug(a.Slug)
	if err != nil || got.Title != "good" || *got.CurrentVersion != 5 {
		t.Fatal(got, err)
	}
	if _, err := s.CommitRollback(a.ID, 1, "", "agent"); !errors.Is(err, ErrAlreadyCurrent) {
		t.Fatal(err)
	}
	if v := push("next"); v.Number != 6 || v.RollbackOf != nil {
		t.Fatal(v)
	}
}

// Behavior: identical packages renew one preview; publishing snapshots current overlays and GC respects both kinds of reference.
func TestPreviewRenewalPublishingAndExpiry(t *testing.T) {
	s := openTest(t)
	a := appTest(t, s)
	deploy(t, s, a, "base", true)
	files := &Layer{Files: []File{{"z", "z", 2}, {"index.html", "a", 1}}}
	m := bundle.Manifest{Title: "review"}
	p, reused, err := s.CommitAppPreview(a.ID, files, m, "reviewed", "first", 100, 4000)
	if err != nil || reused || len(p.Hash) != 12 {
		t.Fatal(p, reused, err)
	}
	slices.Reverse(files.Files)
	q, reused, err := s.CommitAppPreview(a.ID, files, m, "other", "second", 500, 5000)
	if err != nil || !reused || q.Hash != p.Hash || *q.Layer != *p.Layer || q.ExpiresAt != 5000 || q.Message != "reviewed" || q.CreatedBy != "first" {
		t.Fatal(q, reused, err)
	}
	other, _, err := s.CommitAppPreview(a.ID, files, bundle.Manifest{}, "", "agent", 100, 4000)
	if err != nil || other.Hash == p.Hash {
		t.Fatal(other, err)
	}
	if err := s.DeleteOrphanLayer(*other.Layer); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.OrphanLayers(); err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	data, err := s.CommitDataVersion(a.ID, "data", layer("data/x", "new"), "", "agent")
	if err != nil || data.Number != 2 {
		t.Fatal(data, err)
	}
	v, err := s.PublishAppPreview(a.ID, p.Hash, "", "second", 1000)
	if err != nil || v.Number != 3 || *v.Preview != p.Hash || v.BaseLayer != *p.Layer || v.Manifest.Title != "review" || !slices.Equal(v.Overlays, data.Overlays) || v.Message != p.Message {
		t.Fatal(v, err)
	}
	rows, err := s.AppPreviews(a.ID, 4000)
	if err != nil || len(rows) != 1 || *rows[0].PublishedVersion != 3 {
		t.Fatal(rows, err)
	}
	for _, tc := range []struct {
		hash string
		now  int64
		want error
	}{
		{"missing", 1000, ErrPreviewNotFound}, {p.Hash, 4999, ErrAlreadyCurrent}, {p.Hash, 5000, ErrPreviewExpired},
	} {
		if _, err := s.PublishAppPreview(a.ID, tc.hash, "", "agent", tc.now); !errors.Is(err, tc.want) {
			t.Fatal(err)
		}
	}
	if err := s.PruneAppPreviews(5000); err != nil {
		t.Fatal(err)
	}
	orphans, err := s.OrphanLayers()
	if err != nil || len(orphans) != 1 || orphans[0].ID != *other.Layer {
		t.Fatal(orphans, err)
	}
	if err := s.PruneAppPreviews(5001 + 7*86400); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppPreview(a.ID, p.Hash); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatal(err)
	}
	revived, _, err := s.CommitAppPreview(a.ID, files, m, "", "agent", 1000000, 1100000)
	if err != nil || revived.Hash != p.Hash {
		t.Fatal(revived, err)
	}
	if err := s.SoftDeleteApp(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.PurgeTrash(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppPreview(a.ID, p.Hash); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatal(err)
	}
}

// Behavior: upgrading an existing v3 database preserves main, its package, data layers and anonymous previews.
func TestV3VersionsAndPreviewsSurviveMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(schema + authSchema + previewSchema + `
	INSERT INTO apps(id,slug,visibility,created_by,current_version,next_version,created_at,updated_at)
	VALUES('a','old','private','owner',1,2,1,1);
	INSERT INTO layers VALUES('base',1,1,1),('data',1,1,1);
	INSERT INTO versions(app_id,number,kind,base_layer,created_by,created_at) VALUES('a',1,'app','base','owner',1);
	INSERT INTO version_overlays VALUES('a',1,'data/','data','owner',1);
	INSERT INTO previews(id,token,client_id,local_key,source,layer,created_at,updated_at,expires_at)
	VALUES('p','token','agent','key','site','base',1,1,3600);
	PRAGMA user_version=3;`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v, err := s.ResolveVersion("a", 0)
	if err != nil || v.Number != 1 || v.BaseLayer != "base" || v.Overlays[0].Layer != "data" || v.Preview != nil || v.RollbackOf != nil {
		t.Fatal(v, err)
	}
	p, err := s.PreviewByToken("token")
	if err != nil || p == nil || *p.Layer != "base" {
		t.Fatal(p, err)
	}
}

// Behavior: a pruned preview can be renewed and concurrently published only once; hash collisions preserve the reviewed package.
func TestRenewedPreviewPublishesOnceAcrossConcurrentAttempts(t *testing.T) {
	s := openTest(t)
	a := appTest(t, s)
	files := layer("index.html", "review")
	p, _, err := s.CommitAppPreview(a.ID, files, bundle.Manifest{}, "first", "agent", 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PruneAppPreviews(20); err != nil {
		t.Fatal(err)
	}
	q, reused, err := s.CommitAppPreview(a.ID, files, bundle.Manifest{}, "second", "other", 30, 50)
	if err != nil || !reused || p.Hash != q.Hash || *q.Layer == *p.Layer || q.Message != "first" {
		t.Fatal(q, reused, err)
	}

	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := s.PublishAppPreview(a.ID, p.Hash, "", "agent", 40)
			results <- err
		}()
	}
	first, second := <-results, <-results
	if !(first == nil && errors.Is(second, ErrAlreadyCurrent) || second == nil && errors.Is(first, ErrAlreadyCurrent)) {
		t.Fatal(first, second)
	}
	// Simulate a different full digest with the same 12-character public prefix.
	if _, err := s.db.Exec("UPDATE app_previews SET digest=? WHERE app_id=? AND hash=?", p.Hash+"different", a.ID, p.Hash); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CommitAppPreview(a.ID, files, bundle.Manifest{}, "", "other", 45, 100); !errors.Is(err, ErrPreviewCollision) {
		t.Fatal(err)
	}
	record, err := s.AppPreview(a.ID, p.Hash)
	if err != nil || record.ExpiresAt != 50 || *record.PublishedVersion != 1 || *record.Layer != *q.Layer {
		t.Fatal(record, err)
	}
}
