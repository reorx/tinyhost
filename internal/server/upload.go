package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/reorx/tinyhost/internal/bundle"
	"github.com/reorx/tinyhost/internal/store"
)

const (
	warningMaxFiles = 200
	warningMaxBytes = 8 << 20
	warningMaxFile  = 1 << 20
	warningMax      = 20
)

// consoleUpload lets the Owner publish a tar.gz or zip as a new app version.
// Session and Origin checks come from adminMiddleware. Unlike other console
// writes it answers in JSON, because the page uploads with XHR to show progress.
func (s *Server) consoleUpload(w http.ResponseWriter, r *http.Request) {
	s.lifecycle.RLock()
	defer s.lifecycle.RUnlock()
	a, err := s.Store.AppBySlug(r.PathValue("slug"))
	if err != nil {
		s.internal(w, err)
		return
	}
	if a == nil || a.DeletedAt != nil {
		fail(w, 404, "app_not_found", "App 不存在或已删除", "刷新页面后重试")
		return
	}
	q := r.URL.Query()
	message := q.Get("message")
	if len(message) > 1024 {
		fail(w, 400, "bad_request", "说明太长", "说明不超过 1024 字节")
		return
	}
	promote := q.Get("promote") != "0"
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/gzip" && mediaType != "application/zip" {
		fail(w, 415, "unsupported_type", "不支持的上传类型", "只支持文件夹、.zip 和 .html")
		return
	}
	limits, maxBody := s.uploadLimits("")
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	layer := &store.Layer{}
	sink := s.layerSink(layer, "")
	var summary bundle.Summary
	if mediaType == "application/zip" {
		summary, err = s.unpackZipBody(r.Body, limits, sink)
	} else {
		summary, err = bundle.Unpack(r.Body, limits, sink)
	}
	if err != nil {
		s.uploadError(w, err, limits)
		return
	}
	v, err := s.Store.CommitAppVersion(a.ID, layer, summary.Manifest, message, "owner", promote)
	if err != nil {
		s.mutationError(w, a, err)
		return
	}
	s.ownerAppEvent("app.deployed", a.ID, map[string]any{"version": v.Number, "message": v.Message})
	s.deployed(w, a, v, summary, promote)
}

// unpackZipBody spools the zip to the data dir's tmp folder: archive/zip needs a
// ReaderAt, and the spool keeps large uploads out of memory.
func (s *Server) unpackZipBody(body io.Reader, limits bundle.Limits, sink func(string, io.Reader) error) (bundle.Summary, error) {
	f, err := s.Blobs.TempFile()
	if err != nil {
		return bundle.Summary{}, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, err := io.Copy(f, body)
	if err != nil {
		return bundle.Summary{}, err
	}
	return bundle.UnpackZip(f, n, limits, sink)
}

func (s *Server) uploadError(w http.ResponseWriter, err error, limits bundle.Limits) {
	var maxErr *http.MaxBytesError
	var missing *bundle.MissingIndexError
	switch {
	case errors.Is(err, bundle.ErrTooLarge) || errors.As(err, &maxErr):
		fail(w, 413, "too_large", fmt.Sprintf("超过上限：最多 %d MB、%d 个文件", limits.MaxSize>>20, limits.MaxFiles), "减小上传大小或文件数量")
	case errors.Is(err, bundle.ErrNoIndex):
		hint := "根目录需要 index.html"
		if errors.As(err, &missing) && len(missing.Candidates) > 0 {
			hint = fmt.Sprintf("在 %s 里找到了 index.html，可以只上传这个文件夹", strings.Join(missing.Candidates, "、"))
		}
		fail(w, 400, "no_index", "根目录没有 index.html", hint)
	case errors.Is(err, bundle.ErrEncrypted):
		fail(w, 400, "bad_bundle", "不支持加密的 zip", "解压后上传文件夹")
	default:
		hint := "上传文件夹、.zip 或 .html"
		if strings.Contains(err.Error(), "not UTF-8") {
			hint = "文件名编码不是 UTF-8（常见于 Windows 中文系统打的 zip），请解压后上传文件夹"
		}
		fail(w, 400, "bad_bundle", "无法解析上传的内容："+err.Error(), hint)
	}
}

// versionPathWarnings re-scans a stored version for absolute-path references,
// bounded in files, bytes and results so the page render stays cheap.
func (s *Server) versionPathWarnings(v *store.Version) ([]bundle.Warning, error) {
	files, err := s.Store.LayerFiles(v.BaseLayer)
	if err != nil {
		return nil, err
	}
	var out []bundle.Warning
	scanned, read := 0, int64(0)
	for _, f := range files {
		if !bundle.Checkable(f.Path) || f.Size > warningMaxFile {
			continue
		}
		if scanned >= warningMaxFiles || read+f.Size > warningMaxBytes || len(out) >= warningMax {
			break
		}
		scanned++
		read += f.Size
		content, err := s.readBlob(f.Blob)
		if err != nil {
			return nil, err
		}
		out = append(out, bundle.CheckAbsolutePaths(map[string][]byte{f.Path: content})...)
	}
	if len(out) > warningMax {
		out = out[:warningMax]
	}
	return out, nil
}

func (s *Server) readBlob(hash string) ([]byte, error) {
	f, err := s.Blobs.Open(hash)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, warningMaxFile))
}
