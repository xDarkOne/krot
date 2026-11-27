package backup

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCreateBackupScript(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("backup service uses Linux shell utilities")
	}

	for _, tc := range []struct {
		name   string
		action string
		want   string
	}{
		{"new archive", `touch "$BACKUP_DIR/node-2.tar.gz.age"`, "node-2.tar.gz.age"},
		{"new archive with the same timestamped name", `touch "$BACKUP_DIR/new"
mv "$BACKUP_DIR/new" "$BACKUP_DIR/node-1.tar.gz.age"`, "node-1.tar.gz.age"},
		{"retry after joining an existing run", `if [ -e "$BACKUP_DIR/joined" ]; then
    touch "$BACKUP_DIR/node-2.tar.gz.age"
else
    touch "$BACKUP_DIR/joined"
fi`, "node-2.tar.gz.age"},
		{"service returns without a new archive", "return 0", ""},
		{"service fails", "return 1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			old := filepath.Join(dir, "node-1.tar.gz.age")
			if err := os.WriteFile(old, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			earlier := time.Unix(1, 0)
			if err := os.Chtimes(old, earlier, earlier); err != nil {
				t.Fatal(err)
			}
			// Stub systemctl while keeping the actual shell code and filesystem commands.
			script := "systemctl() {\n" + tc.action + "\n}\n" +
				strings.ReplaceAll(createBackupScript, "/var/backups/vpnkit", `"$BACKUP_DIR"`)
			cmd := exec.Command("sh", "-s")
			cmd.Env = append(os.Environ(), "BACKUP_DIR="+dir)
			cmd.Stdin = strings.NewReader(script)
			out, err := cmd.CombinedOutput()
			if tc.want == "" {
				if err == nil {
					t.Fatalf("reported success with no new archive: %s", out)
				}
				return
			}
			if err != nil || strings.TrimSpace(string(out)) != filepath.Join(dir, tc.want) {
				t.Fatalf("script returned %q, %v", out, err)
			}
		})
	}
}
