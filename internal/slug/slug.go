// Package slug validates app names and version URL segments.
package slug

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var pattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)
var reserved = map[string]bool{
	"api": true, "admin": true, "www": true, "static": true,
	"assets": true, "login": true, "logout": true, "auth": true,
}

func Valid(s string) bool {
	return pattern.MatchString(s) && !strings.Contains(s, "--") && !reserved[s]
}

func ParseSegment(seg string) (string, int, error) {
	name, number, versioned := strings.Cut(seg, "@v")
	if !Valid(name) {
		return "", 0, fmt.Errorf("invalid slug: %q", name)
	}
	if !versioned {
		return name, 0, nil
	}
	for _, c := range number {
		if c < '0' || c > '9' {
			return "", 0, fmt.Errorf("invalid version: %q", number)
		}
	}
	n, err := strconv.Atoi(number)
	if err != nil || n <= 0 {
		return "", 0, fmt.Errorf("invalid version: %q", number)
	}
	return name, n, nil
}

type Ref struct {
	Name    string
	Version int
	Preview string
}

var previewPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

func ValidPreviewHash(hash string) bool { return previewPattern.MatchString(hash) }

func ParseRef(segment string) (Ref, error) {
	name, hash, found := strings.Cut(segment, "@")
	if found && Valid(name) && ValidPreviewHash(hash) {
		return Ref{Name: name, Preview: hash}, nil
	}
	name, version, err := ParseSegment(segment)
	return Ref{Name: name, Version: version}, err
}
