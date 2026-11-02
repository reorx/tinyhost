// Package store persists tinyhost metadata in SQLite. Upload blobs before
// entering a publishing transaction; submit the complete new layer atomically.
package store

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/reorx/tinyhost/internal/bundle"
	"github.com/reorx/tinyhost/internal/token"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

var (
	ErrAppNotFound     = errors.New("app not found")
	ErrVersionNotFound = errors.New("version not found")
	ErrOverlayConflict = errors.New("overlay conflict")
	ErrOverlayNotFound = errors.New("overlay not found")
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

type Client struct {
	ID, Name, TokenHash, Meta         string
	CreatedAt                        int64
	ExpiresAt, RevokedAt, LastUsedAt *int64
	LastUsedIP                       string
}

type App struct {
	ID, Slug, Title, Visibility, CreatedBy string
	CurrentVersion                        *int
	NextVersion                           int
	CreatedAt, UpdatedAt                  int64
	DeletedAt                             *int64
}

type File struct {
	Path, Blob string
	Size       int64
}

type Layer struct {
	ID                   string
	FileCount            int
	TotalSize, CreatedAt int64
	Files                []File
}

type Overlay struct {
	Dir, Layer, PushedBy string
	PushedAt             int64
}

type Version struct {
	AppID              string
	Number             int
	Kind, BaseLayer    string
	Manifest           bundle.Manifest
	DataDir            *string
	Message, CreatedBy string
	CreatedAt          int64
	Overlays           []Overlay
}

func id(prefix string) string { return prefix + token.New()[3:] }

func Open(path string) (*Store, error) {
	dsn := (&url.URL{Scheme: "file", Path: path}).String() +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, now: time.Now}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func migrate(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&n); err != nil {
		return err
	}
	if n > 1 {
		return fmt.Errorf("database schema %d is newer than supported version 1", n)
	}
	if n == 0 {
		if _, err = tx.Exec(schema); err != nil {
			return err
		}
		if _, err = tx.Exec("PRAGMA user_version=1"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// write makes all multi-statement mutations atomic, including layer insertion.
func (s *Store) write(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner interface{ Scan(...any) error }

func scanApp(row scanner) (*App, error) {
	var a App
	err := row.Scan(&a.ID, &a.Slug, &a.Title, &a.Visibility, &a.CreatedBy,
		&a.CurrentVersion, &a.NextVersion, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

const appCols = "id,slug,title,visibility,created_by,current_version,next_version,created_at,updated_at,deleted_at"

func (s *Store) CreateClient(name, hash string, expiresAt *int64) (*Client, error) {
	c := &Client{
		ID: id("c_"), Name: name, TokenHash: hash, Meta: "{}",
		CreatedAt: s.now().Unix(), ExpiresAt: expiresAt,
	}
	_, err := s.db.Exec("INSERT INTO clients(id,name,token_hash,created_at,expires_at) VALUES(?,?,?,?,?)",
		c.ID, c.Name, c.TokenHash, c.CreatedAt, c.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ClientByTokenHash returns records including revoked/expired timestamps;
// authentication policy belongs to the HTTP layer. Missing hashes return nil.
func (s *Store) ClientByTokenHash(hash string) (*Client, error) {
	var c Client
	var ip sql.NullString
	err := s.db.QueryRow(`SELECT id,name,token_hash,meta,created_at,expires_at,
		revoked_at,last_used_at,last_used_ip FROM clients WHERE token_hash=?`, hash).
		Scan(&c.ID, &c.Name, &c.TokenHash, &c.Meta, &c.CreatedAt,
			&c.ExpiresAt, &c.RevokedAt, &c.LastUsedAt, &ip)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.LastUsedIP = ip.String
	return &c, nil
}

func (s *Store) TouchClient(client, ip string) error {
	n := s.now().Unix()
	_, err := s.db.Exec(`UPDATE clients SET last_used_at=?,last_used_ip=?
		WHERE id=? AND (last_used_at IS NULL OR last_used_at<=?)`, n, ip, client, n-60)
	return err
}

func (s *Store) RevokeClient(client string) error {
	_, err := s.db.Exec("UPDATE clients SET revoked_at=? WHERE id=? AND revoked_at IS NULL", s.now().Unix(), client)
	return err
}

func (s *Store) CreateApp(slug, title, visibility, by string) (*App, error) {
	n := s.now().Unix()
	a := &App{ID: id("a_"), Slug: slug, Title: title, Visibility: visibility,
		CreatedBy: by, NextVersion: 1, CreatedAt: n, UpdatedAt: n}
	_, err := s.db.Exec("INSERT INTO apps(id,slug,title,visibility,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?)",
		a.ID, slug, title, visibility, by, n, n)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// AppBySlug includes trash so callers can report reserved slug ownership.
func (s *Store) AppBySlug(slug string) (*App, error) {
	return scanApp(s.db.QueryRow("SELECT "+appCols+" FROM apps WHERE slug=?", slug))
}

func (s *Store) ListApps() ([]*App, error) {
	rows, err := s.db.Query("SELECT " + appCols + " FROM apps WHERE deleted_at IS NULL ORDER BY updated_at DESC,slug")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*App{}
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SoftDeleteApp(app string) error {
	n := s.now().Unix()
	r, err := s.db.Exec("UPDATE apps SET deleted_at=?,updated_at=? WHERE id=? AND deleted_at IS NULL", n, n, app)
	if err != nil {
		return err
	}
	count, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrAppNotFound
	}
	return nil
}

func (s *Store) AddEvent(actor, action, app, detail string) error {
	_, err := s.db.Exec("INSERT INTO events(at,actor,action,app_id,detail) VALUES(?,?,?,?,?)",
		s.now().Unix(), actor, action, app, detail)
	return err
}
