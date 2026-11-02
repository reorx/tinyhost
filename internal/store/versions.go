package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/reorx/tinyhost/internal/bundle"
)

type queryer interface {
	QueryRow(string, ...any) *sql.Row
	Query(string, ...any) (*sql.Rows, error)
}

func activeApp(q queryer, app string) (*App, error) {
	a, err := scanApp(q.QueryRow("SELECT "+appCols+" FROM apps WHERE id=? AND deleted_at IS NULL", app))
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrAppNotFound
	}
	return a, nil
}

const versionCols = "app_id,number,kind,base_layer,manifest,data_dir,message,created_by,created_at"

func scanVersion(row scanner) (*Version, error) {
	var v Version
	var manifest string
	err := row.Scan(&v.AppID, &v.Number, &v.Kind, &v.BaseLayer, &manifest,
		&v.DataDir, &v.Message, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(manifest), &v.Manifest); err != nil {
		return nil, err
	}
	return &v, nil
}

func resolve(q queryer, a *App, n int) (*Version, error) {
	if n == 0 {
		if a.CurrentVersion == nil {
			return nil, ErrVersionNotFound
		}
		n = *a.CurrentVersion
	}
	v, err := scanVersion(q.QueryRow("SELECT "+versionCols+" FROM versions WHERE app_id=? AND number=?", a.ID, n))
	if err != nil {
		return nil, err
	}
	rows, err := q.Query("SELECT dir,layer,pushed_by,pushed_at FROM version_overlays WHERE app_id=? AND number=? ORDER BY dir", a.ID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v.Overlays = []Overlay{}
	for rows.Next() {
		var overlay Overlay
		if err = rows.Scan(&overlay.Dir, &overlay.Layer, &overlay.PushedBy, &overlay.PushedAt); err != nil {
			return nil, err
		}
		v.Overlays = append(v.Overlays, overlay)
	}
	return v, rows.Err()
}

func insertLayer(tx *sql.Tx, layer *Layer, at int64) (string, error) {
	if layer == nil {
		return "", fmt.Errorf("layer is required")
	}
	key := id("l_")
	var size int64
	for _, file := range layer.Files {
		if file.Size < 0 {
			return "", fmt.Errorf("negative file size")
		}
		size += file.Size
	}
	if _, err := tx.Exec("INSERT INTO layers(id,file_count,total_size,created_at) VALUES(?,?,?,?)",
		key, len(layer.Files), size, at); err != nil {
		return "", err
	}
	for _, file := range layer.Files {
		if _, err := tx.Exec("INSERT INTO layer_files(layer_id,path,blob,size) VALUES(?,?,?,?)",
			key, file.Path, file.Blob, file.Size); err != nil {
			return "", err
		}
	}
	return key, nil
}

func insertVersion(tx *sql.Tx, v *Version, promote bool, title *string) error {
	manifest, err := json.Marshal(v.Manifest)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO versions("+versionCols+") VALUES(?,?,?,?,?,?,?,?,?)",
		v.AppID, v.Number, v.Kind, v.BaseLayer, string(manifest), v.DataDir,
		v.Message, v.CreatedBy, v.CreatedAt); err != nil {
		return err
	}
	for _, overlay := range v.Overlays {
		if _, err = tx.Exec("INSERT INTO version_overlays(app_id,number,dir,layer,pushed_by,pushed_at) VALUES(?,?,?,?,?,?)",
			v.AppID, v.Number, overlay.Dir, overlay.Layer, overlay.PushedBy, overlay.PushedAt); err != nil {
			return err
		}
	}
	var current any
	if promote {
		current = v.Number
	}
	_, err = tx.Exec(`UPDATE apps SET next_version=next_version+1,
		current_version=COALESCE(?,current_version),title=COALESCE(?,title),updated_at=? WHERE id=?`,
		current, title, v.CreatedAt, v.AppID)
	return err
}

func (s *Store) CommitAppVersion(app string, layer *Layer, manifest bundle.Manifest, msg, by string, promote bool) (*Version, error) {
	var out *Version
	err := s.write(func(tx *sql.Tx) error {
		a, err := activeApp(tx, app)
		if err != nil {
			return err
		}
		at := s.now().Unix()
		base, err := insertLayer(tx, layer, at)
		if err != nil {
			return err
		}
		v := &Version{AppID: app, Number: a.NextVersion, Kind: "app", BaseLayer: base,
			Manifest: manifest, Message: msg, CreatedBy: by, CreatedAt: at, Overlays: []Overlay{}}
		if a.CurrentVersion != nil {
			old, err := resolve(tx, a, 0)
			if err != nil {
				return err
			}
			v.Overlays = old.Overlays
		}
		if err = insertVersion(tx, v, promote, &manifest.Title); err != nil {
			return err
		}
		out = v
		return nil
	})
	return out, err
}

// NormalizeOverlayDir accepts a relative directory and returns its trailing-/ form.
func NormalizeOverlayDir(dir string) (string, error) {
	d := strings.TrimSuffix(dir, "/")
	if d == "" || strings.ContainsAny(d, "\\\x00") || len(d) > 511 {
		return "", fmt.Errorf("invalid overlay directory: %q", dir)
	}
	for _, part := range strings.Split(d, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("invalid overlay directory: %q", dir)
		}
	}
	return d + "/", nil
}

func replaceOverlay(tx *sql.Tx, v *Version, dir string, layer *Layer) error {
	overlays := []Overlay{}
	found := false
	for _, overlay := range v.Overlays {
		if overlay.Dir == dir {
			found = true
			continue
		}
		if strings.HasPrefix(dir, overlay.Dir) || strings.HasPrefix(overlay.Dir, dir) {
			return fmt.Errorf("%w: %s", ErrOverlayConflict, overlay.Dir)
		}
		overlays = append(overlays, overlay)
	}
	if layer == nil && !found {
		return ErrOverlayNotFound
	}
	if layer != nil {
		key, err := insertLayer(tx, layer, v.CreatedAt)
		if err != nil {
			return err
		}
		overlays = append(overlays, Overlay{Dir: dir, Layer: key, PushedBy: v.CreatedBy, PushedAt: v.CreatedAt})
	}
	v.Overlays = overlays
	return nil
}

func (s *Store) CommitDataVersion(app, dir string, layer *Layer, msg, by string) (*Version, error) {
	d, err := NormalizeOverlayDir(dir)
	if err != nil {
		return nil, err
	}
	var out *Version
	err = s.write(func(tx *sql.Tx) error {
		a, err := activeApp(tx, app)
		if err != nil {
			return err
		}
		v := &Version{AppID: app, Number: a.NextVersion, Kind: "data", DataDir: &d,
			Message: msg, CreatedBy: by, CreatedAt: s.now().Unix(), Overlays: []Overlay{}}
		if a.CurrentVersion != nil {
			old, err := resolve(tx, a, 0)
			if err != nil {
				return err
			}
			v.BaseLayer, v.Manifest, v.Overlays = old.BaseLayer, old.Manifest, old.Overlays
		} else {
			v.BaseLayer, err = insertLayer(tx, &Layer{}, v.CreatedAt)
			if err != nil {
				return err
			}
		}
		if err = replaceOverlay(tx, v, d, layer); err != nil {
			return err
		}
		if err = insertVersion(tx, v, true, nil); err != nil {
			return err
		}
		out = v
		return nil
	})
	return out, err
}

func (s *Store) Promote(app string, n int) error {
	return s.write(func(tx *sql.Tx) error {
		a, err := activeApp(tx, app)
		if err != nil {
			return err
		}
		if n <= 0 {
			return ErrVersionNotFound
		}
		if _, err = resolve(tx, a, n); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE apps SET current_version=?,updated_at=? WHERE id=?", n, s.now().Unix(), app)
		return err
	})
}

// ResolveVersion uses n=0 for main. A read transaction keeps pointer and layers consistent.
func (s *Store) ResolveVersion(app string, n int) (*Version, error) {
	var v *Version
	err := s.write(func(tx *sql.Tx) error {
		a, err := activeApp(tx, app)
		if err != nil {
			return err
		}
		v, err = resolve(tx, a, n)
		return err
	})
	return v, err
}

// Versions returns metadata newest first; ResolveVersion loads overlays.
func (s *Store) Versions(app string) ([]*Version, error) {
	rows, err := s.db.Query("SELECT "+versionCols+" FROM versions WHERE app_id=? ORDER BY number DESC", app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) LookupFile(layer, path string) (*File, error) {
	var f File
	err := s.db.QueryRow("SELECT path,blob,size FROM layer_files WHERE layer_id=? AND path=?", layer, path).
		Scan(&f.Path, &f.Blob, &f.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// LookupVersionFile selects an overlay before looking up the exact path;
// absence within an overlay never falls back to the base layer.
func (s *Store) LookupVersionFile(v *Version, path string) (*File, error) {
	key := v.BaseLayer
	for _, overlay := range v.Overlays {
		if strings.HasPrefix(path, overlay.Dir) {
			key = overlay.Layer
			break
		}
	}
	return s.LookupFile(key, path)
}
