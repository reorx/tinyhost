package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/reorx/tinyhost/internal/bundle"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "tinyhost.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func appTest(t *testing.T, store *Store) *App {
	t.Helper()
	app, err := store.CreateApp("report", "Title", "public", "c_a")
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func layer(path, blob string) *Layer {
	return &Layer{Files: []File{{Path: path, Blob: blob, Size: 1}}}
}

func deploy(t *testing.T, store *Store, app *App, blob string, promote bool) *Version {
	t.Helper()
	version, err := store.CommitAppVersion(
		app.ID, layer("index.html", blob), bundle.Manifest{Title: blob}, "msg", "c_a", promote,
	)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

// Behavior: SQLite uses WAL, foreign keys, a busy timeout, single connection and persistent idempotent migrations.
func TestDatabaseReopensWithAllSchemaAndPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		key  string
		want any
	}{
		{key: "journal_mode", want: "wal"},
		{key: "foreign_keys", want: 1},
		{key: "busy_timeout", want: 5000},
		{key: "user_version", want: 2},
	} {
		var value any
		if err := store.db.QueryRow("PRAGMA " + test.key).Scan(&value); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(value) != fmt.Sprint(test.want) {
			t.Fatal(test.key, value)
		}
	}
	if store.db.Stats().MaxOpenConnections != 1 {
		t.Fatal("connections")
	}
	app := appTest(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reopened, err := store.AppBySlug(app.Slug)
	if err != nil || reopened.ID != app.ID {
		t.Fatal(reopened, err)
	}
}

// Behavior: client records store only hashes, preserve expiry/revocation and throttle last-used updates to once per minute.
func TestClientsPersistCredentialsAndThrottleTouch(t *testing.T) {
	store := openTest(t)
	now := int64(1000)
	store.now = func() time.Time { return time.Unix(now, 0) }
	expiry := int64(2000)

	client, err := store.CreateClient("agent", "hash", &expiry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateClient("duplicate", "hash", nil); err == nil {
		t.Fatal("duplicate hash")
	}
	record, err := store.ClientByTokenHash("hash")
	if err != nil || record.ID != client.ID || *record.ExpiresAt != expiry || record.Meta != "{}" {
		t.Fatal(record, err)
	}

	if err := store.TouchClient(client.ID, "ip1"); err != nil {
		t.Fatal(err)
	}
	now += 30
	if err := store.TouchClient(client.ID, "ip2"); err != nil {
		t.Fatal(err)
	}
	record, err = store.ClientByTokenHash("hash")
	if err != nil {
		t.Fatal(err)
	}
	if *record.LastUsedAt != 1000 || record.LastUsedIP != "ip1" {
		t.Fatal(record)
	}

	now += 31
	if err := store.TouchClient(client.ID, "ip3"); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeClient(client.ID); err != nil {
		t.Fatal(err)
	}
	record, err = store.ClientByTokenHash("hash")
	if err != nil {
		t.Fatal(err)
	}
	if record.RevokedAt == nil || record.LastUsedIP != "ip3" {
		t.Fatal(record)
	}
	if record, err := store.ClientByTokenHash("absent"); err != nil || record != nil {
		t.Fatal(record, err)
	}
}

// Behavior: app redeploys preserve overlays, no-promote leaves main unchanged, and data after rollback builds on main.
func TestVersionsSnapshotCurrentLayersAndRollback(t *testing.T) {
	store := openTest(t)
	app := appTest(t, store)
	original := deploy(t, store, app, "old", true)

	dataVersion, err := store.CommitDataVersion(app.ID, "data/", layer("data/a.json", "data"), "", "c_b")
	if err != nil {
		t.Fatal(err)
	}
	unpromoted := deploy(t, store, app, "new", false)
	current, err := store.ResolveVersion(app.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if current.Number != dataVersion.Number {
		t.Fatal(current)
	}
	resolved, err := store.ResolveVersion(app.ID, unpromoted.Number)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Overlays) != 1 || resolved.Overlays[0].PushedBy != "c_b" {
		t.Fatal(resolved)
	}

	if err := store.Promote(app.ID, original.Number); err != nil {
		t.Fatal(err)
	}
	afterRollback, err := store.CommitDataVersion(app.ID, "other/", layer("other/x", "x"), "", "c_c")
	if err != nil || afterRollback.BaseLayer != original.BaseLayer || len(afterRollback.Overlays) != 1 {
		t.Fatal(afterRollback, err)
	}

	removed, err := store.CommitDataVersion(app.ID, "other/", nil, "", "c_c")
	if err != nil || len(removed.Overlays) != 0 {
		t.Fatal(removed, err)
	}
	if _, err := store.CommitDataVersion(app.ID, "other/", nil, "", "c_c"); !errors.Is(err, ErrOverlayNotFound) {
		t.Fatal(err)
	}
	if err := store.Promote(app.ID, 999); !errors.Is(err, ErrVersionNotFound) {
		t.Fatal(err)
	}

	resolved, err = store.ResolveVersion(app.ID, original.Number)
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.LookupFile(resolved.BaseLayer, "index.html")
	if err != nil || file.Blob != "old" {
		t.Fatal(file, err)
	}
	if file, err := store.LookupFile(resolved.BaseLayer, "absent"); err != nil || file != nil {
		t.Fatal(file, err)
	}
}

// Behavior: empty apps accept data with an empty base; nested overlays are rejected, while equal directories replace a layer.
func TestDataOnEmptyAppsAndOverlayValidation(t *testing.T) {
	store := openTest(t)
	app := appTest(t, store)

	version, err := store.CommitDataVersion(app.ID, "data", layer("data/x", "x"), "", "c_a")
	if err != nil || version.Number != 1 || len(version.Overlays) != 1 {
		t.Fatal(version, err)
	}
	if file, err := store.LookupFile(version.BaseLayer, "index.html"); err != nil || file != nil {
		t.Fatal(file, err)
	}

	for _, dir := range []string{"data/sub/", "./", "/", "../x", ".well-known/", "x/.hidden/", "x//y", "x\\y"} {
		if _, err := store.CommitDataVersion(app.ID, dir, layer("x", "x"), "", "c_a"); err == nil {
			t.Fatal(dir)
		}
	}
	if _, err := store.CommitDataVersion(app.ID, "data/", layer("data/y", "y"), "", "c_b"); err != nil {
		t.Fatal(err)
	}

	other, err := store.CreateApp("other", "", "private", "c_b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitDataVersion(other.ID, "data/sub/", layer("data/sub/x", "x"), "", "c_a"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitDataVersion(other.ID, "data/", layer("data/x", "x"), "", "c_a"); !errors.Is(err, ErrOverlayConflict) {
		t.Fatal(err)
	}
}

// Behavior: failed layer inserts roll back the layer, version number, title and main pointer together.
func TestFailedCommitsRollBackEveryWrite(t *testing.T) {
	store := openTest(t)
	app := appTest(t, store)
	duplicateFiles := &Layer{Files: []File{{Path: "x", Blob: "x"}, {Path: "x", Blob: "y"}}}

	_, err := store.CommitAppVersion(app.ID, duplicateFiles, bundle.Manifest{Title: "bad"}, "", "c_a", true)
	if err == nil {
		t.Fatal("duplicate files")
	}

	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM layers").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal(count)
	}
	record, err := store.AppBySlug(app.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if record.NextVersion != 1 || record.CurrentVersion != nil || record.Title != "Title" {
		t.Fatal(record)
	}
	if version := deploy(t, store, app, "good", true); version.Number != 1 {
		t.Fatal(version)
	}
}

// Behavior: concurrent publishers allocate unique sequential immutable versions.
func TestConcurrentPublishesAllocateSequentialVersions(t *testing.T) {
	store := openTest(t)
	app := appTest(t, store)
	var publishers sync.WaitGroup

	for i := 0; i < 12; i++ {
		publishers.Add(1)
		go func() {
			defer publishers.Done()
			_, err := store.CommitAppVersion(app.ID, layer("index.html", "x"), bundle.Manifest{}, "", "c_a", true)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	publishers.Wait()

	versions, err := store.Versions(app.ID)
	if err != nil || len(versions) != 12 {
		t.Fatal(len(versions), err)
	}
	for i, version := range versions {
		if version.Number != 12-i {
			t.Fatal(version.Number)
		}
	}
}

// Behavior: trash reserves slugs and rejects writes until purge cascades versions; GC retains app versions and pinned main.
func TestGarbageCollectionPreservesReferencesAndReleasesPurgedSlugs(t *testing.T) {
	store := openTest(t)
	app := appTest(t, store)
	appVersion := deploy(t, store, app, "base", true)
	var pinned int
	for i := 0; i < 25; i++ {
		version, err := store.CommitDataVersion(app.ID, "data/", layer("data/x", fmt.Sprint(i)), "", "c_a")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			pinned = version.Number
		}
	}
	if err := store.Promote(app.ID, pinned); err != nil {
		t.Fatal(err)
	}

	if err := store.PruneDataVersions(20); err != nil {
		t.Fatal(err)
	}
	versions, err := store.Versions(app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 22 {
		t.Fatal(len(versions))
	}
	if _, err := store.ResolveVersion(app.ID, appVersion.Number); err != nil {
		t.Fatal(err)
	}

	orphans, err := store.OrphanLayers()
	if err != nil || len(orphans) != 4 {
		t.Fatal(len(orphans), err)
	}
	for _, orphan := range orphans {
		if err := store.DeleteOrphanLayer(orphan.ID); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := store.ReferencedBlobs()
	if err != nil || !refs["base"] || refs["1"] {
		t.Fatal(refs, err)
	}
	if err := store.DeleteOrphanLayer(appVersion.BaseLayer); err != nil {
		t.Fatal(err)
	}

	if err := store.SoftDeleteApp(app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateApp(app.Slug, "", "public", "c_b"); err == nil {
		t.Fatal("reserved")
	}
	apps, err := store.ListApps()
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatal(apps)
	}
	if _, err := store.ResolveVersion(app.ID, 0); !errors.Is(err, ErrAppNotFound) {
		t.Fatal(err)
	}
	if _, err := store.CommitAppVersion(app.ID, layer("x", "x"), bundle.Manifest{}, "", "c_a", true); !errors.Is(err, ErrAppNotFound) {
		t.Fatal(err)
	}

	if err := store.PurgeTrash(time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	record, err := store.AppBySlug(app.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil {
		t.Fatal("purged early")
	}

	if err := store.PurgeTrash(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	record, err = store.AppBySlug(app.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if record != nil {
		t.Fatal(record)
	}
	if _, err := store.CreateApp(app.Slug, "", "private", "c_b"); err != nil {
		t.Fatal(err)
	}
}

// Behavior: events persist actor, action, app, timestamp and JSON detail.
func TestEventsRecordAuditDetails(t *testing.T) {
	store := openTest(t)

	if err := store.AddEvent("c_a", "app.created", "a_1", `{"slug":"report"}`); err != nil {
		t.Fatal(err)
	}

	var actor, action, detail string
	var at int64
	err := store.db.QueryRow("SELECT actor,action,detail,at FROM events").Scan(&actor, &action, &detail, &at)
	if err != nil || actor != "c_a" || action != "app.created" || detail != `{"slug":"report"}` || at == 0 {
		t.Fatal(actor, action, detail, at, err)
	}
}

// Behavior: an overlay hides the entire base directory even if a requested file is absent there.
func TestOverlayLookupsNeverFallBackToBase(t *testing.T) {
	store := openTest(t)
	app := appTest(t, store)
	base := &Layer{Files: []File{
		{Path: "index.html", Blob: "base", Size: 2},
		{Path: "data/old.json", Blob: "old", Size: 3},
	}}
	manifest := bundle.Manifest{Data: map[string]string{"data/": "spec"}}
	if _, err := store.CommitAppVersion(app.ID, base, manifest, "", "c_a", true); err != nil {
		t.Fatal(err)
	}

	version, err := store.CommitDataVersion(app.ID, "data/", layer("data/new.json", "new"), "", "c_b")
	if err != nil {
		t.Fatal(err)
	}
	if file, err := store.LookupVersionFile(version, "data/old.json"); err != nil || file != nil {
		t.Fatal(file, err)
	}
	if file, err := store.LookupVersionFile(version, "data/new.json"); err != nil || file.Blob != "new" {
		t.Fatal(file, err)
	}
	if file, err := store.LookupVersionFile(version, "index.html"); err != nil || file.Blob != "base" {
		t.Fatal(file, err)
	}

	version, err = store.CommitDataVersion(app.ID, "data/", nil, "", "c_b")
	if err != nil {
		t.Fatal(err)
	}
	if file, err := store.LookupVersionFile(version, "data/old.json"); err != nil || file.Blob != "old" {
		t.Fatal(file, err)
	}
	if version.Manifest.Data["data/"] != "spec" {
		t.Fatal(version.Manifest)
	}
}

// Behavior: a database created by a future schema is rejected rather than silently downgraded.
func TestFutureSchemasAreRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("PRAGMA user_version=3"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if store, err := Open(path); err == nil {
		store.Close()
		t.Fatal("accepted future database")
	}
}

// Behavior: independent database connections serialize publishers without duplicate version numbers or lost main updates.
func TestIndependentStoresSerializePublishers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	app := appTest(t, first)
	var publishers sync.WaitGroup

	for _, store := range []*Store{first, second} {
		publishers.Add(1)
		go func(store *Store) {
			defer publishers.Done()
			for i := 0; i < 5; i++ {
				_, err := store.CommitAppVersion(app.ID, layer("index.html", "content"), bundle.Manifest{}, "", "c_a", true)
				if err != nil {
					t.Error(err)
					return
				}
			}
		}(store)
	}
	publishers.Wait()

	versions, err := first.Versions(app.ID)
	if err != nil || len(versions) != 10 {
		t.Fatal(len(versions), err)
	}
	current, err := second.ResolveVersion(app.ID, 0)
	if err != nil || current.Number != 10 {
		t.Fatal(current, err)
	}
}
