package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"time"

	"github.com/reorx/tinyhost/internal/api"
	"github.com/reorx/tinyhost/internal/version"
)

type credential struct {
	Token     string `json:"token"`
	ClientID  string `json:"client_id"`
	Name      string `json:"name"`
	ExpiresAt *int64 `json:"expires_at"`
}
type pendingLogin struct {
	Server                  string `json:"server"`
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresAt               int64  `json:"expires_at"`
	Interval                int    `json:"interval,omitempty"`
}

func authPath(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tinyhost", name), nil
}
func readAuthFile(name string, value any) error {
	p, err := authPath(name)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
func saveAuthFile(name string, value any) error {
	p, err := authPath(name)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".auth-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}
func removeAuthFile(name string) error {
	p, err := authPath(name)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func credentials() (map[string]credential, error) {
	values := map[string]credential{}
	err := readAuthFile("credentials.json", &values)
	if os.IsNotExist(err) {
		err = nil
	}
	return values, err
}
func credentialToken(server string) (string, error) {
	values, err := credentials()
	return values[server].Token, err
}
func (c *context) token() (string, error) {
	if key := os.Getenv("TINYHOST_TOKEN"); key != "" {
		return key, nil
	}
	origin, err := c.serverURL()
	if err != nil {
		return "", err
	}
	return credentialToken(origin)
}
func init() {
	register(&command{Name: "login", Summary: "申请设备授权或继续等待批准", Run: loginCommand})
	register(&command{Name: "logout", Summary: "撤销当前凭证并退出", Run: logoutCommand})
}
func loginCommand(c *context, args []string) (any, error) {
	fs := c.flags("login")
	name := fs.String("name", "", "")
	wait := fs.Int("wait", 60, "")
	force := fs.Bool("force", false, "")
	rest, err := c.parse(fs, args)
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 || *wait < 0 {
		return nil, usage("login [--name 名称] [--wait 秒] [--force]")
	}
	origin, err := c.serverURL()
	if err != nil {
		return nil, err
	}
	if os.Getenv("TINYHOST_TOKEN") != "" {
		return api.LoginResult{Status: "environment", Server: origin}, nil
	}
	if !*force {
		key, err := credentialToken(origin)
		if err != nil {
			return nil, err
		}
		if key != "" {
			var who api.WhoamiResult
			err = c.request("GET", "/whoami", nil, nil, "", &who)
			if err == nil {
				return api.LoginResult{Status: "already_logged_in", Name: who.Client.Name, Server: origin, ExpiresAt: who.Client.ExpiresAt}, nil
			}
			var known *commandError
			if !errors.As(err, &known) || known.exit != 3 {
				return nil, err
			}
		}
	}
	var pending pendingLogin
	err = readAuthFile("pending-login.json", &pending)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if *force || pending.Server != origin {
		host, err := os.Hostname()
		if err != nil {
			return nil, err
		}
		if *name == "" {
			username := "agent"
			if u, err := user.Current(); err == nil {
				username = u.Username
			}
			*name = username + "@" + host
		}
		input := api.DeviceCodeRequest{Name: *name, Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH, CLIVersion: version.Version}
		data, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		var result api.DeviceCodeResult
		if err = c.publicRequest("/device/code", data, &result); err != nil {
			return nil, err
		}
		pending = pendingLogin{Server: origin, DeviceCode: result.DeviceCode, UserCode: result.UserCode, VerificationURIComplete: result.VerificationURIComplete, ExpiresAt: time.Now().Unix() + int64(result.ExpiresIn), Interval: result.Interval}
		if err = saveAuthFile("pending-login.json", pending); err != nil {
			return nil, err
		}
	}
	return c.waitForApproval(pending, time.Duration(*wait)*time.Second)
}
func (c *context) waitForApproval(pending pendingLogin, wait time.Duration) (any, error) {
	fmt.Fprintf(c.stderr, "请把下面的链接发给用户，请他打开并批准（验证码 %s，10 分钟内有效）：\n  %s\n等待批准（最多 %.0f 秒）…\n", pending.UserCode, pending.VerificationURIComplete, wait.Seconds())
	deadline := time.Now().Add(wait)
	interval := time.Duration(pending.Interval) * time.Second
	if interval < time.Second {
		interval = 3 * time.Second
	}
	// A zero wait still prints and saves the request, without a polling delay.
	for wait > 0 {
		if time.Until(deadline) <= 0 {
			break
		}
		c.requestTimeout = time.Until(deadline)
		data, _ := json.Marshal(struct {
			DeviceCode string `json:"device_code"`
		}{pending.DeviceCode})
		var result api.DeviceTokenResult
		err := c.publicRequest("/device/token", data, &result)
		c.requestTimeout = 0
		if err != nil && !time.Now().Before(deadline) {
			break
		}
		if err == nil {
			values, err := credentials()
			if err != nil {
				return nil, err
			}
			values[pending.Server] = credential{Token: result.Token, ClientID: result.Client.ID, Name: result.Client.Name, ExpiresAt: result.ExpiresAt}
			if err = saveAuthFile("credentials.json", values); err != nil {
				return nil, err
			}
			if err = removeAuthFile("pending-login.json"); err != nil {
				return nil, err
			}
			return api.LoginResult{Status: "authorized", Name: result.Client.Name, Server: pending.Server, ExpiresAt: result.ExpiresAt}, nil
		}
		var known *commandError
		if !errors.As(err, &known) {
			return nil, err
		}
		switch known.detail.Code {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied", "expired_token":
			if e := removeAuthFile("pending-login.json"); e != nil {
				return nil, e
			}
			return nil, err
		default:
			return nil, err
		}
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		if left < interval {
			time.Sleep(left)
			break
		}
		time.Sleep(interval)
	}
	c.pending = true
	return api.LoginResult{Status: "pending", Server: pending.Server, UserCode: pending.UserCode, VerificationURIComplete: pending.VerificationURIComplete}, nil
}
func (c *context) publicRequest(path string, data []byte, result any) error {
	return c.httpRequest("POST", path, nil, bytes.NewReader(data), "application/json", "", result)
}
func logoutCommand(c *context, args []string) (any, error) {
	rest, err := c.parse(c.flags("logout"), args)
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 {
		return nil, usage("logout 不接受位置参数")
	}
	origin, err := c.serverURL()
	if err != nil {
		return nil, err
	}
	var result api.LogoutResult
	requestErr := c.request("POST", "/logout", nil, nil, "", &result)
	// An already revoked token is also safe to remove locally.
	var known *commandError
	if requestErr != nil && (!errors.As(requestErr, &known) || known.exit != 3) {
		return nil, requestErr
	}
	values, err := credentials()
	if err != nil {
		return nil, err
	}
	delete(values, origin)
	if err = saveAuthFile("credentials.json", values); err != nil {
		return nil, err
	}
	var pending pendingLogin
	if err = readAuthFile("pending-login.json", &pending); err == nil && pending.Server == origin {
		if err = removeAuthFile("pending-login.json"); err != nil {
			return nil, err
		}
	}
	return api.LogoutResult{LoggedOut: true}, nil
}
