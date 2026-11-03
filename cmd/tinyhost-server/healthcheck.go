package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/reorx/tinyhost/internal/server"
)

func init() {
	register(&command{name: "healthcheck", summary: "探测本机正在运行的 serve：健康退出 0，否则退出 1", run: healthcheck})
}

const healthcheckUsage = `用法：tinyhost-server healthcheck [参数]

向本机 serve 的监听地址请求 /_/health，健康退出 0，否则退出 1；不需要凭证，
也不读写数据目录。供容器的 HEALTHCHECK 使用（distroless 镜像里没有 shell 和 curl）。
接受和 serve 相同的参数和环境变量，只用到 --listen。`

func healthcheck(args []string, out io.Writer) error {
	var c server.Config
	_, listen, err := flags("healthcheck", healthcheckUsage, args, out, &c)
	if err != nil {
		return err
	}
	target, err := healthURL(listen)
	if err != nil {
		return err
	}
	// Never route a loopback probe through HTTP_PROXY.
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Get(target)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d", target, response.StatusCode)
	}
	_, err = fmt.Fprintln(out, "ok")
	return err
}

// healthURL dials loopback when serve listens on every interface.
func healthURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("listen address %q: %w", listen, err)
	}
	ip := net.ParseIP(host)
	if host == "" || (ip != nil && ip.IsUnspecified() && ip.To4() != nil) {
		host = "127.0.0.1"
	} else if ip != nil && ip.IsUnspecified() {
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/_/health", nil
}
