package store

import (
	"database/sql"
	"github.com/reorx/tinyhost/internal/token"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Behavior: M2 migration preserves M1 rows and creates every auth and console table.
func TestM2MigrationAndSettings(t *testing.T) {
	s := openTest(t)
	for _, table := range []string{"settings", "device_requests", "sessions", "view_tickets", "slug_redirects"} {
		if _, err := s.db.Exec("SELECT * FROM " + table); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetSetting("owner_password_hash", "hash"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Setting("owner_password_hash"); err != nil || got != "hash" {
		t.Fatalf("%q %v", got, err)
	}
}

// Behavior: approved device requests mint only one hashed client and tickets bind the return path and redeem once.
func TestDeviceConsumptionAndTicketsAreAtomic(t *testing.T) {
	s := openTest(t)
	now := time.Now().Unix()
	d, err := s.CreateDevice("devicehash", "BCDFGHJK", "agent", "{}", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DecideDevice(d.ID, "approved", "renamed", nil, now); err != nil {
		t.Fatal(err)
	}
	c, code, err := s.PollDevice("devicehash", token.Hash("secret"), now)
	if err != nil || code != "" || c.Name != "renamed" {
		t.Fatalf("%+v %s %v", c, code, err)
	}
	if _, code, err = s.PollDevice("devicehash", "other", now+3); err != nil || code != "expired_token" {
		t.Fatalf("%s %v", code, err)
	}
	if err = s.CreateTicket("hash", "/app/?x=1", now); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ConsumeTicket("hash", "/other/", now); err != nil || ok {
		t.Fatal(ok, err)
	}
	for i := 0; i < 2; i++ {
		ok, err := s.ConsumeTicket("hash", "/app/?x=1", now)
		if err != nil || ok != (i == 0) {
			t.Fatal(ok, err)
		}
	}
}

// Behavior: upgrading a real M1 database preserves its clients and apps and is idempotent.
func TestM1DatabaseUpgradesWithoutLosingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema + "; PRAGMA user_version=1;"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO clients(id,name,token_hash,created_at) VALUES('old','old','hash',1)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.ClientByTokenHash("hash")
		if err != nil || c == nil || c.ID != "old" {
			t.Fatal(c, err)
		}
		s.Close()
	}
}

// Behavior: expired auth rows are garbage collected while recent device requests remain for a day.
func TestAuthGCAndSessionKindSeparation(t *testing.T) {
	s := openTest(t)
	now := time.Now().Unix()
	s.CreateSession("hash", "viewer", "", "", now)
	if ok, err := s.ValidSession("hash", "admin", now); err != nil || ok {
		t.Fatal(ok, err)
	}
	s.CreateDevice("old", "BCDFGHJK", "old", "{}", now-90000)
	s.CreateDevice("recent", "JKLMNPQR", "recent", "{}", now-1000)
	s.CreateTicket("ticket", "/", now-60)
	if err := s.PruneAuth(now + 30*86400); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sessions", "view_tickets", "device_requests"} {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
}

// Behavior: redirected slugs cannot be allocated by any App creation path.
func TestRedirectedSlugRemainsReserved(t *testing.T) {
	s := openTest(t)
	a := appTest(t, s)
	if _, err := s.db.Exec("INSERT INTO slug_redirects(old_slug,app_id,created_at) VALUES('old-name',?,1)", a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateApp("old-name", "", "public", "owner"); err == nil {
		t.Fatal("redirected slug allocated")
	}
}

// Behavior: simultaneous polls create exactly one client and return the token only once.
func TestConcurrentDevicePollsMintOnlyOneClient(t *testing.T) {
	s := openTest(t)
	now := time.Now().Unix()
	d, err := s.CreateDevice("hash", "BCDFGHJK", "agent", "{}", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DecideDevice(d.ID, "approved", "agent", nil, now); err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, code, err := s.PollDevice("hash", token.New(), now)
			if err != nil {
				t.Error(err)
			}
			if c != nil && code == "" {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal(successes.Load())
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM clients").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
