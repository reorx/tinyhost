package slug

import (
	"strings"
	"testing"
)

// Behavior: slugs obey length, alphabet, edge, repeated hyphen and reserved word rules.
func TestSlugsObeyAllRules(t *testing.T) {
	for _, s := range []string{"abc", "a-1", "123", strings.Repeat("a", 40)} {
		if !Valid(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{
		"ab", strings.Repeat("a", 41), "-abc", "abc-", "a--b", "Abc", "a_b",
		"api", "admin", "www", "static", "assets", "login", "logout", "auth",
	} {
		if Valid(s) {
			t.Fatal(s)
		}
	}
}

// Behavior: version segments accept positive decimal version numbers and valid slugs only.
func TestSegmentsResolveVersions(t *testing.T) {
	for _, c := range []struct {
		segment, slug string
		number        int
	}{
		{"q3-report@v12", "q3-report", 12},
		{"abc", "abc", 0},
	} {
		s, n, err := ParseSegment(c.segment)
		if err != nil || s != c.slug || n != c.number {
			t.Fatal(s, n, err)
		}
	}
	for _, s := range []string{"abc@v0", "abc@v-1", "abc@v", "abc@v1@v2", "api", "abc@v9999999999999999999999"} {
		if _, _, err := ParseSegment(s); err == nil {
			t.Fatal(s)
		}
	}
}

// Behavior: a preview reference accepts only a valid app name and twelve lowercase hex digits, distinct from main and version links.
func TestParseAppReferences(t *testing.T) {
	for segment, want := range map[string]Ref{
		"report": {Name: "report"}, "report@v12": {Name: "report", Version: 12}, "report@abcdef123456": {Name: "report", Preview: "abcdef123456"},
	} {
		if got, err := ParseRef(segment); err != nil || got != want {
			t.Fatal(segment, got, err)
		}
	}
	for _, segment := range []string{"report@ABCDEF123456", "report@abc", "api@abcdef123456", "report@v0", "report@abcdef123456@v1"} {
		if _, err := ParseRef(segment); err == nil {
			t.Fatal(segment)
		}
	}
}
