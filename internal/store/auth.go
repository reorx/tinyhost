package store

import (
	"database/sql"
	_ "embed"
	"errors"
)

//go:embed auth_schema.sql
var authSchema string

func (s *Store) Setting(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return err
}

type DeviceRequest struct {
	ID, DeviceCodeHash, UserCode, Name, Meta, Status string
	ApprovedName                                     *string
	TokenTTL                                         *int64
	ClientID                                         *string
	CreatedAt, ExpiresAt                             int64
	LastPolledAt                                     *int64
}

const deviceCols = "id,device_code_hash,user_code,name,meta,status,approved_name,token_ttl,client_id,created_at,expires_at,last_polled_at"

func scanDevice(row scanner) (*DeviceRequest, error) {
	var d DeviceRequest
	err := row.Scan(&d.ID, &d.DeviceCodeHash, &d.UserCode, &d.Name, &d.Meta, &d.Status, &d.ApprovedName, &d.TokenTTL, &d.ClientID, &d.CreatedAt, &d.ExpiresAt, &d.LastPolledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}
func (s *Store) CreateDevice(hash, code, name, meta string, now int64) (*DeviceRequest, error) {
	d := &DeviceRequest{ID: id("d_"), DeviceCodeHash: hash, UserCode: code, Name: name, Meta: meta, Status: "pending", CreatedAt: now, ExpiresAt: now + 600}
	_, err := s.db.Exec("INSERT INTO device_requests(id,device_code_hash,user_code,name,meta,status,created_at,expires_at) VALUES(?,?,?,?,?,'pending',?,?)", d.ID, hash, code, name, meta, now, d.ExpiresAt)
	return d, err
}
func (s *Store) DeviceByCode(code string) (*DeviceRequest, error) {
	return scanDevice(s.db.QueryRow("SELECT "+deviceCols+" FROM device_requests WHERE user_code=?", code))
}
func (s *Store) DecideDevice(device, status, name string, ttl *int64, now int64) error {
	result, err := s.db.Exec("UPDATE device_requests SET status=?,approved_name=?,token_ttl=? WHERE id=? AND status='pending' AND expires_at>?", status, name, ttl, device, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("申请不存在、已过期或已处理")
	}
	return err
}
func (s *Store) PollDevice(hash, tokenHash string, now int64) (*Client, string, error) {
	var c *Client
	code := ""
	err := s.write(func(tx *sql.Tx) error {
		d, err := scanDevice(tx.QueryRow("SELECT "+deviceCols+" FROM device_requests WHERE device_code_hash=?", hash))
		if err != nil {
			return err
		}
		if d == nil || d.ExpiresAt <= now || d.Status == "consumed" {
			code = "expired_token"
			return nil
		}
		if d.LastPolledAt != nil && now-*d.LastPolledAt < 2 {
			code = "slow_down"
			return nil
		}
		if _, err = tx.Exec("UPDATE device_requests SET last_polled_at=? WHERE id=?", now, d.ID); err != nil {
			return err
		}
		switch d.Status {
		case "pending":
			code = "authorization_pending"
			return nil
		case "denied":
			code = "access_denied"
			return nil
		}
		c = &Client{ID: id("c_"), Name: *d.ApprovedName, Meta: d.Meta, TokenHash: tokenHash, CreatedAt: now}
		if d.TokenTTL != nil {
			expiry := now + *d.TokenTTL
			c.ExpiresAt = &expiry
		}
		if _, err = tx.Exec("INSERT INTO clients(id,name,meta,token_hash,created_at,expires_at) VALUES(?,?,?,?,?,?)", c.ID, c.Name, c.Meta, tokenHash, now, c.ExpiresAt); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE device_requests SET status='consumed',client_id=? WHERE id=?", c.ID, d.ID)
		return err
	})
	return c, code, err
}
func (s *Store) CreateSession(hash, kind, ip, ua string, now int64) error {
	_, err := s.db.Exec("INSERT INTO sessions(id_hash,kind,created_at,expires_at,ip,user_agent) VALUES(?,?,?,?,?,?)", hash, kind, now, now+30*86400, ip, ua)
	return err
}
func (s *Store) ValidSession(hash, kind string, now int64) (bool, error) {
	var n int
	err := s.db.QueryRow("SELECT count(*) FROM sessions WHERE id_hash=? AND kind=? AND expires_at>?", hash, kind, now).Scan(&n)
	return n == 1, err
}
func (s *Store) DeleteSession(hash string) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE id_hash=?", hash)
	return err
}
func (s *Store) DeleteAllSessions() error { _, err := s.db.Exec("DELETE FROM sessions"); return err }
func (s *Store) CreateTicket(hash, next string, now int64) error {
	_, err := s.db.Exec("INSERT INTO view_tickets(ticket_hash,next_path,expires_at) VALUES(?,?,?)", hash, next, now+60)
	return err
}
func (s *Store) ConsumeTicket(hash, next string, now int64) (bool, error) {
	r, err := s.db.Exec("UPDATE view_tickets SET used_at=? WHERE ticket_hash=? AND next_path=? AND expires_at>? AND used_at IS NULL", now, hash, next, now)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
func (s *Store) PruneAuth(now int64) error {
	return s.write(func(tx *sql.Tx) error {
		for _, q := range []struct {
			sql    string
			cutoff int64
		}{{"DELETE FROM device_requests WHERE expires_at<?", now - 86400}, {"DELETE FROM sessions WHERE expires_at<=?", now}, {"DELETE FROM view_tickets WHERE expires_at<=?", now}} {
			if _, err := tx.Exec(q.sql, q.cutoff); err != nil {
				return err
			}
		}
		return nil
	})
}

// SlugOwner includes redirects for allocation checks, without changing content lookup.
func (s *Store) SlugOwner(slug string) (*App, error) {
	return scanApp(s.db.QueryRow("SELECT "+appCols+" FROM apps WHERE slug=? OR id=(SELECT app_id FROM slug_redirects WHERE old_slug=?)", slug, slug))
}
