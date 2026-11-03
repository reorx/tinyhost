package main

import (
	"bytes"
	"strings"
	"testing"
)

// serveFlags is every flag serve accepts, from the spec plus later milestones.
var serveFlags = []string{
	"data-dir", "listen", "apps-url", "admin-url",
	"max-upload-mb", "max-files", "data-versions-keep", "trash-days",
	"trusted-proxy-header", "insecure-cookies", "release-repo", "dl-dir",
	"ntfy-url", "ntfy-token", "webhook-url", "notify-events",
}

// flagHelp returns the help block of one flag: its "--name" line and the
// indented description lines below it.
func flagHelp(t *testing.T, help, name string) string {
	t.Helper()
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "--"+name {
			continue
		}
		block := []string{line}
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" || strings.HasPrefix(strings.TrimSpace(next), "--") {
				break
			}
			block = append(block, next)
		}
		return strings.Join(block, "\n")
	}
	t.Fatalf("help lacks --%s:\n%s", name, help)
	return ""
}

// Behavior: serve -h and --help print every serve flag with a description and the TINYHOST_* variable that sets it, and succeed.
func TestServeHelpListsEveryFlagWithItsEnvironmentVariable(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		var out bytes.Buffer
		if err := run([]string{"serve", arg}, &out); err != nil {
			t.Fatalf("serve %s: %v", arg, err)
		}
		help := out.String()
		for _, name := range serveFlags {
			block := flagHelp(t, help, name)
			env := "TINYHOST_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
			if !strings.Contains(block, env) {
				t.Errorf("serve %s: --%s does not name %s:\n%s", arg, name, env, block)
			}
			if strings.Count(block, "\n") < 2 {
				t.Errorf("serve %s: --%s has no description:\n%s", arg, name, block)
			}
		}
		if !strings.Contains(help, "./data") || !strings.Contains(help, "127.0.0.1:3471") {
			t.Errorf("serve %s: defaults missing:\n%s", arg, help)
		}
	}
}

// Behavior: every subcommand and the top level answer -h with usage instead of failing; flags without a variable do not claim one.
func TestEveryCommandPrintsHelp(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}, {}} {
		out.Reset()
		if err := run(args, &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		for _, name := range []string{"serve", "passwd", "token", "gc", "healthcheck", "version"} {
			if !strings.Contains(out.String(), name) {
				t.Errorf("%v: top-level help lacks %s:\n%s", args, name, out.String())
			}
		}
	}
	for _, tc := range []struct {
		args       []string
		env, local []string
	}{
		{[]string{"gc", "-h"}, serveFlags, nil},
		{[]string{"healthcheck", "-h"}, serveFlags, nil},
		{[]string{"help", "serve"}, serveFlags, nil},
		{[]string{"passwd", "-h"}, []string{"data-dir"}, nil},
		{[]string{"token", "create", "-h"}, []string{"data-dir"}, []string{"name", "ttl"}},
		{[]string{"token", "-h"}, []string{"data-dir"}, []string{"name", "ttl"}},
		{[]string{"version", "-h"}, nil, nil},
	} {
		out.Reset()
		if err := run(tc.args, &out); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		help := out.String()
		if !strings.Contains(help, "tinyhost-server "+tc.args[0]) && !strings.Contains(help, "tinyhost-server "+tc.args[1]) {
			t.Errorf("%v: usage line missing:\n%s", tc.args, help)
		}
		for _, name := range tc.env {
			if block := flagHelp(t, help, name); !strings.Contains(block, "TINYHOST_") {
				t.Errorf("%v: --%s lacks its variable:\n%s", tc.args, name, block)
			}
		}
		for _, name := range tc.local {
			if block := flagHelp(t, help, name); strings.Contains(block, "TINYHOST_") {
				t.Errorf("%v: --%s claims a variable it does not read:\n%s", tc.args, name, block)
			}
		}
	}
}
