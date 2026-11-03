package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/reorx/tinyhost/internal/blob"
	"github.com/reorx/tinyhost/internal/notify"
	"github.com/reorx/tinyhost/internal/server"
	"github.com/reorx/tinyhost/internal/store"
	"github.com/reorx/tinyhost/internal/token"
	"github.com/reorx/tinyhost/internal/version"
)

type command struct {
	name, summary string
	run           func([]string, io.Writer) error
}

var commands = map[string]*command{}

func register(c *command) { commands[c.name] = c }
func init() {
	register(&command{name: "serve", summary: "启动服务：一个进程按 Host 提供 apps 域名和后台", run: serve})
	register(&command{name: "token", summary: "token create --name NAME [--ttl 30d]：在本机数据库里直接签发 Agent token", run: createToken})
	register(&command{name: "gc", summary: "立即做一次垃圾回收（serve 每小时自动做一次）", run: gc})
	register(&command{name: "version", summary: "打印版本", run: printVersion})
}
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(os.Args[1:], os.Stdout); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || (args[0] == "help" && len(args) == 1) {
		return usage(out)
	}
	if args[0] == "help" {
		args = []string{args[1], "-h"}
	}
	c := commands[args[0]]
	if c == nil {
		return fmt.Errorf("unknown command %q", args[0])
	}
	// Commands have already printed their usage for -h.
	if err := c.run(args[1:], out); !errors.Is(err, flag.ErrHelp) {
		return err
	}
	return nil
}
func printVersion(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	if err := parseFlags(fs, args, out, "用法：tinyhost-server version\n\n打印版本。"); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, version.Version)
	return err
}

const serveUsage = `用法：tinyhost-server serve [参数]

启动服务。同一个进程按请求的 Host 分流：--apps-url 的主机提供 App、Agent API
和安装脚本，--admin-url 的主机提供后台。`

// flags parses the options shared by serve, gc and healthcheck, so the same
// command line and environment work for all three.
func flags(name, about string, args []string, out io.Writer, c *server.Config, notifications ...*notify.Config) (string, string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var dir, listen string
	fs.StringVar(&dir, "data-dir", "./data", "存放数据库、文件块和下载缓存的目录")
	fs.StringVar(&listen, "listen", "127.0.0.1:3471", "HTTP 监听地址；:3471 表示监听所有网卡")
	fs.StringVar(&c.AppsURL, "apps-url", "", "apps 域名的源，如 https://tinyhost.space；serve 必填")
	fs.StringVar(&c.AdminURL, "admin-url", "", "后台域名的源；serve 必填，主机必须和 --apps-url 不同")
	fs.IntVar(&c.MaxUploadMB, "max-upload-mb", 50, "单次上传压缩包的大小上限，单位 MiB")
	fs.IntVar(&c.MaxFiles, "max-files", 1000, "单次上传的文件数上限")
	fs.IntVar(&c.DataVersionsKeep, "data-versions-keep", 20, "每个 App 保留的数据版本数，更早的由 GC 删除，主入口所在版本除外")
	fs.IntVar(&c.TrashDays, "trash-days", 7, "删除的 App 在回收站保留的天数，过期由 GC 清理")
	fs.StringVar(&c.TrustedProxyHeader, "trusted-proxy-header", "", "从这个请求头读取客户端 IP（如 CF-Connecting-IP），只在前面有可信反向代理时设置；不设则用连接地址")
	fs.BoolVar(&c.InsecureCookies, "insecure-cookies", false, "Cookie 不加 Secure 和 __Host- 前缀，只用于本地 http 开发")
	fs.StringVar(&c.ReleaseRepo, "release-repo", "reorx/tinyhost", "下载代理从这个 GitHub 仓库（owner/repo）的 Release 获取 CLI 包")
	fs.StringVar(&c.DLDir, "dl-dir", "", "本地 Release 产物目录；设置后下载代理只读这里，不访问 GitHub")
	n := &notify.Config{}
	if len(notifications) > 0 {
		n = notifications[0]
	}
	fs.StringVar(&n.NtfyURL, "ntfy-url", "", "ntfy 话题 URL；不设则不推送")
	fs.StringVar(&n.NtfyToken, "ntfy-token", "", "ntfy 的 Bearer token")
	fs.StringVar(&n.WebhookURL, "webhook-url", "", "通知 webhook URL，事件以 JSON POST 过去；不设则不发送")
	fs.StringVar(&n.Events, "notify-events", notify.DefaultEvents, "要通知的事件，逗号分隔")
	if err := parseFlags(fs, args, out, about); err != nil {
		return "", "", err
	}
	if c.MaxUploadMB <= 0 || c.MaxFiles <= 0 || c.DataVersionsKeep <= 0 || c.TrashDays <= 0 {
		return "", "", fmt.Errorf("limits must be positive")
	}
	return dir, listen, nil
}
func openServer(dir string, c server.Config) (*server.Server, error) {
	// store.Open constructs a file URI; an absolute path avoids a relative
	// directory being interpreted as the URI authority by SQLite.
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	c.DLCacheDir = filepath.Join(dir, "dl", strings.TrimPrefix(version.Version, "v"))
	db, err := store.Open(filepath.Join(dir, "tinyhost.db"))
	if err != nil {
		return nil, err
	}
	blobs, err := blob.New(dir)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &server.Server{Store: db, Blobs: blobs, Config: c}, nil
}
func serve(args []string, out io.Writer) error {
	var c server.Config
	var notifications notify.Config
	dir, listen, err := flags("serve", serveUsage, args, out, &c, &notifications)
	if err != nil {
		return err
	}
	if c.AdminURL == "" {
		return fmt.Errorf("serve requires --admin-url")
	}
	if err = c.Validate(); err != nil {
		return err
	}
	s, err := openServer(dir, c)
	if err != nil {
		return err
	}
	defer s.Store.Close()
	hub := notify.New(notifications, s.Logger)
	defer hub.Close()
	s.OnEvent = notificationCallback(s, hub)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	gcDone := make(chan struct{})
	go func() {
		defer close(gcDone)
		s.RunGC(ctx)
	}()
	defer func() {
		stop()
		<-gcDone
	}()
	httpServer := &http.Server{Addr: listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown failed", "error", err)
			_ = httpServer.Close()
		}
	}()
	if c.InsecureCookies {
		slog.Warn("insecure cookies enabled; development only")
	}
	slog.Info("listening", "address", listen, "apps_url", c.AppsURL)
	err = httpServer.ListenAndServe()
	stop()
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

const tokenUsage = `用法：tinyhost-server token create --name NAME [--ttl 30d] [--data-dir DIR]

在本机数据库里直接签发一个 Agent token，明文只打印这一次。不经过设备授权，
适合给云端 Agent 预先配置 TINYHOST_TOKEN。`

func createToken(args []string, out io.Writer) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		args = []string{"create", args[0]}
	}
	if len(args) == 0 || args[0] != "create" {
		return fmt.Errorf("usage: token create --name NAME [--ttl 30d]")
	}
	fs := flag.NewFlagSet("token create", flag.ContinueOnError)
	dir := fs.String("data-dir", "./data", "数据目录，和 serve 的 --data-dir 相同")
	name := fs.String("name", "", "Agent 名称，必填，显示在后台和操作记录里")
	ttl := fs.String("ttl", "", "有效期，如 30d、12h；不设则不过期")
	if err := parseFlags(fs, args[1:], out, tokenUsage, "name", "ttl"); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("token create requires --name")
	}
	expires, err := expiry(*ttl)
	if err != nil {
		return err
	}
	s, err := openServer(*dir, server.Config{})
	if err != nil {
		return err
	}
	defer s.Store.Close()
	plain := token.New()
	if _, err = s.Store.CreateClient(*name, token.Hash(plain), expires); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, plain)
	return err
}
func expiry(value string) (*int64, error) {
	if value == "" {
		return nil, nil
	}
	var duration time.Duration
	var err error
	if strings.HasSuffix(value, "d") {
		var days int64
		days, err = strconv.ParseInt(strings.TrimSuffix(value, "d"), 10, 64)
		if err == nil && (days <= 0 || days > 106751) {
			return nil, fmt.Errorf("ttl days out of range")
		}
		duration = time.Duration(days) * 24 * time.Hour
	} else {
		duration, err = time.ParseDuration(value)
	}
	if err != nil {
		return nil, err
	}
	if duration <= 0 {
		return nil, fmt.Errorf("ttl must be positive")
	}
	n := time.Now().Add(duration).Unix()
	return &n, nil
}

const gcUsage = `用法：tinyhost-server gc [参数]

立即做一次垃圾回收（serve 每小时自动做一次）：清理过期授权和预览、多余的数据
版本、回收站里过期的 App，以及没有引用的层和文件块。接受和 serve 相同的参数，
只用到 --data-dir、--data-versions-keep、--trash-days。`

func gc(args []string, out io.Writer) error {
	var c server.Config
	dir, _, err := flags("gc", gcUsage, args, out, &c)
	if err != nil {
		return err
	}
	s, err := openServer(dir, c)
	if err != nil {
		return err
	}
	defer s.Store.Close()
	if err = s.GC(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "GC 完成")
	return err
}
