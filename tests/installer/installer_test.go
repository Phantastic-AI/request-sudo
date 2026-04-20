package installer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallScriptInstallsBinariesAndUnitIntoTempRoot(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	tmp := t.TempDir()
	prefix := filepath.Join(tmp, "usr", "local")
	systemdDir := filepath.Join(tmp, "etc", "systemd", "system")
	stateDir := filepath.Join(tmp, "var", "lib", "request-sudo")
	cmd := exec.Command(filepath.Join(repoRoot, "scripts", "install.sh"),
		"--prefix", prefix,
		"--systemd-dir", systemdDir,
		"--state-dir", stateDir,
		"--review-uids", "1234",
		"--review-gids", "5678",
	)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	for _, rel := range []string{"bin/request-sudo", "bin/request-sudoctl", "bin/request-sudod"} {
		if _, err := os.Stat(filepath.Join(prefix, rel)); err != nil {
			t.Fatalf("missing installed file %s: %v", rel, err)
		}
	}
	unitPath := filepath.Join(systemdDir, "request-sudod.service")
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{"request-sudod", "--review-uids 1234", "--review-gids 5678", stateDir + "/events.jsonl", "NoNewPrivileges=true", "PrivateTmp=true", "ProtectSystem=strict"} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("unit missing %q\n%s", snippet, content)
		}
	}
}
