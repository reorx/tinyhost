package store

import (
	"errors"
	"testing"
)

// Behavior: rename conflicts roll back without reserving the active name; all prior slugs stay assigned until purge.
func TestRenameIsAtomicAndReservesHistoricalSlugs(t *testing.T) {
	s := openTest(t)
	a := appTest(t, s)
	other, err := s.CreateApp("other-app", "", "private", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RenameApp(a.ID, other.Slug); !errors.Is(err, ErrSlugTaken) {
		t.Fatal(err)
	}
	if err = s.RenameApp(a.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err = s.RenameApp(other.ID, a.Slug); !errors.Is(err, ErrSlugTaken) {
		t.Fatal(err)
	}
	if err = s.RenameApp(a.ID, "third-name"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"report", "renamed", "third-name"} {
		owner, err := s.SlugOwner(name)
		if err != nil || owner == nil || owner.ID != a.ID || owner.Slug != "third-name" {
			t.Fatal(owner, err)
		}
	}
}

// Behavior: deleting a version is atomic with the main-pointer check, and purge/restore act only on trash.
func TestConsoleMutationsProtectLiveAppsAndCurrentVersions(t *testing.T) {
	s := openTest(t)
	a := appTest(t, s)
	deploy(t, s, a, "first", true)
	deploy(t, s, a, "second", true)
	if err := s.DeleteVersion(a.ID, 2); !errors.Is(err, ErrCurrentVersion) {
		t.Fatal(err)
	}
	if err := s.DeleteVersion(a.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveVersion(a.ID, 1); !errors.Is(err, ErrVersionNotFound) {
		t.Fatal(err)
	}
	if err := s.PurgeApp(a.ID); !errors.Is(err, ErrAppNotFound) {
		t.Fatal(err)
	}
	if err := s.RestoreApp(a.ID); !errors.Is(err, ErrAppNotFound) {
		t.Fatal(err)
	}
	if err := s.SoftDeleteApp(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVisibility(a.ID, "public"); !errors.Is(err, ErrAppNotFound) {
		t.Fatal(err)
	}
	if err := s.RestoreApp(a.ID); err != nil {
		t.Fatal(err)
	}
	v, err := s.ResolveVersion(a.ID, 0)
	if err != nil || v.Number != 2 {
		t.Fatal(v, err)
	}
}

// Behavior: audit queries return newest 200 records, preserve history after purge, and filter by stable app ID.
func TestEventQueriesBoundAndFilterHistory(t *testing.T) {
	s := openTest(t)
	a := appTest(t, s)
	for i := 0; i < 205; i++ {
		if err := s.AddEvent("owner", "app.changed", a.ID, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddEvent("owner", "agent.revoked", "", "{}"); err != nil {
		t.Fatal(err)
	}
	all, err := s.Events("")
	if err != nil || len(all) != 200 || all[0].Action != "agent.revoked" {
		t.Fatal(len(all), err)
	}
	filtered, err := s.Events(a.ID)
	if err != nil || len(filtered) != 200 || filtered[0].AppSlug != a.Slug {
		t.Fatal(len(filtered), err)
	}
	if err = s.SoftDeleteApp(a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeApp(a.ID); err != nil {
		t.Fatal(err)
	}
	filtered, err = s.Events(a.ID)
	if err != nil || len(filtered) != 200 || filtered[0].AppID != a.ID {
		t.Fatal(len(filtered), err)
	}
}
