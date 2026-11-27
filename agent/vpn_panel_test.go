package agent

import (
	"os/exec"
	"testing"
)

func TestPanelTelegramAccessAndDeviceTracking(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed to exercise the Mini App")
	}
	script := `
import hmac, json, pathlib, runpy, tempfile, urllib.parse
from hashlib import sha256

scope = runpy.run_path("vpn-panel/files/vpn-panel.py")
authenticate = scope["authenticate"]
settings = authenticate.__globals__
token = "local-" + "test-value"
now = 1800000000

def signed(user, timestamp):
    fields = {"user": json.dumps({"id": user}), "auth_date": str(timestamp)}
    check = "\n".join(f"{k}={v}" for k, v in sorted(fields.items()))
    key = hmac.digest(b"WebAppData", token.encode(), "sha256")
    fields["hash"] = hmac.new(key, check.encode(), sha256).hexdigest()
    return urllib.parse.urlencode(fields)

assert authenticate(signed(1234, now), token, {1234}, now) == 1234
assert authenticate(signed(4321, now), token, {1234}, now) is None
assert authenticate(signed(1234, now - 3601), token, {1234}, now) is None
assert authenticate(signed(1234, now) + "&user=null", token, {1234}, now) is None
assert authenticate(signed(1234, now), "different", {1234}, now) is None

with tempfile.TemporaryDirectory() as work:
    settings["DB"] = pathlib.Path(work) / "panel.db"
    sample = "ab" * 16
    settings["clients"] = lambda: {"fixture": {"uuid": "fixture-id", "token": sample}}
    settings["settings"] = lambda: (token, {1234})
    scope["observe"]("/u/" + sample, "hwid fixture", "INCY/Linux")
    scope["observe"]("/u/" + sample, "hwid fixture", "INCY/Linux")
    scope["observe"]("/u/" + "cd" * 16, "invalid client", "Android")
    with scope["database"]() as db:
        assert len(scope["user_devices"](db, "fixture-id")) == 1
        assert db.execute("SELECT platform FROM device").fetchone()[0] == "Linux"
        assert db.execute("SELECT identifier FROM device").fetchone()[0] != "hwid fixture"

generator = runpy.run_path("common/files/vpnkit-gen.py")
conf = generator["singbox_config"]({"clients": {"fixture": {"uuid": "test-id", "hy2": "test-pass"}}},
                                  {"SOCKS_PORT": "10801"},
                                  {"TUNNEL_NAME": "vpn.example.org", "HY2_PORT": "443",
                                   "HY2_PER_CLIENT": "1", "HY2_VLESS_PORT": "10802"})
assert conf["route"]["rules"] == [{"auth_user": ["fixture"], "outbound": "hy2-user-fixture"}]
assert conf["outbounds"][1]["uuid"] == "test-id"
`
	out, err := exec.Command(python, "-B", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("Mini App tests: %v\n%s", err, out)
	}
}
