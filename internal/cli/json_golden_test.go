package cli

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite CLI JSON golden snapshots")
var goldenRoot, goldenRootErr = filepath.Abs(filepath.Join("testdata", "golden"))
var originPattern = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)
var clientPattern = regexp.MustCompile(`c_[A-Za-z0-9_-]{43}`)

func mask(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if item != nil && (key == "id" || key == "created_by" || key == "pushed_by" || key == "app_created_by") {
				v[key] = "<id>"
				continue
			}
			if item != nil && strings.HasSuffix(key, "_at") {
				v[key] = "<time>"
				continue
			}
			v[key] = mask(item)
		}
	case []any:
		for i, item := range v {
			v[i] = mask(item)
		}
	case string:
		return clientPattern.ReplaceAllString(originPattern.ReplaceAllString(v, "http://server.test"), "<id>")
	}
	return value
}
func checkGolden(t *testing.T, name, doc string) {
	t.Helper()
	if goldenRootErr != nil {
		t.Fatal(goldenRootErr)
	}
	var value any
	if err := json.Unmarshal([]byte(doc), &value); err != nil {
		t.Fatalf("JSON %s: %v: %s", name, err, doc)
	}
	b, err := json.MarshalIndent(mask(value), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(b) + "\n"
	path := filepath.Join(goldenRoot, name+".json")
	if *updateGolden {
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run go test ./internal/cli -update", err)
	}
	if got != string(want) {
		t.Errorf("%s\nwant: %s\ngot: %s", name, want, got)
	}
}
