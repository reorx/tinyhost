package bundle

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type packer struct {
	writer  *tar.Writer
	limits  Limits
	summary Summary
	index   bool
	count   int
	total   int64
}

func (p *packer) add(src, name string, info fs.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		p.summary.Warnings = append(p.summary.Warnings, "skipping symlink: "+name)
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported file: %s", name)
	}
	if _, err := normalizePath(name); err != nil {
		return err
	}
	if hidden(name) {
		p.summary.Skipped = append(p.summary.Skipped, name)
		return nil
	}
	p.count++
	if info.Size() > p.limits.MaxSize-p.total || p.count > p.limits.MaxFiles {
		return ErrTooLarge
	}
	p.total += info.Size()
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()
	if name == "tinyhost.json" {
		p.summary.Manifest, err = readManifest(source)
		if err != nil {
			return err
		}
		if _, err = source.Seek(0, 0); err != nil {
			return err
		}
	}
	if checkable(name) && info.Size() <= 1<<20 && len(p.summary.PathWarnings) < 20 {
		content, err := io.ReadAll(io.LimitReader(source, (1<<20)+1))
		if err != nil {
			return err
		}
		warnings := CheckAbsolutePaths(map[string][]byte{name: content})
		remaining := 20 - len(p.summary.PathWarnings)
		if len(warnings) > remaining {
			warnings = warnings[:remaining]
		}
		p.summary.PathWarnings = append(p.summary.PathWarnings, warnings...)
		if _, err = source.Seek(0, 0); err != nil {
			return err
		}
	}
	if err = p.writer.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: info.Size(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err = io.Copy(p.writer, source); err != nil {
		return err
	}
	if name != "tinyhost.json" {
		p.summary.Files++
		p.summary.Size += info.Size()
	}
	if name == "index.html" {
		p.index = true
	}
	return nil
}

func (p *packer) directory(root string) error {
	return filepath.WalkDir(root, func(src string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if src == root {
			return nil
		}
		name, err := filepath.Rel(root, src)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		excluded := hidden(name)
		for _, part := range strings.Split(name, "/") {
			if part == "node_modules" {
				excluded = true
			}
		}
		if excluded {
			p.summary.Skipped = append(p.summary.Skipped, name)
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return p.add(src, name, info)
	})
}

// Pack returns a temporary tar.gz path owned by the caller. Options{Data:true}
// permits data-only uploads and preserves the basename of a single data file.
func Pack(path string, options ...Options) (string, Summary, error) {
	var opt Options
	if len(options) > 0 {
		opt = options[0]
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", Summary{}, err
	}
	f, err := os.CreateTemp("", "tinyhost-*.tar.gz")
	if err != nil {
		return "", Summary{}, err
	}
	success := false
	defer func() {
		f.Close()
		if !success {
			os.Remove(f.Name())
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	p := &packer{writer: tw, limits: defaults(opt.Limits)}
	if info.IsDir() {
		err = p.directory(path)
	} else {
		name := filepath.Base(path)
		if !opt.Data {
			if filepath.Ext(name) != ".html" {
				return "", p.summary, fmt.Errorf("deploy file must be .html")
			}
			name = "index.html"
		}
		err = p.add(path, name, info)
	}
	if err != nil {
		return "", p.summary, err
	}
	if p.summary.Files == 0 {
		return "", p.summary, fmt.Errorf("empty bundle")
	}
	if !opt.Data && !p.index {
		return "", p.summary, fmt.Errorf("bundle requires index.html")
	}
	if err = tw.Close(); err != nil {
		return "", p.summary, err
	}
	if err = gz.Close(); err != nil {
		return "", p.summary, err
	}
	if err = f.Close(); err != nil {
		return "", p.summary, err
	}
	success = true
	return f.Name(), p.summary, nil
}
