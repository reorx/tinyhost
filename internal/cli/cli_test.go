package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/blob"
	"github.com/reorx/tinyhost/internal/server"
	"github.com/reorx/tinyhost/internal/store"
	"github.com/reorx/tinyhost/internal/token"
)

type fixture struct {
	t                         *testing.T
	http                      *httptest.Server
	site, key, other, creator string
	requests                  atomic.Int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tinyhost.db"))
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := blob.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, key: token.New(), other: token.New(), site: t.TempDir()}
	for _, item := range []struct{ name, key string }{{"writer", f.key}, {"data-agent", f.other}} {
		c, err := db.CreateClient(item.name, token.Hash(item.key), nil)
		if err != nil {
			t.Fatal(err)
		}
		if item.key == f.key {
			f.creator = c.ID
		}
	}
	f.http = httptest.NewUnstartedServer(nil)
	origin := "http://" + f.http.Listener.Addr().String()
	s := &server.Server{Store: db, Blobs: blobs, Config: server.Config{AppsURL: origin}, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	handler := s.Handler()
	f.http.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.requests.Add(1); handler.ServeHTTP(w, r) })
	f.http.Start()
	t.Cleanup(func() { f.http.Close(); db.Close() })
	t.Setenv("TINYHOST_SERVER", f.http.URL)
	t.Setenv("TINYHOST_TOKEN", f.key)
	f.write("index.html", "<script src=\"./app.js\"></script>v1")
	f.write("app.js", "fetch('data/sales.json')")
	f.write("data/sales.json", "old")
	return f
}
func (f *fixture) write(name, content string) {
	f.t.Helper()
	p := filepath.Join(f.site, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		f.t.Fatal(err)
	}
}
func run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}
func (f *fixture) ok(args ...string) string {
	f.t.Helper()
	code, out, errOut := run(args...)
	if code != 0 {
		f.t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, out, errOut)
	}
	return out
}
func (f *fixture) create() {
	f.t.Helper()
	f.ok("deploy", f.site, "--app", "demo", "--create", "--public")
}
func (f *fixture) content(path, want string) {
	f.t.Helper()
	res, err := http.Get(f.http.URL + path)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != 200 || string(b) != want {
		f.t.Fatalf("%s: status=%d body=%s err=%v", path, res.StatusCode, b, err)
	}
}

// Behavior: deploy creates working URLs and remembers only app while preserving unknown manifest fields.
func TestDeployCreatesURLsAndPreservesManifest(t *testing.T) {
	f := setup(t)
	f.write("tinyhost.json", `{"title":"报告","data":{"data/":"销售额"},"custom":{"keep":true},"count":9007199254740993}`)
	out := f.ok("deploy", f.site, "--app", "demo", "--create", "--public", "--title", "显式标题", "--json")
	checkGolden(t, "deploy", out)
	var d api.DeployResult
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if d.Version != 1 || d.Files != 3 || !d.Promoted {
		t.Fatalf("%+v", d)
	}
	f.content("/demo/", `<script src="./app.js"></script>v1`)
	f.content("/demo@v1/app.js", `fetch('data/sales.json')`)
	b, err := os.ReadFile(filepath.Join(f.site, "tinyhost.json"))
	if err != nil || !strings.Contains(string(b), `"app": "demo"`) || !strings.Contains(string(b), `"custom"`) || !strings.Contains(string(b), `9007199254740993`) {
		t.Fatalf("manifest %s %v", b, err)
	}
	info := f.ok("info", "demo", "--json")
	if !strings.Contains(info, "显式标题") {
		t.Fatal(info)
	}
}

// Behavior: deploy defaults to the current directory and manifest app, preserving immutable versions and no-promote.
func TestRedeployUsesCurrentDirectoryAndCanLeaveMainUnchanged(t *testing.T) {
	f := setup(t)
	f.create()
	t.Chdir(f.site)
	f.write("index.html", "v2")
	f.ok("deploy", "-m", "第二版")
	f.write("index.html", "v3")
	checkGolden(t, "deploy_no_promote", f.ok("--json", "deploy", "--no-promote"))
	f.content("/demo/", "v2")
	f.content("/demo@v1/", `<script src="./app.js"></script>v1`)
	f.content("/demo@v3/", "v3")
	checkGolden(t, "promote", f.ok("promote", "demo", "v3", "--json"))
	f.content("/demo/", "v3")
}

// Behavior: a second agent can push data, redeploy preserves it, and removing the overlay reveals bundled data.
func TestDataCommandsPreserveAndRemoveOverlays(t *testing.T) {
	f := setup(t)
	f.create()
	data := filepath.Join(t.TempDir(), "sales.json")
	if err := os.WriteFile(data, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TINYHOST_TOKEN", f.other)
	checkGolden(t, "data_push", f.ok("data", "push", data, "--app", "demo", "--to", "data/", "-m", "新数据", "--json"))
	f.content("/demo/data/sales.json", "new")
	t.Setenv("TINYHOST_TOKEN", f.key)
	out := f.ok("deploy", f.site)
	if !strings.Contains(out, "创建者："+f.creator) {
		t.Fatal(out)
	}
	f.content("/demo/data/sales.json", "new")
	code, out, _ := run("data", "push", data, "--app", "demo", "--to", "data/nested/", "--json")
	if code != 1 {
		t.Fatalf("%d %s", code, out)
	}
	checkGolden(t, "overlay_conflict", out)
	checkGolden(t, "data_rm", f.ok("data", "rm", "--app", "demo", "--to", "data/", "--json"))
	f.content("/demo/data/sales.json", "old")
}

// Behavior: apps, info, versions and whoami expose the shared DTOs and useful readable summaries.
func TestQueriesDescribeAppsVersionsOverlaysAndIdentity(t *testing.T) {
	f := setup(t)
	f.write("tinyhost.json", `{"title":"报告","data":{"data/":"销售额"}}`)
	f.create()
	f.ok("data", "push", filepath.Join(f.site, "data"), "--app", "demo", "--to", "data/")
	for _, tc := range []struct {
		name string
		args []string
		text []string
	}{
		{"apps", []string{"apps"}, []string{"demo", "报告", "public", "v2", f.creator}},
		{"info", []string{"info", "demo"}, []string{"销售额", "data/", f.creator, "主入口"}},
		{"versions", []string{"versions", "demo"}, []string{"v2", "主入口", "data/", f.creator}},
		{"whoami", []string{"whoami"}, []string{"writer", f.http.URL}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkGolden(t, tc.name, f.ok(append(tc.args, "--json")...))
			out := f.ok(tc.args...)
			for _, s := range tc.text {
				if !strings.Contains(out, s) {
					t.Fatalf("missing %q: %s", s, out)
				}
			}
		})
	}
}

// Behavior: delete respects ownership and reports the restoration period before content becomes inaccessible.
func TestDeleteReportsOwnershipAndRestorationPeriod(t *testing.T) {
	f := setup(t)
	f.create()
	t.Setenv("TINYHOST_TOKEN", f.other)
	code, out, _ := run("delete", "demo", "--json")
	if code != 1 || !strings.Contains(out, f.creator) {
		t.Fatalf("%d %s", code, out)
	}
	checkGolden(t, "not_creator", out)
	t.Setenv("TINYHOST_TOKEN", f.key)
	checkGolden(t, "delete", f.ok("delete", "demo", "--json"))
	res, err := http.Get(f.http.URL + "/demo/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatal(res.StatusCode)
	}
	f.ok("deploy", f.site, "--app", "other-app", "--create")
	if out := f.ok("delete", "other-app"); !strings.Contains(out, "7 天内可以请 Owner 在后台恢复") {
		t.Fatal(out)
	}
}

// Behavior: missing and occupied slugs return actionable errors without changing the local manifest on failure.
func TestDeployReportsMissingTakenAndInvalidSlugs(t *testing.T) {
	f := setup(t)
	code, out, _ := run("deploy", f.site, "--app", "demo", "--json")
	if code != 1 {
		t.Fatal(code)
	}
	checkGolden(t, "app_not_found", out)
	if _, err := os.Stat(filepath.Join(f.site, "tinyhost.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	f.create()
	code, out, _ = run("deploy", f.site, "--app", "demo", "--create", "--json")
	if code != 1 {
		t.Fatal(code)
	}
	checkGolden(t, "slug_taken", out)
	_, _, errOut := run("deploy", f.site, "--app", "demo", "--create")
	if !strings.Contains(errOut, f.creator) || !strings.Contains(errOut, "提示：") {
		t.Fatal(errOut)
	}
	code, _, errOut = run("deploy", f.site, "--app", "bad--slug", "--create")
	if code != 1 || !strings.Contains(errOut, "3–40") {
		t.Fatalf("%d %s", code, errOut)
	}
}

// Behavior: local bundle validation rejects empty, missing-index, excessive-file and oversized uploads before HTTP.
func TestInvalidBundlesNeverReachServer(t *testing.T) {
	f := setup(t)
	for _, kind := range []string{"empty", "no-index", "many", "large"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			switch kind {
			case "no-index":
				if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			case "many":
				for i := 0; i < 1001; i++ {
					name := filepath.Join(dir, fmt.Sprintf("file-%04d", i))
					if err := os.WriteFile(name, []byte("x"), 0644); err != nil {
						t.Fatal(err)
					}
				}
			case "large":
				file, err := os.Create(filepath.Join(dir, "index.html"))
				if err != nil {
					t.Fatal(err)
				}
				err = file.Truncate(51 << 20)
				file.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			before := f.requests.Load()
			code, _, errOut := run("deploy", dir, "--app", "demo", "--create")
			if code != 1 || errOut == "" || f.requests.Load() != before {
				t.Fatalf("code=%d err=%s requests=%d/%d", code, errOut, f.requests.Load(), before)
			}
		})
	}
}

// Behavior: single HTML files deploy as index.html and dotfiles are excluded while symlinks produce warnings.
func TestSingleFileAndExcludedFiles(t *testing.T) {
	f := setup(t)
	f.write(".secret", "secret")
	f.write(".git/config", "secret")
	f.write("node_modules/no.js", "secret")
	f.write(".well-known/test", "kept")
	if err := os.Symlink(filepath.Join(f.site, "index.html"), filepath.Join(f.site, "linked.html")); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run("deploy", f.site, "--app", "demo", "--create", "--public", "--json")
	if code != 0 || !strings.Contains(errOut, "symlink") {
		t.Fatalf("%d %s", code, errOut)
	}
	for _, p := range []string{".secret", ".git/config", "node_modules/no.js", "linked.html", "tinyhost.json"} {
		res, err := http.Get(f.http.URL + "/demo/" + p)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatalf("%s %d", p, res.StatusCode)
		}
	}
	f.content("/demo/.well-known/test", "kept")
	f.write("one.html", "single")
	f.ok("deploy", filepath.Join(f.site, "one.html"), "--app", "single-app", "--create", "--public")
	f.content("/single-app/", "single")
}

// Behavior: CLI errors distinguish usage, missing credentials, invalid credentials and server failures in JSON.
func TestExitCodesAndJSONErrors(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"usage", []string{"promote", "demo", "wrong", "--json"}, 2},
		{"unknown", []string{"nonesuch", "--json"}, 2},
		{"missing_app", []string{"deploy", f.site, "--json"}, 2},
		{"missing_arg", []string{"info", "--json"}, 2},
		{"bad_flag", []string{"--json", "apps", "--bad"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, _ := run(tc.args...)
			if code != tc.code {
				t.Fatalf("%d %s", code, out)
			}
			checkGolden(t, tc.name, out)
		})
	}
	t.Setenv("TINYHOST_TOKEN", "")
	code, out, _ := run("apps", "--json")
	if code != 3 {
		t.Fatal(code)
	}
	checkGolden(t, "missing_token", out)
	t.Setenv("TINYHOST_TOKEN", "invalid")
	code, out, _ = run("apps", "--json")
	if code != 3 {
		t.Fatal(code)
	}
	checkGolden(t, "unauthorized", out)
}

// Behavior: server flag overrides environment which overrides the platform config file; malformed config is actionable.
func TestServerConfigurationPrecedence(t *testing.T) {
	f := setup(t)
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(configDir, "tinyhost"), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(configDir, "tinyhost", "config.json")
	if err = os.WriteFile(config, []byte(`{"server":"`+f.http.URL+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TINYHOST_SERVER", "")
	f.ok("apps")
	t.Setenv("TINYHOST_SERVER", "http://127.0.0.1:1")
	f.ok("--server", f.http.URL, "apps")
	f.ok("apps", "--server", f.http.URL)
	if err = os.WriteFile(config, []byte(`invalid`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TINYHOST_SERVER", f.http.URL)
	f.ok("apps")
	t.Setenv("TINYHOST_SERVER", "")
	code, _, errOut := run("apps")
	if code != 1 || !strings.Contains(errOut, "config.json") {
		t.Fatalf("%d %s", code, errOut)
	}
}

// Behavior: help begins with Agent instructions and version and skill are usable offline in text and JSON.
func TestLocalCommandsWorkWithoutCredentials(t *testing.T) {
	t.Setenv("TINYHOST_TOKEN", "")
	t.Setenv("TINYHOST_SERVER", "")
	for _, name := range []string{"help", "version", "skill"} {
		code, out, errOut := run(name, "--json")
		if code != 0 {
			t.Fatalf("%s: %d %s", name, code, errOut)
		}
		checkGolden(t, name, out)
		code, out, errOut = run(name)
		if code != 0 || out == "" {
			t.Fatalf("%d %s", code, errOut)
		}
	}
	code, out, _ := run()
	if code != 0 || !strings.HasPrefix(out, "AGENTS: run \"tinyhost skill\" and read it fully before first use.") {
		t.Fatalf("%d %s", code, out)
	}
	for _, args := range [][]string{{"--help"}, {"deploy", "--help"}, {"data", "--help"}} {
		code, out, _ := run(args...)
		if code != 0 || !strings.Contains(out, "tinyhost") {
			t.Fatalf("%v %d %s", args, code, out)
		}
	}
}

// Behavior: global flags also work between data and its subcommand, and malformed subcommands stay machine-readable.
func TestNestedCommandsAcceptGlobalFlags(t *testing.T) {
	f := setup(t)
	f.create()
	out := f.ok("data", "--json", "--server", f.http.URL, "push", filepath.Join(f.site, "data"), "--app", "demo", "--to", "data/")
	var result api.DeployResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Kind != "data" {
		t.Fatalf("%s %v", out, err)
	}
	code, out, _ := run("data", "nonesuch", "--json")
	if code != 2 {
		t.Fatalf("%d %s", code, out)
	}
	var failure api.Error
	if err := json.Unmarshal([]byte(out), &failure); err != nil || failure.Error.Code != "usage" {
		t.Fatalf("%s %v", out, err)
	}
}

// Behavior: transport failures and non-JSON proxy errors have exit 1, and output failures cannot report success.
func TestNetworkAndOutputFailuresAreReported(t *testing.T) {
	f := setup(t)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		io.WriteString(w, "upstream unavailable")
	}))
	t.Cleanup(broken.Close)
	code, _, errOut := run("apps", "--server", broken.URL)
	if code != 1 || !strings.Contains(errOut, "HTTP 502") {
		t.Fatalf("%d %s", code, errOut)
	}
	code, _, errOut = run("apps", "--server", "http://127.0.0.1:1")
	if code != 1 || errOut == "" {
		t.Fatalf("%d %s", code, errOut)
	}
	var stderr bytes.Buffer
	if code = Run([]string{"version", "--json"}, failingWriter{}, &stderr); code != 1 {
		t.Fatal(code)
	}
	f.ok("apps")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// Behavior: a successful remote publish remains successful when remembering app locally fails, avoiding duplicate retries.
func TestSuccessfulPublishReportsManifestWriteFailureAsWarning(t *testing.T) {
	f := setup(t)
	f.write("tinyhost.json", `{"title":"只读元数据"}`)
	path := filepath.Join(f.site, "tinyhost.json")
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0644) })
	probe, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		probe.Close()
		t.Skip("filesystem permits writes to a read-only file")
	}
	code, out, errOut := run("deploy", f.site, "--app", "demo", "--create", "--public", "--json")
	if code != 0 || !strings.Contains(errOut, "发布已成功") || !strings.Contains(errOut, "--app demo") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	var result api.DeployResult
	if err = json.Unmarshal([]byte(out), &result); err != nil || result.Version != 1 {
		t.Fatalf("%s %v", out, err)
	}
	f.content("/demo/", `<script src="./app.js"></script>v1`)
}
