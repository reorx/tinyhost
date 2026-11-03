package bundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

type zentry struct {
	name string
	body string
}

func makeZip(t *testing.T, entries ...zentry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(w, e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func unpackZip(t *testing.T, data []byte, limits Limits) (map[string]string, Summary, error) {
	t.Helper()
	files := map[string]string{}
	summary, err := UnpackZip(bytes.NewReader(data), int64(len(data)), limits, func(p string, r io.Reader) error {
		b, err := io.ReadAll(r)
		files[p] = string(b)
		return err
	})
	return files, summary, err
}

// Behavior: a zip with index.html at its root unpacks completely with correct counts.
func TestUnpackZipRootIndex(t *testing.T) {
	data := makeZip(t, zentry{"index.html", "<h1>hi</h1>"}, zentry{"css/a.css", "body{}"}, zentry{"中文.txt", "x"})
	files, summary, err := unpackZip(t, data, Limits{RequireIndex: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files["css/a.css"] != "body{}" || files["中文.txt"] != "x" {
		t.Fatalf("files = %v", files)
	}
	if summary.Files != 3 || summary.Size != int64(len("<h1>hi</h1>")+len("body{}")+1) {
		t.Fatalf("summary = %+v", summary)
	}
}

// Behavior: a single wrapping folder is stripped, __MACOSX vanishes entirely and root junk is skipped by name.
func TestUnpackZipStripsShellAndIgnoresMacosx(t *testing.T) {
	data := makeZip(t,
		zentry{"site/index.html", "home"},
		zentry{"site/js/app.js", "js"},
		zentry{"site/tinyhost.json", `{"title":"Shell Title"}`},
		zentry{"site/.DS_Store", "junk"},
		zentry{"__MACOSX/site/._index.html", "junk"},
		zentry{".DS_Store", "junk"},
	)
	files, summary, err := unpackZip(t, data, Limits{RequireIndex: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, map[string]string{"index.html": "home", "js/app.js": "js"}) {
		t.Fatalf("files = %v", files)
	}
	if summary.Manifest.Title != "Shell Title" {
		t.Fatalf("manifest = %+v", summary.Manifest)
	}
	skipped := strings.Join(summary.Skipped, ",")
	if strings.Contains(skipped, "__MACOSX") || !strings.Contains(skipped, ".DS_Store") {
		t.Fatalf("skipped = %v", summary.Skipped)
	}
}

// Behavior: hidden files, .well-known exception and node_modules are skipped without counting toward limits.
func TestUnpackZipSkipsHiddenAndNodeModules(t *testing.T) {
	data := makeZip(t,
		zentry{"index.html", "x"},
		zentry{".git/config", "c"},
		zentry{"node_modules/a/b.js", "big big big big"},
		zentry{".well-known/security.txt", "s"},
	)
	files, summary, err := unpackZip(t, data, Limits{RequireIndex: true, MaxFiles: 2, MaxSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, map[string]string{"index.html": "x", ".well-known/security.txt": "s"}) {
		// .well-known is not hidden, so it counts as a file
		t.Fatalf("files = %v", files)
	}
	if len(summary.Skipped) != 2 {
		t.Fatalf("skipped = %v", summary.Skipped)
	}
}

// Behavior: without a root index.html the error names directories that contain one.
func TestUnpackZipMissingIndexNamesCandidates(t *testing.T) {
	data := makeZip(t, zentry{"repo/README.md", "r"}, zentry{"repo/dist/index.html", "i"})
	_, _, err := unpackZip(t, data, Limits{RequireIndex: true})
	if !errors.Is(err, ErrNoIndex) {
		t.Fatalf("err = %v", err)
	}
	var missing *MissingIndexError
	if !errors.As(err, &missing) || !reflect.DeepEqual(missing.Candidates, []string{"repo/dist/"}) {
		t.Fatalf("err = %#v", err)
	}
}

// Behavior: candidates are ordered shallow first and capped at three.
func TestUnpackZipCandidatesOrderedAndCapped(t *testing.T) {
	data := makeZip(t,
		zentry{"a/b/c/index.html", "1"}, zentry{"z/index.html", "2"}, zentry{"y/index.html", "3"},
		zentry{"x/index.html", "4"}, zentry{"README", "r"},
	)
	_, _, err := unpackZip(t, data, Limits{RequireIndex: true})
	var missing *MissingIndexError
	if !errors.As(err, &missing) || !reflect.DeepEqual(missing.Candidates, []string{"x/", "y/", "z/"}) {
		t.Fatalf("err = %#v", err)
	}
}

// Behavior: an empty or fully ignored zip is reported as missing index rather than succeeding.
func TestUnpackZipEmptyFailsRequireIndex(t *testing.T) {
	data := makeZip(t, zentry{".DS_Store", "x"})
	_, _, err := unpackZip(t, data, Limits{RequireIndex: true})
	if !errors.Is(err, ErrNoIndex) {
		t.Fatalf("err = %v", err)
	}
}

// Behavior: the shell is only stripped one level deep.
func TestUnpackZipDoesNotStripTwoLevels(t *testing.T) {
	data := makeZip(t, zentry{"repo-main/dist/index.html", "i"}, zentry{"repo-main/README", "r"})
	_, _, err := unpackZip(t, data, Limits{RequireIndex: true})
	if !errors.Is(err, ErrNoIndex) {
		t.Fatalf("err = %v", err)
	}
}

// Behavior: dangerous entry names are rejected.
func TestUnpackZipRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"../x", "/abs", "a/../../b", "a\\b", "a//b"} {
		data := makeZip(t, zentry{"index.html", "x"}, zentry{name, "y"})
		if _, _, err := unpackZip(t, data, Limits{RequireIndex: true}); err == nil {
			t.Errorf("%q accepted", name)
		}
	}
}

// Behavior: symlink entries are rejected.
func TestUnpackZipRejectsSymlink(t *testing.T) {
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	h := &zip.FileHeader{Name: "link"}
	h.SetMode(fs.ModeSymlink | 0777)
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, "/etc/passwd")
	w, _ = zw.Create("index.html")
	io.WriteString(w, "x")
	zw.Close()
	_, _, err = unpackZip(t, buffer.Bytes(), Limits{RequireIndex: true})
	if err == nil || !strings.Contains(err.Error(), "unsupported entry") {
		t.Fatalf("err = %v", err)
	}
}

// Behavior: entries whose names are not UTF-8 are rejected with a clear error.
func TestUnpackZipRejectsNonUTF8Names(t *testing.T) {
	data := makeZip(t, zentry{"index.html", "x"}, zentry{"\xd6\xd0\xce\xc4.txt", "y"})
	_, _, err := unpackZip(t, data, Limits{RequireIndex: true})
	if err == nil || !strings.Contains(err.Error(), "not UTF-8") {
		t.Fatalf("err = %v", err)
	}
}

// Behavior: encrypted entries return ErrEncrypted.
func TestUnpackZipRejectsEncrypted(t *testing.T) {
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	h := &zip.FileHeader{Name: "index.html", Method: zip.Store, Flags: 0x1}
	w, err := zw.CreateRaw(h)
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, "secret")
	zw.Close()
	_, _, err = unpackZip(t, buffer.Bytes(), Limits{RequireIndex: true})
	if !errors.Is(err, ErrEncrypted) {
		t.Fatalf("err = %v", err)
	}
}

// Behavior: stripping the shell cannot create duplicate paths.
func TestUnpackZipRejectsDuplicateAfterStrip(t *testing.T) {
	data := makeZip(t, zentry{"site/index.html", "a"}, zentry{"site/index.html", "b"})
	if _, _, err := unpackZip(t, data, Limits{RequireIndex: true}); err == nil {
		t.Fatal("duplicate accepted")
	}
}

// Behavior: too many files or too much declared size is ErrTooLarge.
func TestUnpackZipEnforcesLimits(t *testing.T) {
	data := makeZip(t, zentry{"index.html", "x"}, zentry{"a", "y"}, zentry{"b", "z"})
	if _, _, err := unpackZip(t, data, Limits{RequireIndex: true, MaxFiles: 2}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("count err = %v", err)
	}
	data = makeZip(t, zentry{"index.html", strings.Repeat("x", 100)})
	if _, _, err := unpackZip(t, data, Limits{RequireIndex: true, MaxSize: 50}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size err = %v", err)
	}
}

// Behavior: an entry that decompresses beyond its declared size fails instead of being stored.
func TestUnpackZipRejectsActualSizeBeyondDeclared(t *testing.T) {
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	h := &zip.FileHeader{Name: "index.html", Method: zip.Store, UncompressedSize64: 2, CompressedSize64: 10}
	w, err := zw.CreateRaw(h)
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, "0123456789")
	zw.Close()
	_, _, err = unpackZip(t, buffer.Bytes(), Limits{RequireIndex: true})
	if err == nil {
		t.Fatal("oversized entry accepted")
	}
}

// Behavior: garbage that is not a zip returns an error.
func TestUnpackZipRejectsGarbage(t *testing.T) {
	if _, _, err := unpackZip(t, []byte("not a zip file at all"), Limits{RequireIndex: true}); err == nil {
		t.Fatal("garbage accepted")
	}
}

// Behavior: Unpack's missing-index error still reads the same and satisfies ErrNoIndex.
func TestUnpackMissingIndexIsErrNoIndex(t *testing.T) {
	data := archive(t, entry{name: "a.txt", body: "x"})
	_, err := Unpack(bytes.NewReader(data), Limits{RequireIndex: true}, func(string, io.Reader) error { return nil })
	if !errors.Is(err, ErrNoIndex) || err.Error() != "bundle requires index.html" {
		t.Fatalf("err = %v", err)
	}
}
