package store

import (
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/reorx/tinyhost/internal/bundle"
)

//go:embed app_preview_schema.sql
var appPreviewSchema string

type AppPreview struct {
	AppID, Hash, digest, Message, CreatedBy string
	Layer                                   *string
	Manifest                                bundle.Manifest
	CreatedAt, UpdatedAt, ExpiresAt         int64
	PublishedVersion                        *int
}

const appPreviewCols = "app_id,hash,digest,layer,manifest,message,created_by,created_at,updated_at,expires_at,published_version"

// PreviewHash returns the public reference and full digest for a package.
// manifest is the canonical JSON encoding of bundle.Manifest.
func PreviewHash(files []File, manifest json.RawMessage) (short, full string) {
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	h := sha256.New()
	for _, f := range ordered {
		fmt.Fprintf(h, "%s\x00%s\n", f.Path, f.Blob)
	}
	h.Write(manifest)
	full = hex.EncodeToString(h.Sum(nil))
	return full[:12], full
}

func scanAppPreview(row scanner) (*AppPreview, error) {
	var p AppPreview
	var manifest string
	err := row.Scan(&p.AppID, &p.Hash, &p.digest, &p.Layer, &manifest, &p.Message, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt, &p.ExpiresAt, &p.PublishedVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPreviewNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(manifest), &p.Manifest); err != nil {
		return nil, err
	}
	return &p, nil
}

func appPreview(q queryer, app, hash string) (*AppPreview, error) {
	return scanAppPreview(q.QueryRow("SELECT "+appPreviewCols+" FROM app_previews WHERE app_id=? AND hash=?", app, hash))
}

func (s *Store) AppPreview(app, hash string) (*AppPreview, error) {
	return appPreview(s.db, app, hash)
}

func (s *Store) CommitAppPreview(app string, layer *Layer, manifest bundle.Manifest, msg, by string, now, expires int64) (*AppPreview, bool, error) {
	if layer == nil {
		return nil, false, fmt.Errorf("layer is required")
	}
	meta, err := json.Marshal(manifest)
	if err != nil {
		return nil, false, err
	}
	hash, digest := PreviewHash(layer.Files, meta)
	var p *AppPreview
	var reused bool
	err = s.write(func(tx *sql.Tx) error {
		if _, err := activeApp(tx, app); err != nil {
			return err
		}
		existing, err := appPreview(tx, app, hash)
		if err != nil && !errors.Is(err, ErrPreviewNotFound) {
			return err
		}
		reused = existing != nil
		if reused && existing.digest != digest {
			return ErrPreviewCollision
		}
		var key string
		if reused && existing.Layer != nil {
			key = *existing.Layer
		} else {
			key, err = insertLayer(tx, layer, now)
			if err != nil {
				return err
			}
		}
		_, err = tx.Exec(`INSERT INTO app_previews(`+appPreviewCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,NULL)
			ON CONFLICT(app_id,hash) DO UPDATE SET layer=excluded.layer,updated_at=excluded.updated_at,
			expires_at=max(app_previews.expires_at,excluded.expires_at)`, app, hash, digest, key, string(meta), msg, by, now, now, expires)
		if err != nil {
			return err
		}
		p, err = appPreview(tx, app, hash)
		return err
	})
	return p, reused, err
}

func (s *Store) AppPreviews(app string, now int64) ([]*AppPreview, error) {
	rows, err := s.db.Query("SELECT "+appPreviewCols+" FROM app_previews WHERE app_id=? AND expires_at>? ORDER BY updated_at DESC,hash", app, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*AppPreview{}
	for rows.Next() {
		p, err := scanAppPreview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PublishAppPreview(app, hash, msg, by string, now int64) (*Version, error) {
	var v *Version
	err := s.write(func(tx *sql.Tx) error {
		a, err := activeApp(tx, app)
		if err != nil {
			return err
		}
		p, err := appPreview(tx, app, hash)
		if err != nil {
			return err
		}
		if p.ExpiresAt <= now || p.Layer == nil {
			return ErrPreviewExpired
		}
		if msg == "" {
			msg = p.Message
		}
		v = &Version{AppID: app, Number: a.NextVersion, Kind: "app", BaseLayer: *p.Layer,
			Manifest: p.Manifest, Message: msg, CreatedBy: by, CreatedAt: now, Preview: &hash}
		if err = commitFromLayer(tx, a, v, true); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE app_previews SET published_version=? WHERE app_id=? AND hash=?", v.Number, app, hash)
		return err
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Store) PruneAppPreviews(now int64) error {
	return s.write(func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE app_previews SET layer=NULL WHERE layer IS NOT NULL AND expires_at<=?", now); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM app_previews WHERE expires_at<=?", now-7*86400)
		return err
	})
}
