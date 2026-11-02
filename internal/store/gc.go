package store

import (
	"database/sql"
	"fmt"
	"time"
)

// PruneDataVersions keeps the newest keep data versions PLUS main if older.
func (s *Store) PruneDataVersions(keep int) error {
	if keep < 0 {
		return fmt.Errorf("keep must be nonnegative")
	}
	return s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM versions WHERE kind='data'
			AND number NOT IN (
				SELECT v.number FROM versions v
				WHERE v.app_id=versions.app_id AND v.kind='data'
				ORDER BY v.number DESC LIMIT ?)
			AND NOT EXISTS (
				SELECT 1 FROM apps a
				WHERE a.id=versions.app_id AND a.current_version=versions.number)`, keep)
		return err
	})
}

func (s *Store) PurgeTrash(before time.Time) error {
	_, err := s.db.Exec("DELETE FROM apps WHERE deleted_at < ?", before.Unix())
	return err
}

const orphanPredicate = `NOT EXISTS (SELECT 1 FROM versions v WHERE v.base_layer=layers.id)
	AND NOT EXISTS (SELECT 1 FROM version_overlays o WHERE o.layer=layers.id)`

func (s *Store) OrphanLayers() ([]*Layer, error) {
	rows, err := s.db.Query("SELECT id,file_count,total_size,created_at FROM layers WHERE " + orphanPredicate + " ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Layer{}
	for rows.Next() {
		var layer Layer
		if err = rows.Scan(&layer.ID, &layer.FileCount, &layer.TotalSize, &layer.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &layer)
	}
	return out, rows.Err()
}

// DeleteOrphanLayer rechecks references atomically; stale GC candidates are safe.
func (s *Store) DeleteOrphanLayer(id string) error {
	_, err := s.db.Exec("DELETE FROM layers WHERE id=? AND "+orphanPredicate, id)
	return err
}

func (s *Store) ReferencedBlobs() (map[string]bool, error) {
	rows, err := s.db.Query("SELECT DISTINCT blob FROM layer_files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var hash string
		if err = rows.Scan(&hash); err != nil {
			return nil, err
		}
		out[hash] = true
	}
	return out, rows.Err()
}
