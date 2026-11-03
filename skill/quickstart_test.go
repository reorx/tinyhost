package skill

import (
	"strings"
	"testing"
)

// Behavior: the quickstart text names the real server and the full install flow, with no unfilled placeholders or doubled slashes.
func TestQuickstartHasRealServerAndFlow(t *testing.T) {
	text := Quickstart("https://apps.example.com/", "")
	for _, want := range []string{
		InstallCommand("https://apps.example.com"),
		"tinyhost skill",
		"tinyhost login --name",
		"--wait 120",
		"tinyhost whoami",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	for _, bad := range []string{"{{", QuickstartNamePlaceholder, ".com//"} {
		if strings.Contains(text, bad) {
			t.Fatalf("unexpected %q:\n%s", bad, text)
		}
	}
}

// Behavior: a given name goes straight into the login command, while an empty name asks the Agent to pick its own.
func TestQuickstartNameChangesLoginStep(t *testing.T) {
	const server = "https://apps.example.com"
	named := Quickstart(server, "report-agent")
	if !strings.Contains(named, `tinyhost login --name "report-agent" --wait 120`) {
		t.Fatal(named)
	}
	anon := Quickstart(server, "")
	if !strings.Contains(anon, `tinyhost login --name "<your-name>" --wait 120`) {
		t.Fatal(anon)
	}
	pick := "choose a short name"
	if !strings.Contains(anon, pick) || strings.Contains(named, pick) {
		t.Fatalf("pick-a-name sentence: anon=%v named=%v", strings.Contains(anon, pick), strings.Contains(named, pick))
	}
}

// Behavior: replacing the placeholder in the template version yields exactly what the server would generate for that name.
func TestQuickstartPlaceholderMatchesNamedVersion(t *testing.T) {
	const server = "https://apps.example.com"
	template := Quickstart(server, QuickstartNamePlaceholder)
	if n := strings.Count(template, QuickstartNamePlaceholder); n != 1 {
		t.Fatalf("placeholder appears %d times:\n%s", n, template)
	}
	got := strings.ReplaceAll(template, QuickstartNamePlaceholder, "qs-accept")
	if want := Quickstart(server, "qs-accept"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Behavior: a dev build can only be installed from when a local package directory is configured.
func TestInstallAvailable(t *testing.T) {
	if InstallAvailable("dev", false) {
		t.Fatal("dev without local packages must be unavailable")
	}
	if !InstallAvailable("dev", true) || !InstallAvailable("0.1.1", false) {
		t.Fatal("dev with local packages and release builds must be available")
	}
}
