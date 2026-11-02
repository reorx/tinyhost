package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/reorx/tinyhost/internal/slug"
)

var (
	ErrSlugTaken      = errors.New("slug 已被占用")
	ErrCurrentVersion = errors.New("不能删除主入口所在的版本")
	ErrInvalidInput   = errors.New("无效参数")
)

// RenameApp reserves the previous slug in the same transaction as changing it.
func (s *Store) RenameApp(app, name string) error {
	if !slug.Valid(name) {
		return fmt.Errorf("%w: slug 不合法", ErrInvalidInput)
	}
	return s.write(func(tx *sql.Tx) error {
		a, err := activeApp(tx, app)
		if err != nil {
			return err
		}
		if name == a.Slug {
			return nil
		}
		var n int
		if err = tx.QueryRow("SELECT (SELECT count(*) FROM apps WHERE slug=?)+(SELECT count(*) FROM slug_redirects WHERE old_slug=?)", name, name).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrSlugTaken
		}
		now := s.now().Unix()
		if _, err = tx.Exec("INSERT INTO slug_redirects(old_slug,app_id,created_at) VALUES(?,?,?)", a.Slug, a.ID, now); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE apps SET slug=?,updated_at=? WHERE id=?", name, now, a.ID)
		return err
	})
}
func changed(result sql.Result, err error, missing error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return missing
	}
	return nil
}
func (s *Store) SetVisibility(app, visibility string) error {
	if visibility != "public" && visibility != "private" {
		return fmt.Errorf("%w: visibility 不合法", ErrInvalidInput)
	}
	result, err := s.db.Exec("UPDATE apps SET visibility=?,updated_at=? WHERE id=? AND deleted_at IS NULL", visibility, s.now().Unix(), app)
	return changed(result, err, ErrAppNotFound)
}
func (s *Store) DeleteVersion(app string, number int) error {
	return s.write(func(tx *sql.Tx) error {
		a, err := activeApp(tx, app)
		if err != nil {
			return err
		}
		if a.CurrentVersion != nil && *a.CurrentVersion == number {
			return ErrCurrentVersion
		}
		result, err := tx.Exec("DELETE FROM versions WHERE app_id=? AND number=?", app, number)
		return changed(result, err, ErrVersionNotFound)
	})
}
func (s *Store) AppByID(id string) (*App, error) {
	return scanApp(s.db.QueryRow("SELECT "+appCols+" FROM apps WHERE id=?", id))
}
func (s *Store) TrashApps() ([]*App, error) {
	rows, err := s.db.Query("SELECT " + appCols + " FROM apps WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC,id")
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
func (s *Store) RestoreApp(app string) error {
	result, err := s.db.Exec("UPDATE apps SET deleted_at=NULL,updated_at=? WHERE id=? AND deleted_at IS NOT NULL", s.now().Unix(), app)
	return changed(result, err, ErrAppNotFound)
}
func (s *Store) PurgeApp(app string) error {
	result, err := s.db.Exec("DELETE FROM apps WHERE id=? AND deleted_at IS NOT NULL", app)
	return changed(result, err, ErrAppNotFound)
}
func (s *Store) PendingDevices(now int64) ([]*DeviceRequest, error) {
	rows, err := s.db.Query("SELECT "+deviceCols+" FROM device_requests WHERE status='pending' AND expires_at>? ORDER BY created_at,id", now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*DeviceRequest{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) Clients() ([]*Client, error) {
	rows, err := s.db.Query("SELECT id,name,meta,created_at,expires_at,revoked_at,last_used_at,COALESCE(last_used_ip,'') FROM clients ORDER BY created_at DESC,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Client{}
	for rows.Next() {
		var c Client
		if err = rows.Scan(&c.ID, &c.Name, &c.Meta, &c.CreatedAt, &c.ExpiresAt, &c.RevokedAt, &c.LastUsedAt, &c.LastUsedIP); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

type AuditEvent struct {
	ID, At                                           int64
	Actor, ActorName, Action, AppID, AppSlug, Detail string
}

// Events keeps stable app IDs even after the associated app has been purged.
func (s *Store) Events(app string) ([]AuditEvent, error) {
	rows, err := s.db.Query(`SELECT e.id,e.at,e.actor,COALESCE(c.name,e.actor),e.action,COALESCE(e.app_id,''),COALESCE(a.slug,''),e.detail
 FROM events e LEFT JOIN apps a ON a.id=e.app_id LEFT JOIN clients c ON c.id=e.actor
 WHERE (?='' OR e.app_id=?) ORDER BY e.id DESC LIMIT 200`, app, app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		if err = rows.Scan(&e.ID, &e.At, &e.Actor, &e.ActorName, &e.Action, &e.AppID, &e.AppSlug, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
