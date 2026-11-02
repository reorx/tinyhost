// Package cli implements the Agent CLI using the frozen internal/api DTOs.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/reorx/tinyhost/internal/api"
)

type command struct {
	Name    string
	Summary string
	Run     func(*context, []string) (any, error)
	Sub     map[string]*command
}
type context struct {
	stdout, stderr io.Writer
	json           bool
	pending        bool
	requestTimeout time.Duration
	server         string
}
type commandError struct {
	detail api.ErrorDetail
	exit   int
}

func (e *commandError) Error() string { return e.detail.Message }
func usage(message string) error {
	return &commandError{detail: api.ErrorDetail{Code: "usage", Message: message, Hint: "运行 tinyhost help 查看用法"}, exit: 2}
}

var commands []*command

func register(c *command) { commands = append(commands, c) }
func find(name string) *command {
	for _, c := range commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Run returns the process exit status. JSON documents, including errors, go to
// stdout; human errors and non-fatal packaging/writeback warnings go to stderr.
func Run(args []string, stdout, stderr io.Writer) int {
	c := &context{stdout: stdout, stderr: stderr}
	result, err := c.execute(args)
	if err == nil {
		if c.json {
			err = writeJSON(stdout, result)
		} else {
			err = c.printHuman(result)
		}
	}
	if err == nil {
		if c.pending {
			return 4
		}
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		if c.json {
			err = writeJSON(stdout, api.HelpResult{Help: helpText()})
		} else {
			_, err = io.WriteString(stdout, helpText())
		}
		if err == nil {
			return 0
		}
	}
	failure := &commandError{detail: api.ErrorDetail{Code: "cli_error", Message: err.Error(), Hint: ""}, exit: 1}
	var known *commandError
	if errors.As(err, &known) {
		failure = known
	}
	if c.json {
		if writeErr := writeJSON(stdout, api.Error{Error: failure.detail}); writeErr != nil {
			fmt.Fprintf(stderr, "错误：无法写入 JSON 输出：%v\n", writeErr)
			return 1
		}
	} else {
		fmt.Fprintf(stderr, "错误：%s\n", failure.detail.Message)
		if failure.detail.CreatedBy != "" {
			fmt.Fprintf(stderr, "创建者：%s\n", failure.detail.CreatedBy)
		}
		if failure.detail.CreatedAt != nil {
			fmt.Fprintf(stderr, "创建时间：%s\n", timestamp(*failure.detail.CreatedAt))
		}
		if len(failure.detail.ExistingDirs) > 0 {
			fmt.Fprintf(stderr, "现有覆盖目录：%s\n", strings.Join(failure.detail.ExistingDirs, ", "))
		}
		if failure.detail.Hint != "" {
			fmt.Fprintf(stderr, "提示：%s\n", failure.detail.Hint)
		}
	}
	return failure.exit
}
func (c *context) execute(args []string) (any, error) {
	fs := c.flags("tinyhost")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, err
		}
		return nil, usage(err.Error())
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return api.HelpResult{Help: helpText()}, nil
	}
	name := rest[0]
	cmd := find(name)
	if cmd == nil {
		// Parse trailing global flags even for unknown commands, so --json errors
		// use the same stream and format as successful invocations.
		_, _ = c.parse(c.flags("tinyhost"), rest[1:])
		return nil, usage("未知命令：" + name)
	}
	rest = rest[1:]
	if cmd.Sub != nil {
		nestedFlags := c.flags(cmd.Name)
		if err := nestedFlags.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usage(err.Error())
		}
		rest = nestedFlags.Args()
		if len(rest) == 0 {
			return nil, usage("data 需要子命令 push 或 rm")
		}
		sub := cmd.Sub[rest[0]]
		if sub == nil {
			_, _ = c.parse(c.flags("data"), rest[1:])
			return nil, usage("未知 data 子命令：" + rest[0])
		}
		cmd = sub
		rest = rest[1:]
	}
	return cmd.Run(c, rest)
}
func (c *context) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&c.json, "json", c.json, "输出 JSON")
	fs.StringVar(&c.server, "server", c.server, "服务器地址")
	return fs
}
func (c *context) parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if args[0] == "--" {
			positional = append(positional, args[1:]...)
			break
		}
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usage(err.Error())
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return positional, nil
}
func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
func helpText() string {
	var b strings.Builder
	b.WriteString("AGENTS: run \"tinyhost skill\" and read it fully before first use.\n\n用法：tinyhost [--server URL] [--json] <命令> [参数]\n\n")
	for _, cmd := range commands {
		fmt.Fprintf(&b, "  %-12s %s\n", cmd.Name, cmd.Summary)
	}
	b.WriteString("\n全局参数 --server、--json 可放在命令前后。\n服务器：--server > TINYHOST_SERVER > 系统配置目录 tinyhost/config.json。\n凭证：tinyhost login，或设置 TINYHOST_TOKEN。\n退出码：0 成功，1 一般错误，2 用法错误，3 未授权，4 授权等待中。\n")
	return b.String()
}
