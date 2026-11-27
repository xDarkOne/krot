#!/usr/bin/python3
"""Select the transport to the exit and update the entry's policy route."""

from concurrent.futures import ThreadPoolExecutor
import json
import socket
import subprocess
import sys
import time


INTERVAL = 10
FAIL_TO_SWITCH = 3
OK_TO_RETURN = 8
BACKUP_FAIL_TO_RETURN = 2
V4_PROBE_INTERVAL = 600
V4_PROBES_TO_RETURN = 3
TG_FAIL_TO_WARP = 6
TG_OK_TO_RETURN = 12
TG_WARP_FAIL_TO_RETURN = 3
TG_PROBES = ("149.154.167.50", "149.154.175.50", "91.108.4.204")
HTTPS_PROBES = (
    ("https://1.1.1.1/cdn-cgi/trace", "ip="),
    ("https://checkip.amazonaws.com", ""),
)


def command(*args):
    try:
        return subprocess.run(args, capture_output=True, text=True, errors="replace", timeout=9)
    except subprocess.TimeoutExpired:
        return subprocess.CompletedProcess(args, 124, "", "")


def curl(interface, *args):
    return command("/usr/bin/curl", "-4sS", "--noproxy", "*", "--interface", "if!" + interface,
                   "--connect-timeout", "4", "--max-time", "6", *args)


def addresses(host):
    result = command("/usr/bin/getent", "ahostsv4", host)
    return {line.split()[0] for line in result.stdout.splitlines() if line.strip()}


def https_probe(interface, host):
    expected = addresses(host)
    if not expected:
        return False

    def request(probe):
        url, prefix = probe
        result = curl(interface, "--fail", "--max-filesize", "4096", url)
        if result.returncode:
            return False
        if not prefix:
            return result.stdout.strip() in expected
        found = [line.removeprefix(prefix) for line in result.stdout.splitlines() if line.startswith(prefix)]
        return len(found) == 1 and found[0] in expected

    with ThreadPoolExecutor(max_workers=len(HTTPS_PROBES)) as pool:
        return any(pool.map(request, HTTPS_PROBES))


def log(message):
    print(message, flush=True)


def ping(interface, peer, count="3"):
    family = "-6" if ":" in peer else "-4"
    args = ["-I", interface] if interface else []
    return command("ping", family, *args, "-q", "-c", count, "-i", "0.3", "-W", "1", peer).returncode == 0


def tcp_probe(host, port):
    try:
        with socket.create_connection((host, port), timeout=2):
            return True
    except OSError:
        return False


def endpoint_v6(endpoint):
    return endpoint.startswith("[")


class Watch:
    def __init__(self, config):
        self.config = config
        self.primary = config["endpoint"]
        self.alternate = config["endpoint_v6"]
        self.host = config["exit_host"]
        self.peer = config["peer"]
        self.tunnel_ip = config["tunnel_ip"]
        self.prefixes = config["telegram_prefixes"]
        self.fails = self.oks = self.backup_fails = 0
        self.v4_probes = self.retry_v4 = 0
        self.v4_retry_delay = V4_PROBE_INTERVAL
        self.v6 = bool(self.alternate and endpoint_v6(self.current_endpoint()))
        self.tg_fails = self.tg_oks = self.tg_warp_fails = 0
        self.tg_warp = False

    def current_endpoint(self):
        result = command("awg", "show", "awg0", "endpoints")
        return next((value for key, value in (line.split() for line in result.stdout.splitlines())
                     if key == self.peer), "")

    def check_tunnel(self):
        return ping("awg0", self.tunnel_ip) or https_probe("awg0", self.host)

    def set_family(self, v6):
        wanted = self.alternate if v6 else self.primary
        previous = self.current_endpoint()
        if not wanted or not previous or previous == "(none)":
            return False
        if command("awg", "set", "awg0", "peer", self.peer, "endpoint", wanted).returncode != 0:
            return False
        for _ in range(3):
            if ping("awg0", self.tunnel_ip, "2") and endpoint_v6(self.current_endpoint()) == v6:
                self.v6 = v6
                self.v4_probes = self.fails = self.oks = 0
                self.v4_retry_delay = V4_PROBE_INTERVAL
                self.retry_v4 = time.monotonic() + V4_PROBE_INTERVAL
                log("AWG: выбран IPv6" if v6 else "AWG: восстановлен IPv4")
                return True
        if command("awg", "set", "awg0", "peer", self.peer, "endpoint", previous).returncode:
            log("AWG: не удалось восстановить предыдущий endpoint")
        return False

    def v4_reachable(self):
        ports = {self.config["exit_ssh_port"]}
        if self.config["backup"]:
            ports.add(self.config["reality_port"])
        peers = [(address, port) for address in addresses(self.host) for port in ports]
        if not peers:
            return False
        with ThreadPoolExecutor(max_workers=min(len(peers), 8)) as pool:
            return any(pool.map(lambda peer: tcp_probe(*peer), peers))

    def check_family(self):
        if not self.alternate:
            return
        now = time.monotonic()
        if self.v6:
            if self.fails >= FAIL_TO_SWITCH and self.v4_reachable() and self.set_family(False):
                return
            if now < self.retry_v4:
                return
            self.v4_probes = self.v4_probes + 1 if self.v4_reachable() else 0
            self.retry_v4 = now + V4_PROBE_INTERVAL
            if self.v4_probes >= V4_PROBES_TO_RETURN and not self.set_family(False):
                self.v4_probes = 0
                self.v4_retry_delay = min(self.v4_retry_delay * 2, 3600)
                self.retry_v4 = now + self.v4_retry_delay
        elif self.fails and not self.v4_reachable():
            address = self.alternate.removeprefix("[").split("]")[0]
            if ping("", address, "2"):
                self.set_family(True)

    def route(self):
        result = command("ip", "-j", "-4", "route", "show", "default", "table", "200")
        if result.returncode:
            return ""
        return next((r["dev"] for r in json.loads(result.stdout) if "dev" in r), "")

    def select_route(self, interface):
        result = command("ip", "-4", "route", "replace", "default", "dev", interface, "table", "200")
        if result.returncode:
            log(f"Таблица 200: не удалось выбрать {interface}: {result.stderr.strip()}")
            return False
        log(f"Таблица 200: выбран {interface}")
        return True

    def backup_ready(self):
        return (command("systemctl", "is-active", "-q", "vpnkit-cascade-tun.service").returncode == 0
                and https_probe("sbtun", self.host))

    def check_route(self):
        current = self.route()
        if current == "sbtun":
            self.backup_fails = 0 if self.config["backup"] and self.backup_ready() else self.backup_fails + 1
            if self.backup_fails >= BACKUP_FAIL_TO_RETURN or (
                    self.oks >= OK_TO_RETURN and https_probe("awg0", self.host)):
                if self.select_route("awg0"):
                    self.backup_fails = self.oks = self.fails = 0
        elif self.config["backup"] and self.fails >= FAIL_TO_SWITCH and self.backup_ready():
            if self.select_route("sbtun"):
                self.backup_fails = self.oks = self.fails = 0
        elif current != "awg0" and self.oks:
            self.select_route("awg0")

    def telegram_route(self, interface):
        for prefix in self.prefixes:
            result = command("ip", "-4", "route", "replace", prefix, "dev", interface, "scope", "link")
            if result.returncode:
                log(f"Telegram: маршрут {prefix} через {interface} не установлен")
                return False
        return True

    def telegram_check(self, interface):
        def request(address):
            result = curl(interface, "--output", "/dev/null", "--write-out", "%{time_connect}",
                          "--max-time", "3", "https://" + address + "/")
            return result.stdout.strip() not in ("", "0.000000")
        if request(TG_PROBES[0]):
            return True
        with ThreadPoolExecutor(max_workers=2) as pool:
            return any(pool.map(request, TG_PROBES[1:]))

    def check_telegram(self, current):
        if not self.prefixes or not current:
            return
        warp_exists = command("ip", "link", "show", "dev", "awgw").returncode == 0
        if not warp_exists:
            self.tg_warp = False
            self.tg_fails = self.tg_oks = self.tg_warp_fails = 0
        else:
            cascade_ok = self.telegram_check(current)
            if self.tg_warp:
                self.tg_warp_fails = 0 if self.telegram_check("awgw") else self.tg_warp_fails + 1
                self.tg_oks = self.tg_oks + 1 if cascade_ok else 0
                if self.tg_oks >= TG_OK_TO_RETURN or self.tg_warp_fails >= TG_WARP_FAIL_TO_RETURN:
                    self.tg_warp = False
                    self.tg_fails = self.tg_oks = self.tg_warp_fails = 0
            else:
                self.tg_fails = 0 if cascade_ok else self.tg_fails + 1
                if self.tg_fails >= TG_FAIL_TO_WARP and self.telegram_check("awgw"):
                    self.tg_warp = True
        selected = "awgw" if self.tg_warp else current
        # Проверяем текущие маршруты: awg-quick может восстановить их при перезапуске.
        routes = command("ip", "-j", "-4", "route", "show", "table", "main")
        on_interface = {r.get("dst") for r in json.loads(routes.stdout)
                        if r.get("dev") == selected} if routes.returncode == 0 else set()
        if not set(self.prefixes).issubset(on_interface):
            self.telegram_route(selected)

    def step(self):
        if self.check_tunnel():
            self.oks += 1
            self.fails = 0
        else:
            self.fails += 1
            self.oks = 0
        self.check_family()
        self.check_route()
        self.check_telegram(self.route())

    def restore(self):
        if command("ip", "link", "show", "dev", "awg0").returncode == 0:
            self.select_route("awg0")
            self.telegram_route("awg0")


def main():
    with open(sys.argv[1], encoding="utf-8") as f:
        watch = Watch(json.load(f))
    if len(sys.argv) > 2 and sys.argv[2] == "restore":
        watch.restore()
    elif len(sys.argv) > 2 and sys.argv[2] == "probe":
        sys.exit(0 if https_probe(sys.argv[3], watch.host) else 1)
    else:
        while True:
            watch.step()
            time.sleep(INTERVAL)


if __name__ == "__main__":
    main()
