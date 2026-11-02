package bundle

import (
	"strings"
	"testing"
)

// Behavior: common root-relative references warn with lines, while remote, data, and relative URLs do not.
func TestCheckAbsolutePaths(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       int
	}{
		{"index.html", "<script src=\"/app.js\">\n<a href='/docs'>", 2},
		{"style.css", "a { background: url( /image.png) }", 1},
		{"app.js", "fetch(`/data.json`)\nimport('/mod.js')\nimport x from \"/lib.js\"", 3},
		{"a.mjs", "fetch(\"/x\")", 1},
		{"a.htm", "<form action='/send'><img poster='/x'>", 1},
		{"index.html", "<script src=\"//cdn/a.js\"><a href='https://a/b'><img src='data:image/png;base64,x'><img src='./x'>", 0},
		{"style.css", "url(//cdn/x) url('https://a/x') url(data:image/png,x) url(./x)", 0},
		{"app.js", "fetch('//cdn/x'); import('https://a/x'); import x from './x'", 0},
		{"data.json", "{\"src\":\"/x\"}", 0},
		{"large.js", strings.Repeat(" ", 1<<20) + "fetch('/x')", 0},
		{"many.js", strings.Repeat("fetch('/x')\n", 10), 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			warnings := CheckAbsolutePaths(map[string][]byte{tc.name: []byte(tc.text)})
			if len(warnings) != tc.want {
				t.Fatalf("%+v", warnings)
			}
			for i, w := range warnings {
				if w.File != tc.name || w.Line != i+1 || w.Text == "" {
					t.Fatal(w)
				}
			}
		})
	}
	files := map[string][]byte{}
	for _, name := range []string{"a.js", "b.js", "c.js", "d.js", "e.js"} {
		files[name] = []byte(strings.Repeat("fetch('/x')\n", 8))
	}
	if len(CheckAbsolutePaths(files)) != 20 {
		t.Fatal("total cap")
	}
}
