#!/usr/bin/env python3
"""Telegram-бот администратора узла Крот: состояние, клиенты подписки, трафик.

Токен и получатели — те же, что у оповещений мониторинга (/etc/vpnkit/alert.env: TG_TOKEN, TG_CHATS).
Отвечает только тем, чьи chat id перечислены в TG_CHATS. Клиентами подписки управляет через
vpnkit-gen.py (реестр /etc/vpnkit/users.json, тот же, что у команд пульта `krot clients sub …`).
Профили AmneziaWG заводятся из пульта: ключи клиентов хранятся только у вас на компьютере.
"""
import glob
import json
import os
import re
import shutil
import sqlite3
import subprocess
import time
import urllib.parse
import urllib.request

ENV = "/etc/vpnkit/alert.env"
GEN = "/usr/local/sbin/vpnkit-gen.py"
STATS_DB = "/var/lib/vpnkit/stats.db"
NAME_RE = re.compile(r"^[A-Za-z0-9._-]{1,32}$")
UNITS = ["xray", "hysteria2", "sing-box", "nginx", "dnsmasq", "warp-ru", "telemt", "mtproxyl",
         "traffic-guard.timer", "vpnkit-watch.timer", "vpnkit-backup.timer"]


def load_env():
    cfg = {}
    for line in open(ENV):
        k, _, v = line.strip().partition("=")
        if k:
            cfg[k] = v.strip().strip('"')
    return cfg["TG_TOKEN"], {int(x) for x in cfg["TG_CHATS"].split()}


TOKEN, ADMINS = load_env()


def api(method, **params):
    data = urllib.parse.urlencode({k: json.dumps(v) if isinstance(v, (dict, list)) else v
                                   for k, v in params.items()}).encode()
    req = urllib.request.Request(f"https://api.telegram.org/bot{TOKEN}/{method}", data=data)
    with urllib.request.urlopen(req, timeout=70) as r:
        return json.load(r)


def run(cmd, timeout=30):
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
        return p.returncode, (p.stdout + p.stderr).strip()
    except Exception as e:
        return 1, str(e)


def unit_ok(u):
    if run(["systemctl", "is-enabled", u])[0]:
        return None  # не установлен
    return run(["systemctl", "is-active", u])[0] == 0


def status_text():
    la = open("/proc/loadavg").read().split()[0]
    mem = {l.split(":")[0]: int(l.split()[1]) for l in open("/proc/meminfo")}
    used = (mem["MemTotal"] - mem["MemAvailable"]) // 1024
    du = shutil.disk_usage("/")
    up = int(float(open("/proc/uptime").read().split()[0]) // 3600)
    lines = [f"Нагрузка {la}, память {used}/{mem['MemTotal'] // 1024} МБ, диск {du.used * 100 // du.total}%, "
             f"работает {up // 24} д {up % 24} ч", ""]
    for u in UNITS:
        s = unit_ok(u)
        if s is not None:
            lines.append(("✅ " if s else "❌ ") + u)
    for c in sorted(glob.glob("/etc/amnezia/amneziawg/*.conf")):
        i = os.path.basename(c)[:-5]
        rc, out = run(["awg", "show", i, "latest-handshakes"])
        n = len([l for l in out.splitlines() if l.split("\t")[-1:] != ["0"]]) if rc == 0 else 0
        lines.append(f"{'✅' if rc == 0 else '❌'} {i}: активных пиров {n}")
    return "\n".join(lines)


def sub_names():
    rc, out = run([GEN, "list"])
    return out.split() if rc == 0 else []


def awg_peers():
    res = {}
    for c in sorted(glob.glob("/etc/amnezia/amneziawg/*.conf")):
        name = None
        for line in open(c):
            s = line.strip()
            if s.startswith("# name:"):
                name = s.split(":", 1)[1].strip()
            elif s.startswith("PublicKey") and name:
                res.setdefault(name, []).append(os.path.basename(c)[:-5])
                name = None
    return res


def fmt_bytes(n):
    for u in ("Б", "КБ", "МБ", "ГБ", "ТБ"):
        if n < 1024 or u == "ТБ":
            return f"{n:.0f} {u}" if u == "Б" else f"{n:.1f} {u}"
        n /= 1024


def stats_text(days):
    if not os.path.exists(STATS_DB):
        return "Статистики пока нет: первый сбор через несколько минут после установки."
    db = sqlite3.connect(f"file:{STATS_DB}?mode=ro", uri=True)
    rows = db.execute("SELECT peer, SUM(rx), SUM(tx) FROM traffic WHERE ts >= ? GROUP BY peer "
                      "ORDER BY SUM(rx)+SUM(tx) DESC", (int(time.time()) - days * 86400,)).fetchall()
    if not rows:
        return "За этот период трафика нет."
    # rx/tx в дампе awg — с точки зрения сервера: rx — от клиента, tx — клиенту
    lines = [f"Трафик за {days} дн.:"]
    for peer, rx, tx in rows:
        lines.append(f"{peer}: ↓ {fmt_bytes(tx)}  ↑ {fmt_bytes(rx)}")
    lines.append(f"Всего: {fmt_bytes(sum(r[1] + r[2] for r in rows))}")
    return "\n".join(lines)


def card_text(name):
    rc, out = run([GEN, "show", name])
    if rc:
        return out, None
    c = json.loads(out)
    lines = [f"Клиент {name}"]
    if c.get("sub_url"):
        lines += ["", "Подписка (Happ, v2rayNG, Hiddify):", c["sub_url"]]
    for k, v in (c.get("routing") or {}).items():
        lines.append(f"Маршруты {k}: {v}")
    return "\n".join(lines), c


def menu():
    return {"inline_keyboard": [
        [{"text": "Состояние", "callback_data": "status"}, {"text": "Клиенты", "callback_data": "clients"}],
        [{"text": "Трафик: сутки", "callback_data": "st:1"}, {"text": "неделя", "callback_data": "st:7"},
         {"text": "месяц", "callback_data": "st:30"}]]}


def clients_view():
    names, peers = sub_names(), awg_peers()
    rows = [[{"text": n, "callback_data": f"c:{n}"}] for n in names]
    rows.append([{"text": "➕ Новый клиент подписки", "callback_data": "add"}])
    rows.append([{"text": "← Меню", "callback_data": "main"}])
    text = f"Клиенты подписки: {len(names)}\nПрофили AmneziaWG: {len(peers)} (заводятся из пульта: krot clients add)"
    if peers:
        text += "\n" + ", ".join(sorted(peers))
    return text, {"inline_keyboard": rows}


def send(chat, text, kb=None):
    p = {"chat_id": chat, "text": text, "disable_web_page_preview": True}
    if kb:
        p["reply_markup"] = kb
    api("sendMessage", **p)


def on_callback(cb):
    chat = cb["message"]["chat"]["id"]
    data = cb["data"]
    api("answerCallbackQuery", callback_query_id=cb["id"])
    if data == "main":
        send(chat, "Крот: что показать?", menu())
    elif data == "status":
        send(chat, status_text(), menu())
    elif data == "clients":
        send(chat, *clients_view())
    elif data.startswith("st:"):
        send(chat, stats_text(int(data[3:])), menu())
    elif data == "add":
        send(chat, "Имя нового клиента (латиница, цифры, . _ -):", {"force_reply": True})
    elif data.startswith("c:"):
        name = data[2:]
        text, _ = card_text(name)
        send(chat, text, {"inline_keyboard": [[{"text": "🗑 Удалить", "callback_data": f"d:{name}"},
                                               {"text": "← Клиенты", "callback_data": "clients"}]]})
    elif data.startswith("d:"):
        name = data[2:]
        send(chat, f"Удалить клиента {name}? Его подписка перестанет работать.",
             {"inline_keyboard": [[{"text": "Да, удалить", "callback_data": f"D:{name}"},
                                   {"text": "Отмена", "callback_data": "clients"}]]})
    elif data.startswith("D:"):
        rc, out = run([GEN, "del", data[2:]], timeout=120)
        send(chat, out, menu())


def on_message(m):
    chat = m["chat"]["id"]
    text = (m.get("text") or "").strip()
    reply = (m.get("reply_to_message") or {}).get("text", "")
    if reply.startswith("Имя нового клиента"):
        if not NAME_RE.match(text):
            send(chat, "Имя: латиница, цифры, . _ -, до 32 знаков.", menu())
            return
        rc, out = run([GEN, "add", text], timeout=120)
        if rc:
            send(chat, out, menu())
        else:
            card, _ = card_text(text)
            send(chat, "Готово.\n\n" + card, menu())
    elif text.startswith("/status"):
        send(chat, status_text(), menu())
    elif text.startswith("/clients"):
        send(chat, *clients_view())
    elif text.startswith("/stats"):
        send(chat, stats_text(7), menu())
    else:
        send(chat, "Крот: что показать?", menu())


def main():
    try:
        api("setMyCommands", commands=[{"command": "menu", "description": "Меню"},
                                       {"command": "status", "description": "Состояние узла"},
                                       {"command": "clients", "description": "Клиенты"},
                                       {"command": "stats", "description": "Трафик за неделю"}])
    except Exception:
        pass
    offset = 0
    while True:
        try:
            r = api("getUpdates", offset=offset, timeout=50, allowed_updates=["message", "callback_query"])
        except Exception:
            time.sleep(10)
            continue
        for u in r.get("result", []):
            offset = u["update_id"] + 1
            try:
                cb, m = u.get("callback_query"), u.get("message")
                if cb and cb["from"]["id"] in ADMINS:
                    on_callback(cb)
                elif m and m["chat"]["id"] in ADMINS:
                    on_message(m)
            except Exception as e:
                print("ошибка обработки:", e, flush=True)


if __name__ == "__main__":
    main()
