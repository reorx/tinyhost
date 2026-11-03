package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// envName is the variable that sets a flag: --data-dir is TINYHOST_DATA_DIR.
func envName(flagName string) string {
	return "TINYHOST_" + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

// parseFlags applies the TINYHOST_* variable of every flag not listed in noEnv,
// then args, so the command line wins and a malformed variable fails like a
// malformed flag. -h and --help print usage to out and return flag.ErrHelp.
func parseFlags(fs *flag.FlagSet, args []string, out io.Writer, about string, noEnv ...string) error {
	fs.SetOutput(io.Discard)
	local := map[string]bool{}
	for _, name := range noEnv {
		local[name] = true
	}
	var envErr error
	fs.VisitAll(func(f *flag.Flag) {
		value, ok := os.LookupEnv(envName(f.Name))
		if local[f.Name] || !ok || envErr != nil {
			return
		}
		if err := fs.Set(f.Name, value); err != nil {
			envErr = fmt.Errorf("%s: %w", envName(f.Name), err)
		}
	})
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = io.WriteString(out, usageText(fs, about, local))
		return err
	}
	if envErr != nil {
		return envErr
	}
	if err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return nil
}

// usageText lists flags as "--name type", then the description, the built-in
// default and the variable. Defaults are the compiled ones, not the environment.
func usageText(fs *flag.FlagSet, about string, local map[string]bool) string {
	var b strings.Builder
	b.WriteString(about + "\n")
	first := true
	fs.VisitAll(func(f *flag.Flag) {
		if first {
			b.WriteString("\n参数（命令行优先于对应的环境变量）：\n")
			first = false
		}
		kind, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(&b, "  --%s", f.Name)
		if kind != "" {
			b.WriteString(" " + kind)
		}
		b.WriteString("\n        " + usage)
		if f.DefValue != "" && f.DefValue != "false" {
			fmt.Fprintf(&b, "（默认 %s）", f.DefValue)
		}
		if !local[f.Name] {
			b.WriteString("\n        环境变量 " + envName(f.Name))
		}
		b.WriteString("\n")
	})
	return b.String()
}

func usage(out io.Writer) error {
	var b strings.Builder
	b.WriteString("用法：tinyhost-server <命令> [参数]\n\n命令：\n")
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, "  %-12s %s\n", name, commands[name].summary)
	}
	b.WriteString("\n运行 tinyhost-server <命令> -h 查看参数。参数也能用环境变量 TINYHOST_<参数名> 设置，例如 --data-dir 对应 TINYHOST_DATA_DIR，命令行优先。\n")
	_, err := io.WriteString(out, b.String())
	return err
}
