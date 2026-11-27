package agent

import (
	"os/exec"
	"testing"
)

func TestCascadeWatchTransitions(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed to exercise the cascade watchdog")
	}
	script := `
import runpy

scope = runpy.run_path("cascade-awg0/files/cascade-watch.py")
Watch = scope["Watch"]
globals_ = Watch.check_route.__globals__
globals_["https_probe"] = lambda interface, host: interface == "awg0"

class Simulated(Watch):
    interface = "awg0"
    backup_ok = False
    transport = "192.0.2.20:443"

    def current_endpoint(self):
        return self.transport

    def route(self):
        return self.interface

    def select_route(self, interface):
        self.interface = interface
        return True

    def backup_ready(self):
        return self.backup_ok

config = {
    "peer": "peer", "tunnel_ip": "10.100.0.1", "endpoint": "192.0.2.20:443",
    "endpoint_v6": "[2001:db8::20]:443", "exit_host": "192.0.2.20", "backup": True,
    "telegram_prefixes": ["149.154.160.0/20"],
}
watch = Simulated(config)
watch.fails = 3
watch.check_route()
assert watch.route() == "awg0", "unhealthy backup must never take over"
watch.backup_ok = True
watch.check_route()
assert watch.route() == "sbtun", "three failures with healthy backup must switch the route"
watch.oks = 7
watch.check_route()
assert watch.route() == "sbtun", "short recovery must not trigger failback"
watch.oks = 8
watch.check_route()
assert watch.route() == "awg0", "healthy primary must take the route back"
watch.interface = "sbtun"
watch.backup_ok = False
watch.check_route()
assert watch.route() == "sbtun"
watch.check_route()
assert watch.route() == "awg0", "two lost backup probes must return traffic to the primary"

from subprocess import CompletedProcess
def awg_set(*args):
    assert args[:5] == ("awg", "set", "awg0", "peer", "peer"), args
    watch.transport = args[-1]
    return CompletedProcess(args, 0, "", "")

globals_["command"] = awg_set
globals_["ping"] = lambda *args: False
assert not watch.set_family(True)
assert not watch.v6 and watch.transport == config["endpoint"], "failed IPv6 trial must restore IPv4"
globals_["ping"] = lambda *args: True
assert watch.set_family(True) and watch.v6
assert watch.set_family(False) and not watch.v6
`
	out, err := exec.Command(python, "-B", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("watchdog transition tests: %v\n%s", err, out)
	}
}
