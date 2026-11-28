package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVpnkitGenXHTTP443AndXFF(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed to test vpnkit-gen.py")
	}
	script := `
import runpy

scope = runpy.run_path("common/files/vpnkit-gen.py")
stream_conf = scope["stream_conf"]
xray_config = scope["xray_config"]

# 1. Entry with MTProxyL + WEB Proxy + XHTTP on 443: tunnel_name routes to 127.0.0.1:4444
m = {"BACKEND": "mtproxyl", "LISTEN_PORT": "15443", "TLS_DOMAIN": "ru.example.org", "WEB_DOMAIN": "web.example.org", "WEB_TLS_PORT": "15444"}
n = {"STREAM": "1", "TUNNEL_NAME": "es.example.org", "XHTTP_PORT": "443"}
sc = stream_conf(m, n)
assert "web.example.org 127.0.0.1:15444;" in sc, sc
assert "es.example.org 127.0.0.1:4444;" in sc, sc
assert "default 127.0.0.1:15443;" in sc, sc
assert "proxy_protocol on;" in sc, sc

# 2. Entry without MTProxy: everything goes to 127.0.0.1:8444 without proxy_protocol
sc_nom = stream_conf(None, n)
assert "default 127.0.0.1:8444;" in sc_nom, sc_nom
assert "proxy_protocol on;" not in sc_nom, sc_nom

# 3. Single-domain solo with MTProxy (TUNNEL_NAME == TLS_DOMAIN): do not hijack TLS_DOMAIN from telemt
m_solo = {"BACKEND": "telemt", "LISTEN_PORT": "15443", "TLS_DOMAIN": "vpn.example.org"}
n_solo = {"STREAM": "1", "TUNNEL_NAME": "vpn.example.org", "XHTTP_PORT": "443"}
sc_solo = stream_conf(m_solo, n_solo)
assert "127.0.0.1:4444" not in sc_solo, sc_solo

# 4. Xray config sets trustedXForwardedFor=["X-Forwarded-For"] on xh-in
u = {"vlessenc": {"decryption": "none", "encryption": "none"}, "clients": {"alice": {"uuid": "<TEST_UUID>", "token": "0"*32, "hy2": "test"}}}
x = {"XH_PORT": "11443", "XH_PATH": "/static/abc", "ENC_PORT": "31443", "ENC_PATH": "/ab", "SOCKS_PORT": "10801", "CASCADE": "0", "TUNNEL_NAME": "es.example.org", "XHTTP_PORT": "443"}
cfg = xray_config(u, x, None)
xh = next(i for i in cfg["inbounds"] if i["tag"] == "xh-in")
assert xh["streamSettings"]["sockopt"]["trustedXForwardedFor"] == ["X-Forwarded-For"], xh
`
	out, err := exec.Command(python, "-B", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("vpnkit-gen.py tests failed: %v\n%s", err, out)
	}
}

func TestVpnkitReipIPv4AndIPv6(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to test vpnkit-reip")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is needed to test vpnkit-reip")
	}
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("etc/network/interfaces", "address 203.0.113.10/24\ngateway 203.0.113.1\n")
	write("etc/network/interfaces.d/ipv6-link", "address 2001:db8:1::2/64\n")
	write("var/lib/vpnkit/node.addr", "V4=203.0.113.10\n")
	write("etc/vpnkit/watch.conf", "EXIT_IP=198.51.100.20\n")
	write("etc/vpnkit/cascade/watch.json", `{"exit_host":"198.51.100.20","endpoint":"198.51.100.20:43819"}`+"\n")

	cmd := exec.Command("bash", "relocate/files/vpnkit-reip",
		"--map", "203.0.113.10", "192.0.2.10",
		"--map", "203.0.113.1", "192.0.2.1",
		"--map", "198.51.100.20", "192.0.2.20",
		"--map", "2001:db8:1::2", "2001:db8:2::2",
		"--apply",
	)
	cmd.Env = append(os.Environ(), "REIP_ROOT="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpnkit-reip failed: %v\n%s", err, out)
	}
	check := func(rel, want string) {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("%s does not contain %q:\n%s", rel, want, string(b))
		}
	}
	check("etc/network/interfaces", "address 192.0.2.10/24")
	check("etc/network/interfaces", "gateway 192.0.2.1")
	check("etc/network/interfaces.d/ipv6-link", "address 2001:db8:2::2/64")
	check("var/lib/vpnkit/node.addr", "V4=192.0.2.10")
	check("etc/vpnkit/watch.conf", "EXIT_IP=192.0.2.20")
	check("etc/vpnkit/cascade/watch.json", `"endpoint":"192.0.2.20:43819"`)
}
