package cli

import (
	"fmt"
	"strings"

	"github.com/reorx/tinyhost/internal/version"
	"golang.org/x/mod/semver"
)

func (c *context) upgradeHint() {
	local := "v" + strings.TrimPrefix(version.Version, "v")
	remote := "v" + strings.TrimPrefix(c.remoteVersion, "v")
	if !semver.IsValid(local) || !semver.IsValid(remote) || semver.Compare(local, remote) >= 0 {
		return
	}
	fmt.Fprintf(c.stderr, "服务器版本 %s 较新，请升级：\ncurl -fsSL %s/install.sh | sh\n", c.remoteVersion, c.remoteOrigin)
}
