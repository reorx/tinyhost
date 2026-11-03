package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/reorx/tinyhost/internal/server"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func init() {
	register(&command{name: "passwd", summary: "设置后台的 Owner 密码，并让所有后台会话失效", run: passwd})
}

const passwdUsage = `用法：tinyhost-server passwd [--data-dir DIR]

设置后台的 Owner 密码，并让所有后台会话失效。在终端里会要求输入两次；
不是终端时从标准输入读一行。`

func passwd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("passwd", flag.ContinueOnError)
	dir := fs.String("data-dir", "./data", "数据目录，和 serve 的 --data-dir 相同")
	if err := parseFlags(fs, args, out, passwdUsage); err != nil {
		return err
	}
	password, err := readPassword(out)
	if err != nil {
		return err
	}
	if len(password) == 0 {
		return fmt.Errorf("password must not be empty")
	}
	hash, err := bcrypt.GenerateFromPassword(password, 12)
	if err != nil {
		return err
	}
	s, err := openServer(*dir, server.Config{})
	if err != nil {
		return err
	}
	defer s.Store.Close()
	if err = s.Store.SetSetting("owner_password_hash", string(hash)); err != nil {
		return err
	}
	// Password rotation also invalidates existing browser sessions.
	if err = s.Store.DeleteAllSessions(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "密码已设置")
	return err
}
func readPassword(out io.Writer) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		return bytes.TrimRight([]byte(line), "\r\n"), nil
	}
	fmt.Fprint(out, "密码：")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(out)
	if err != nil {
		return nil, err
	}
	fmt.Fprint(out, "再次输入：")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(out)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(first, second) {
		return nil, fmt.Errorf("passwords do not match")
	}
	return first, nil
}
