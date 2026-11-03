package scripts

import (
	"os"
	"strings"
	"testing"
)

// finalStage returns the instructions of the Dockerfile's last stage, one per
// element, without comments.
func finalStage(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	var stage []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "FROM ") {
			stage = nil
		}
		stage = append(stage, line)
	}
	return stage
}
func indexOf(stage []string, match func(string) bool) int {
	for i, line := range stage {
		if match(line) {
			return i
		}
	}
	return -1
}

// Behavior: the server image runs as distroless nonroot (65532), ships /data owned by it before declaring the volume so fresh named volumes are writable, probes itself with the healthcheck subcommand, and keeps the CMD deployments rely on.
func TestServerImageRunsAsNonrootWithWritableDataAndHealthcheck(t *testing.T) {
	stage := finalStage(t)
	user := indexOf(stage, func(l string) bool { return l == "USER 65532:65532" })
	data := indexOf(stage, func(l string) bool {
		return strings.HasPrefix(l, "COPY ") && strings.Contains(l, "--chown=65532:65532") && strings.HasSuffix(l, " /data")
	})
	volume := indexOf(stage, func(l string) bool { return l == "VOLUME /data" })
	health := indexOf(stage, func(l string) bool {
		return strings.HasPrefix(l, "HEALTHCHECK ") && strings.Contains(l, `CMD ["/tinyhost-server", "healthcheck", "--listen", ":3471"]`)
	})
	cmd := indexOf(stage, func(l string) bool { return l == `CMD ["serve", "--data-dir", "/data", "--listen", ":3471"]` })
	if user < 0 || data < 0 || volume < 0 || health < 0 || cmd < 0 {
		t.Fatalf("user=%d data=%d volume=%d health=%d cmd=%d\n%s", user, data, volume, health, cmd, strings.Join(stage, "\n"))
	}
	// Changes to a volume path after VOLUME are discarded.
	if data > volume {
		t.Fatalf("/data is populated after VOLUME:\n%s", strings.Join(stage, "\n"))
	}
	if later := indexOf(stage[user+1:], func(l string) bool { return strings.HasPrefix(l, "USER ") }); later >= 0 {
		t.Fatalf("a later USER overrides nonroot:\n%s", strings.Join(stage, "\n"))
	}
}
