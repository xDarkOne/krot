package agent

import (
	"os/exec"
	"strings"
	"testing"
)

func TestTrafficGuardInstallExemptions(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required to test the installer")
	}
	install, ok := Step("traffic-guard", "install")
	if !ok {
		t.Fatal("traffic-guard install step is missing")
	}
	const setup = `
set -euo pipefail
VK_RUN=$1
VK_NODES="192.0.2.1 192.0.2.2"
SSH_CONNECTION="203.0.113.1 54321 192.0.2.1 22"
unset VK_EXEMPT
if [[ -n $2 ]]; then VK_EXEMPT=$2; fi
mkdir -p "$VK_RUN/src"
printf 'fixture\n' > "$VK_RUN/src/traffic-guard"
apt_install() { :; }
mkdir() { :; }
put() {
    if [[ $1 == /etc/traffic-guard.exempt ]]; then
        cat
    else
        cat >/dev/null
    fi
}
systemctl() { :; }
function /usr/local/sbin/traffic-guard { :; }
die() { echo "$*" >&2; exit 1; }
`
	for _, tt := range []struct {
		name    string
		exempt  string
		wantIPs string
	}{
		{name: "unset", wantIPs: "192.0.2.1\n192.0.2.2\n203.0.113.1 # пульт"},
		{name: "configured", exempt: "198.51.100.1 2001:db8::/64", wantIPs: "192.0.2.1\n192.0.2.2\n198.51.100.1\n2001:db8::/64\n203.0.113.1 # пульт"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(bash, "-s", "--", t.TempDir(), tt.exempt)
			cmd.Stdin = strings.NewReader(setup + "\n" + install)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("installer failed: %v\n%s", err, out)
			}
			var ips []string
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if !strings.HasPrefix(line, "#") {
					ips = append(ips, line)
				}
			}
			if got := strings.Join(ips, "\n"); got != tt.wantIPs {
				t.Errorf("exemptions = %q, want %q", got, tt.wantIPs)
			}
		})
	}
}
