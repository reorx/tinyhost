package bundle

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Warning struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

var absoluteReference = regexp.MustCompile("(?i)(?:(?:src|href|action|poster)\\s*=\\s*[\"']/[^/]|url\\(\\s*[\"']?/[^/]|(?:fetch|import)\\s*\\(\\s*[\"'`]/[^/]|from\\s+[\"']/[^/])")

// Checkable reports whether name is a text file CheckAbsolutePaths can scan.
func Checkable(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html", ".htm", ".css", ".js", ".mjs":
		return true
	}
	return false
}

// CheckAbsolutePaths warns once per matching line in deterministic file order.
// It deliberately uses a small heuristic rather than parsing generated code.
func CheckAbsolutePaths(files map[string][]byte) []Warning {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []Warning{}
	for _, name := range names {
		content := files[name]
		if !Checkable(name) || len(content) > 1<<20 {
			continue
		}
		count := 0
		for i, line := range strings.Split(string(content), "\n") {
			if !absoluteReference.MatchString(line) {
				continue
			}
			out = append(out, Warning{File: name, Line: i + 1, Text: strings.TrimSpace(line)})
			count++
			if len(out) == 20 {
				return out
			}
			if count == 5 {
				break
			}
		}
	}
	return out
}
