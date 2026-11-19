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

// Behavior: App previews require an explicit app, keep main in place, renew the same URL, and can be published by URL or slug/hash with an optional message.
func TestReviewPublishAndRollbackFromCLI(t *testing.T) {
	f := setup(t)
	f.create()
	f.write("index.html", "review")
	if standalone := previewDoc(t, f.ok("preview", f.site, "--json")); !strings.Contains(standalone["url"].(string), "/_p/") {
		t.Fatal(standalone)
	}
	raw := f.ok("preview", f.site, "--app", "demo", "--ttl", "1h", "-m", "review notes", "--json")
	checkGolden(t, "app_preview", raw)
	p := previewDoc(t, raw)
	hash, link := p["hash"].(string), p["url"].(string)
	if p["current_version"] != float64(1) || p["message"] != "review notes" || p["expires_at"].(float64)-p["updated_at"].(float64) != 3600 {
		t.Fatal(p)
	}
	human := f.ok("preview", f.site, "--app", "demo")
	for _, want := range []string{link, "v1", "tinyhost publish demo " + hash, "过期"} {
		if !strings.Contains(human, want) {
			t.Fatal(human)
		}
	}
	checkGolden(t, "publish", f.ok("publish", link, "-m", "approved", "--json"))
	checkGolden(t, "app_previews", f.ok("previews", "--app", "demo", "--json"))
	f.content("/demo/", "review")
	if out := f.ok("preview", f.site, "--app", "demo"); !strings.Contains(out, "已发布为 v2") {
		t.Fatal(out)
	}
	checkGolden(t, "rollback", f.ok("rollback", "demo", "v1", "-m", "restore", "--json"))
	if out := f.ok("versions", "demo"); !strings.Contains(out, "回滚自 v1") || !strings.Contains(out, "来自预览 "+hash) || !strings.Contains(out, "approved") || !strings.Contains(out, "restore") {
		t.Fatal(out)
	}
	f.ok("publish", "demo", hash)
	if out := f.ok("rollback", "demo", "v1"); !strings.Contains(out, "v5 已回滚到 v1") {
		t.Fatal(out)
	}
	for _, args := range [][]string{
		{"preview", f.site, "--app", "demo", "--new"},
		{"publish", "https://elsewhere.test/demo@" + hash + "/"},
		{"publish", f.http.URL + "/demo@v1/"}, {"publish", link + "index.html"},
		{"previews", "--app", "demo", "rm", hash},
	} {
		before := f.requests.Load()
		if code, out, errOut := run(args...); code != 2 || f.requests.Load() != before {
			t.Fatal(args, code, out, errOut)
		}
	}
}
