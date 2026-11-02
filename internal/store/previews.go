package store

import (
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base32"
	"errors"
	"strings"
)

//go:embed preview_schema.sql
var previewSchema string

// Preview tokens are intentionally stored in plaintext so Owner can display
// short-lived public links. Unlike sessions, they never authorize mutations.
type Preview struct {
	ID, Token, ClientID, LocalKey, Source string
	Layer                                 *string
	CreatedAt, UpdatedAt, ExpiresAt       int64
}

const previewCols = "id,token,client_id,local_key,source,layer,created_at,updated_at,expires_at"

func scanPreview(row scanner) (*Preview, error) {
	var p Preview
	err := row.Scan(&p.ID, &p.Token, &p.ClientID, &p.LocalKey, &p.Source, &p.Layer, &p.CreatedAt, &p.UpdatedAt, &p.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CommitPreview atomically replaces the layer or creates an independent preview.
// The caller holds the upload lifecycle lock from blob publication through commit.
func (s *Store) CommitPreview(client, key, source string, layer *Layer, now, expires int64) (*Preview, bool, error) {
	var p *Preview
	reused := false
	err := s.write(func(tx *sql.Tx) error {
		var err error
		p, err = scanPreview(tx.QueryRow("SELECT "+previewCols+" FROM previews WHERE client_id=? AND local_key=?", client, key))
		if err != nil {
			return err
		}
		layerID, err := insertLayer(tx, layer, now)
		if err != nil {
			return err
		}
		reused = p != nil
		if p == nil {
			// 13 bytes contain 104 random bits; the first 20 base32 symbols use 100.
			random := make([]byte, 13)
			if _, err = rand.Read(random); err != nil {
				return err
			}
			p = &Preview{ID: id("p_"), Token: strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random))[:20], ClientID: client, LocalKey: key, CreatedAt: now}
			_, err = tx.Exec("INSERT INTO previews("+previewCols+") VALUES(?,?,?,?,?,?,?,?,?)", p.ID, p.Token, client, key, source, layerID, now, now, expires)
		} else {
			_, err = tx.Exec("UPDATE previews SET source=?,layer=?,updated_at=?,expires_at=? WHERE id=?", source, layerID, now, expires, p.ID)
		}
		if err != nil {
			return err
		}
		p.Source = source
		p.Layer = &layerID
		p.UpdatedAt = now
		p.ExpiresAt = expires
		return nil
	})
	return p, reused, err
}

func (s *Store) PreviewByToken(value string) (*Preview, error) {
	return scanPreview(s.db.QueryRow("SELECT "+previewCols+" FROM previews WHERE token=?", value))
}

// Previews with a client filter lists only that client's active links; Owner's
// empty filter includes tombstones so expired links can also be deleted.
func (s *Store) Previews(client string, now int64) ([]*Preview, error) {
	query := "SELECT " + previewCols + " FROM previews"
	var args []any
	if client != "" {
		query += " WHERE client_id=? AND expires_at>?"
		args = []any{client, now}
	}
	rows, err := s.db.Query(query+" ORDER BY updated_at DESC,id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Preview{}
	for rows.Next() {
		p, err := scanPreview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePreview hides other clients' IDs as missing; empty client is Owner only.
func (s *Store) DeletePreview(id, client string) (bool, error) {
	query := "DELETE FROM previews WHERE id=?"
	args := []any{id}
	if client != "" {
		query += " AND client_id=?"
		args = append(args, client)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func (s *Store) PrunePreviews(now int64) error {
	return s.write(func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE previews SET layer=NULL WHERE expires_at<=?", now); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM previews WHERE expires_at<?", now-7*24*60*60)
		return err
	})
}
