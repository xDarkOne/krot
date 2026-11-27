#!/usr/bin/python3
"""Krot's Telegram Mini App and private subscription request observer."""

import calendar
import datetime as dt
import hashlib
import hmac
import json
import os
import re
import sqlite3
import subprocess
import sys
import time
import urllib.parse
import urllib.request
from contextlib import closing, contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from zoneinfo import ZoneInfo

ROOT = Path("/var/lib/vpnkit")
DB = ROOT / "panel.db"
USERS = Path("/etc/vpnkit/users.json")
ALERT = Path("/etc/vpnkit/alert.env")
GEN = "/usr/local/sbin/vpnkit-gen.py"
REDIRECTS = Path("/var/www/vpnkit-sub/go")
NAME = re.compile(r"[a-zA-Z0-9.-][a-zA-Z0-9._-]{0,31}")
IMPORT = re.compile(r"/g/([a-f0-9]{32})/incy")
FEED = re.compile(r"/u/([a-f0-9]{32})(?:\.vpnuri)?")
DEVICE = re.compile(r"[a-f0-9]{32}")
MAX_AGE = 3600


def settings():
    values = {}
    for line in ALERT.read_text().splitlines():
        key, equal, value = line.partition("=")
        if equal and not key.startswith("#"):
            values[key.strip()] = value.strip().strip('"')
    return values["TG_TOKEN"], {int(i) for i in values["TG_CHATS"].split()}


def authenticate(raw, token, admins, now=None):
    """Validate signed Telegram initData and return the authorized user's ID."""
    if not raw or len(raw) > 8192:
        return None
    try:
        pairs = urllib.parse.parse_qsl(raw, keep_blank_values=True, strict_parsing=True, max_num_fields=24)
        fields = dict(pairs)
        if len(pairs) != len(fields):
            return None
        received = fields.pop("hash")
        check = "\n".join(f"{k}={v}" for k, v in sorted(fields.items()))
        key = hmac.digest(b"WebAppData", token.encode(), "sha256")
        expected = hmac.new(key, check.encode(), hashlib.sha256).hexdigest()
        if not hmac.compare_digest(expected, received):
            return None
        age = (time.time() if now is None else now) - int(fields["auth_date"])
        user = json.loads(fields["user"])["id"]
        if -30 <= age <= MAX_AGE and type(user) is int and user > 0 and user in admins:
            return user
    except (ValueError, KeyError, TypeError):
        pass
    return None


@contextmanager
def database():
    with closing(sqlite3.connect(DB, timeout=10)) as conn, conn:
        conn.execute("""CREATE TABLE IF NOT EXISTS device (
          profile TEXT, identifier TEXT, platform TEXT, last_seen INTEGER,
          PRIMARY KEY (profile, identifier))""")
        conn.execute("""CREATE TABLE IF NOT EXISTS payment (
          profile TEXT PRIMARY KEY, day INTEGER, admin INTEGER, sent_month TEXT)""")
        yield conn


def clients():
    return json.loads(USERS.read_text()).get("clients", {})


def command(action, name, expect_json=False):
    if action not in ("add", "add-local-awg", "show", "del", "tg-add", "tg-show", "tg-del") or not NAME.fullmatch(name):
        raise ValueError("Неверное имя или действие")
    result = subprocess.run([GEN, action, name], capture_output=True, text=True, timeout=150)
    if result.returncode:
        raise ValueError("Команда не выполнена. Проверьте имя профиля и состояние модулей.")
    return json.loads(result.stdout) if expect_json else {}


def profile_id(profile):
    return str(profile["uuid"])


def user_devices(conn, profile):
    return [{"id": identifier, "platform": platform, "last_seen": seen}
            for identifier, platform, seen in conn.execute(
                "SELECT identifier, platform, last_seen FROM device WHERE profile=? ORDER BY last_seen DESC",
                (profile,))]


def profile_card(name, profile):
    card = command("show", name, expect_json=True)
    card["payment_day"] = None
    card["has_mtproxy"] = Path("/etc/vpnkit/mtproxy.env").is_file()
    if card["has_mtproxy"]:
        try:
            card["telegram_proxy"] = command("tg-show", name, expect_json=True)
        except ValueError:
            pass
    with database() as conn:
        row = conn.execute("SELECT day FROM payment WHERE profile=?", (profile,)).fetchone()
        card.update(devices=user_devices(conn, profile), payment_day=row[0] if row else None)
    stats = ROOT / "stats.db"
    if stats.is_file():
        try:
            with closing(sqlite3.connect(stats.as_uri() + "?mode=ro", uri=True, timeout=2)) as conn:
                since = int(time.time()) - 86400
                row = conn.execute("""SELECT COALESCE(SUM(rx + tx), 0) FROM profile_traffic
                                      WHERE name=? AND ts>=?""", (name, since)).fetchone()
                awg = conn.execute("""SELECT COALESCE(SUM(rx + tx), 0) FROM traffic
                                      WHERE peer=? AND ts>=?""", (name, since)).fetchone()
                card["traffic_24h"] = row[0] + awg[0]
        except sqlite3.Error:
            pass
    return card


def observed_profile(path, registry):
    match = FEED.fullmatch(urllib.parse.urlsplit(path).path)
    if not match:
        return None
    value = match[1]
    for name, client in registry.items():
        awg = client.get("amnezia") or {}
        if value in {client.get("token"), awg.get("token"), awg.get("feed_token")}:
            return name
    return None


def observe(path, hardware, agent):
    if not hardware or len(hardware) > 256:
        return
    registry = clients()
    name = observed_profile(path, registry)
    if not name:
        return
    profile = profile_id(registry[name])
    token, _ = settings()
    identifier = hmac.new(token.encode(), (profile + "\0" + hardware).encode(), hashlib.sha256).hexdigest()[:32]
    platform = next((name for name in ("Android", "iPhone", "iPad", "Windows", "Mac", "Linux")
                     if name.lower() in agent.lower()), "Не определено")
    with database() as conn:
        conn.execute("""INSERT INTO device VALUES (?, ?, ?, ?)
                     ON CONFLICT(profile, identifier) DO UPDATE SET
                     platform=excluded.platform,last_seen=excluded.last_seen""",
                     (profile, identifier, platform, int(time.time())))
        conn.execute("""DELETE FROM device WHERE profile=? AND identifier NOT IN
                     (SELECT identifier FROM device WHERE profile=? ORDER BY last_seen DESC LIMIT 50)""", (profile, profile))


def reminders():
    token, admins = settings()
    now = dt.datetime.now(ZoneInfo(os.environ.get("KROT_TIMEZONE", "Europe/Moscow")))
    if now.hour < 10:
        return
    month = now.strftime("%Y-%m")
    last_day = calendar.monthrange(now.year, now.month)[1]
    names = {profile_id(profile): name for name, profile in clients().items()}
    with database() as conn:
        rows = list(conn.execute("SELECT profile,day,admin FROM payment WHERE sent_month IS NOT ?", (month,)))
        for profile, day, admin in rows:
            if now.day < min(day, last_day) or admin not in admins or profile not in names:
                continue
            payload = urllib.parse.urlencode({"chat_id": admin, "text": f"Крот: день оплаты профиля {names[profile]}."}).encode()
            try:
                with urllib.request.urlopen(f"https://api.telegram.org/bot{token}/sendMessage",
                                            data=payload, timeout=15) as reply:
                    if not json.load(reply).get("ok"):
                        continue
            except (OSError, ValueError):
                print("Telegram: напоминание не отправлено", file=sys.stderr)
                continue
            with conn:
                conn.execute("UPDATE payment SET sent_month=? WHERE profile=? AND day=? AND admin=?",
                             (month, profile, day, admin))


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass  # Neither subscription tokens nor Telegram initData belong in logs.

    def setup(self):
        self.request.settimeout(10)
        super().setup()

    def reply(self, status, value=None):
        body = json.dumps(value, ensure_ascii=False).encode() if value is not None else b""
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def do_GET(self):
        match = IMPORT.fullmatch(self.path)
        if match:
            try:
                target = (REDIRECTS / match[1]).read_text().strip()
            except FileNotFoundError:
                return self.reply(404)
            if not re.fullmatch(r"incy://import/[A-Za-z0-9_-]+", target):
                return self.reply(404)
            self.send_response(302)
            self.send_header("Location", target)
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        if self.path == "/health":
            return self.reply(200, {"ok": True})
        self.reply(404)

    def do_POST(self):
        try:
            if self.path == "/observe":
                observe(self.headers.get("X-Krot-Path", ""), self.headers.get("X-Krot-Device", ""),
                        self.headers.get("User-Agent", ""))
                return self.reply(204)
            if self.path != "/panel/api/":
                return self.reply(404)
            token, admins = settings()
            admin = authenticate(self.headers.get("X-Telegram-Init-Data", ""), token, admins)
            if not admin:
                return self.reply(403, {"error": "Откройте панель из личного чата с ботом."})
            size = int(self.headers.get("Content-Length", "0"))
            if not 0 < size <= 4096:
                return self.reply(400, {"error": "Неверный размер запроса."})
            request = json.loads(self.rfile.read(size))
            if not isinstance(request, dict):
                return self.reply(400, {"error": "Ожидался объект JSON."})
            return self.reply(200, self.action(request, admin))
        except (ValueError, TypeError):
            self.reply(400, {"error": "Запрос не выполнен. Проверьте значения и состояние модулей."})
        except (OSError, KeyError, subprocess.SubprocessError, sqlite3.Error):
            self.reply(503, {"error": "Панель временно недоступна."})

    def action(self, request, admin):
        action = request.get("action")
        if action == "list":
            return {"names": sorted(clients()), "can_issue_awg": Path("/etc/vpnkit/client-awg.env").is_file()}
        name = request.get("name", "")
        if not isinstance(name, str) or not NAME.fullmatch(name):
            raise ValueError("profile")
        if action == "create":
            combined = request.get("combined", False)
            if type(combined) is not bool:
                raise ValueError("combined")
            command("add-local-awg" if combined else "add", name)
            return profile_card(name, profile_id(clients()[name]))
        profile = profile_id(clients()[name])
        if action == "profile":
            return profile_card(name, profile)
        if action in ("tg-add", "tg-del"):
            command(action, name)
            return profile_card(name, profile)
        if action == "delete":
            command("del", name)
            with database() as conn:
                conn.execute("DELETE FROM device WHERE profile=?", (profile,))
                conn.execute("DELETE FROM payment WHERE profile=?", (profile,))
            return {}
        with database() as conn:
            if action == "payment":
                day = request.get("day")
                if day is None:
                    conn.execute("DELETE FROM payment WHERE profile=?", (profile,))
                elif type(day) is int and 1 <= day <= 31:
                    conn.execute("""INSERT INTO payment VALUES (?, ?, ?, NULL)
                  ON CONFLICT(profile) DO UPDATE SET day=excluded.day,admin=excluded.admin""", (profile, day, admin))
                else:
                    raise ValueError("day")
            elif action == "reset_devices":
                conn.execute("DELETE FROM device WHERE profile=?", (profile,))
            elif action == "remove_device":
                device = request.get("id", "")
                if not isinstance(device, str) or not DEVICE.fullmatch(device):
                    raise ValueError("device")
                conn.execute("DELETE FROM device WHERE profile=? AND identifier=?", (profile, device))
            else:
                raise ValueError("action")
        return profile_card(name, profile)


if __name__ == "__main__":
    os.umask(0o077)
    if sys.argv[1:] == ["reminders"]:
        reminders()
    else:
        ThreadingHTTPServer(("127.0.0.1", 19194), Handler).serve_forever()
