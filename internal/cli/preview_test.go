package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func previewDoc(t *testing.T, doc string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func previewGolden(t *testing.T, name, doc string) {
	t.Helper()
	v := previewDoc(t, doc)
	if _, ok := v["url"]; ok {
		v["url"] = "http://server.test/_p/<token>/"
	}
	if rows, ok := v["previews"].([]any); ok {
		for _, r := range rows {
			row := r.(map[string]any)
			row["url"] = "http://server.test/_p/<token>/"
			row["source"] = "site"
		}
	}
	b, _ := json.Marshal(v)
	checkGolden(t, name, string(b))
}

// Behavior: CLI remembers a private server/path key, updates its link, supports new links, lists and removes by URL or ID.
func TestPreviewCLIRemembersLinkAndJSONContract(t *testing.T) {
	f := setup(t)
	one := f.ok("preview", f.site, "--json")
	previewGolden(t, "preview", one)
	p := previewDoc(t, one)
	second := previewDoc(t, f.ok("preview", f.site, "--json"))
	if p["url"] != second["url"] || second["reused"] != true {
		t.Fatal(second)
	}
	newer := previewDoc(t, f.ok("preview", f.site, "--new", "--json"))
	if newer["url"] == p["url"] {
		t.Fatal(newer)
	}
	again := previewDoc(t, f.ok("preview", f.site, "--json"))
	if again["url"] != newer["url"] {
		t.Fatal(again)
	}
	previewGolden(t, "previews", f.ok("previews", "--json"))
	path, err := authPath("previews.json")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), f.http.URL+"|"+filepath.Clean(f.site)) {
		t.Fatal(string(raw), err)
	}
	previewGolden(t, "preview_rm", f.ok("previews", "rm", p["url"].(string), "--json"))
	f.ok("previews", "rm", newer["id"].(string))
	if !strings.Contains(f.ok("previews"), "预览") {
		t.Fatal("missing human output")
	}
	code, out, _ := run("preview", f.site, "--ttl", "30d", "--json")
	if code != 2 {
		t.Fatal(code, out)
	}
	checkGolden(t, "preview_bad_ttl", out)
}

// Behavior: deploy, data push and preview warn on stderr before uploading and include optional JSON warnings.
func TestUploadsWarnAboutAbsolutePathsWithoutBlocking(t *testing.T) {
	f := setup(t)
	f.write("index.html", "<script src=\"/app.js\"></script>")
	f.write("data/data.js", "fetch('/data.json')")
	for _, args := range [][]string{
		{"deploy", f.site, "--app", "demo", "--create", "--json"},
		{"data", "push", filepath.Join(f.site, "data"), "--app", "demo", "--to", "data/", "--json"},
		{"preview", f.site, "--json"},
	} {
		code, out, stderr := run(args...)
		if code != 0 || !strings.Contains(stderr, ":1") || !strings.Contains(stderr, "相对路径") {
			t.Fatal(code, out, stderr)
		}
		result := previewDoc(t, out)
		if len(result["warnings"].([]any)) == 0 {
			t.Fatal(out)
		}
		if args[0] == "deploy" {
			checkGolden(t, "deploy_warnings", out)
		} else if args[0] == "data" {
			checkGolden(t, "data_push_warnings", out)
		} else {
			previewGolden(t, "preview_warnings", out)
		}
	}
}
