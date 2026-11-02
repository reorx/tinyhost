package server

import (
	"context"
	"os"
	"time"
)

// GC prunes metadata before taking the final blob reference snapshot. The lock
// prevents uploads from publishing blobs between that snapshot and deletion.
func (s *Server) GC() error {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if err := s.Store.PruneAuth(s.now().Unix()); err != nil {
		return err
	}
	if err := s.Store.PrunePreviews(s.now().Unix()); err != nil {
		return err
	}
	keep := s.Config.DataVersionsKeep
	if keep == 0 {
		keep = 20
	}
	if err := s.Store.PruneDataVersions(keep); err != nil {
		return err
	}
	if err := s.Store.PurgeTrash(s.now().Add(-time.Duration(s.trashDays()) * 24 * time.Hour)); err != nil {
		return err
	}
	layers, err := s.Store.OrphanLayers()
	if err != nil {
		return err
	}
	for _, layer := range layers {
		if err = s.Store.DeleteOrphanLayer(layer.ID); err != nil {
			return err
		}
	}
	refs, err := s.Store.ReferencedBlobs()
	if err != nil {
		return err
	}
	before := s.now().Add(-time.Hour)
	return s.Blobs.Walk(func(hash string, info os.FileInfo) error {
		if !refs[hash] && info.ModTime().Before(before) {
			return s.Blobs.Remove(hash)
		}
		return nil
	})
}

// RunGC exits when the serving process shuts down; callers own the goroutine.
func (s *Server) RunGC(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.GC(); err != nil {
				s.logger().Error("gc failed", "error", err)
			}
		}
	}
}
