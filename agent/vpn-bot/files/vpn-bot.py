#!/usr/bin/env python3
"""Telegram-бот администратора узла Крот: состояние, клиенты подписки, трафик.

Токен и получатели — те же, что у оповещений мониторинга (/etc/vpnkit/alert.env: TG_TOKEN, TG_CHATS).
Отвечает только тем, чьи chat id перечислены в TG_CHATS. Клиентами подписки управляет через
vpnkit-gen.py (реестр /etc/vpnkit/users.json, тот же, что у команд пульта `krot clients sub …`).
Профили AmneziaWG заводятся из пульта: ключи клиентов хранятся только у вас на компьютере.
"""
import datetime
import glob
import io
import json
import os
import re
import shutil
import sqlite3
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from collections import defaultdict
from contextlib import closing

from PIL import Image, ImageDraw, ImageFont

ENV = "/etc/vpnkit/alert.env"
GEN = "/usr/local/sbin/vpnkit-gen.py"
STATS_DB = "/var/lib/vpnkit/stats.db"
FONT_DIR = "/usr/share/fonts/truetype/dejavu"
NAME_RE = re.compile(r"^[A-Za-z0-9._-]{1,32}$")
UNITS = ["xray.service", "sing-box.service", "nginx.service", "dnsmasq.service", "warp-ru.service",
         "telemt.service", "mtproxyl-telemt.service",
         "traffic-guard.timer", "vpnkit-watch.timer", "vpnkit-backup.timer"]
PERIODS = {1: "Сутки", 7: "Неделя", 30: "Месяц"}
PROTOCOLS = {
    "awg": ("AmneziaWG", "#5699f5"),
    "xhttp": ("VLESS XHTTP", "#46c6a4"),
    "pq": ("VLESS PQ", "#f1ad49"),
    "hy2": ("Hysteria2", "#e66a9e"),
}
BACKGROUND, WHITE, MUTED, PANEL = "#16212b", "#f0f5fa", "#a2b4c6", "#22313f"
HEALTH = {
    "active": ("Работает", "#68d5b3"),
    "inactive": ("Остановлен", "#ff9caa"),
    "partial": ("Частично", "#f1ad49"),
    "missing": ("Не установлен", MUTED),
    "unknown": ("Нет данных", "#f1ad49"),
}


def load_env():
    cfg = {}
    for line in open(ENV):
        k, _, v = line.strip().partition("=")
        if k:
            cfg[k] = v.strip().strip('"')
    return cfg["TG_TOKEN"], {int(x) for x in cfg["TG_CHATS"].split()}


TOKEN, ADMINS = load_env()


def api(method, png=None, **params):
    fields = {k: json.dumps(v) if isinstance(v, (dict, list)) else v for k, v in params.items()}
    headers = {}
    if png is None:
        data = urllib.parse.urlencode(fields).encode()
    else:
        boundary = uuid.uuid4().hex
        parts = [f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'.encode()
                 for key, value in fields.items()]
        parts += [f'--{boundary}\r\nContent-Disposition: form-data; name="snapshot"; filename="dashboard.png"\r\n'
                  'Content-Type: image/png\r\n\r\n'.encode(), png, f'\r\n--{boundary}--\r\n'.encode()]
        data = b"".join(parts)
        headers["Content-Type"] = f"multipart/form-data; boundary={boundary}"
    req = urllib.request.Request(f"https://api.telegram.org/bot{TOKEN}/{method}", data=data, headers=headers)
    with urllib.request.urlopen(req, timeout=70) as r:
        return json.load(r)


def run(cmd, timeout=30):
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
        return p.returncode, (p.stdout + p.stderr).strip()
    except Exception as e:
        return 1, str(e)


def unit_states():
    _, out = run(["systemctl", "show", "--property=Id,LoadState,ActiveState", *UNITS])
    states = dict.fromkeys(UNITS, "unknown")
    for block in out.split("\n\n"):
        fields = dict(line.split("=", 1) for line in block.splitlines() if "=" in line)
        name, load, active = fields.get("Id"), fields.get("LoadState"), fields.get("ActiveState")
        if name not in states:
            continue
        if load == "not-found":
            states[name] = "missing"
        elif active == "active":
            states[name] = "active"
        elif active in {"inactive", "failed"}:
            states[name] = "inactive"
    return states


def status_snapshot():
    with open("/proc/loadavg") as file:
        load = file.read().split()[0]
    with open("/proc/meminfo") as file:
        mem = {line.split(":")[0]: int(line.split()[1]) for line in file}
    with open("/proc/uptime") as file:
        uptime = int(float(file.read().split()[0]) // 60)
    disk = shutil.disk_usage("/")
    used = (mem["MemTotal"] - mem["MemAvailable"]) // 1024
    metrics = [(f"{uptime // 1440} д {uptime // 60 % 24} ч" if uptime >= 1440
                else f"{uptime // 60} ч {uptime % 60} мин", "Время работы"),
               (load, "Нагрузка за 1 минуту"), (f"{used} / {mem['MemTotal'] // 1024} МБ", "Память"),
               (f"{disk.used * 100 // disk.total}%", "Диск")]
    services = unit_states()
    interfaces = {}
    now = int(time.time())
    for path in sorted(glob.glob("/etc/amnezia/amneziawg/*.conf")):
        name = os.path.basename(path)[:-5]
        client = name not in {"awg0", "awgw"}
        if not client:
            try:
                with open(path) as config:
                    client = config.readline().startswith("# vpnkit client-awg ")
            except OSError:
                pass
        rc, out = run(["awg", "show", name, "latest-handshakes"])
        times = [line.split("\t")[-1] for line in out.splitlines()]
        recent = sum(stamp.isdecimal() and 0 <= now - int(stamp) <= 180 for stamp in times) if rc == 0 else 0
        interfaces[name] = ("active" if rc == 0 else "inactive", recent, client)
    return metrics, services, interfaces


def protocol_states(services, interfaces):
    awg = {state for state, _, client in interfaces.values() if client}
    awg_state = "partial" if len(awg) > 1 else next(iter(awg), "missing")
    xray, nginx, sing_box = (services[name] for name in ("xray.service", "nginx.service", "sing-box.service"))

    def with_dependency(primary, dependency):
        if primary != "active" or dependency == "active":
            return primary
        return "unknown" if dependency == "unknown" else "inactive"

    proxy = [services[name] for name in ("mtproxyl-telemt.service", "telemt.service")]
    proxy_state = next((state for state in ("active", "inactive", "unknown") if state in proxy), "missing")
    return [
        (*PROTOCOLS["awg"], awg_state),
        (*PROTOCOLS["xhttp"], with_dependency(xray, nginx)),
        (*PROTOCOLS["pq"], xray),
        (*PROTOCOLS["hy2"], with_dependency(sing_box, xray)),
        ("Прокси Telegram", "#5ac5ed", proxy_state),
    ]


def status_text(metrics, services, interfaces):
    lines = [f"{label}: {value}" for value, label in metrics]
    lines += ["", "Службы"]
    lines += [f"{name.removesuffix('.service')}: {HEALTH[state][0]}"
              for name, state in services.items() if state != "missing"]
    if interfaces:
        lines += ["", "Интерфейсы AmneziaWG"]
        for name, (state, recent, _) in interfaces.items():
            handshakes = f" · пиров с рукопожатием за 3 мин: {recent}" if state == "active" else ""
            lines.append(f"{name}: {HEALTH[state][0]}{handshakes}")
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


def traffic(days, now=None):
    now = time.time() if now is None else now
    today = datetime.datetime.fromtimestamp(now).date()
    if days == 1:
        keys = list(range(int(now) // 3600 - 23, int(now) // 3600 + 1))
        labels = [datetime.datetime.fromtimestamp(key * 3600).strftime("%H:%M") for key in keys]
        cutoff = keys[0] * 3600
        bucket = "ts / 3600"
    else:
        dates = [today - datetime.timedelta(days=offset) for offset in range(days - 1, -1, -1)]
        keys = [day.isoformat() for day in dates]
        labels = [day.strftime("%d.%m") for day in dates]
        cutoff = int(datetime.datetime.combine(dates[0], datetime.time()).timestamp())
        bucket = "date(ts, 'unixepoch', 'localtime')"
    bins = {key: defaultdict(int) for key in keys}
    peers = defaultdict(lambda: [0, 0])
    if os.path.isfile(STATS_DB):
        with closing(sqlite3.connect(f"file:{STATS_DB}?mode=ro", uri=True)) as db:
            rows = db.execute(f"SELECT {bucket}, peer, SUM(rx), SUM(tx) FROM traffic "
                              "WHERE ts BETWEEN ? AND ? GROUP BY 1, peer", (cutoff, int(now)))
            for key, peer, rx, tx in rows:
                bins[key]["awg"] += rx + tx
                peers[peer][0] += rx
                peers[peer][1] += tx
            # После обновления бот может открыть старую базу до первого запуска сборщика.
            if db.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name='protocol_traffic'").fetchone():
                rows = db.execute(f"SELECT {bucket}, protocol, SUM(rx + tx) FROM protocol_traffic "
                                  "WHERE ts BETWEEN ? AND ? GROUP BY 1, protocol", (cutoff, int(now)))
                for key, protocol, total in rows:
                    if protocol in PROTOCOLS and protocol != "awg":
                        bins[key][protocol] += total
    return labels, list(bins.values()), peers


def stats_text(days):
    _, _, peers = traffic(days)
    lines = [f"AmneziaWG · {PERIODS[days]}"]
    if not peers:
        return lines[0] + "\nЗа этот период трафика нет."
    rows = sorted(peers.items(), key=lambda item: (-sum(item[1]), item[0]))
    for peer, (rx, tx) in rows[:25]:
        lines.append(f"{peer[:48]}: ↓ {fmt_bytes(tx)}  ↑ {fmt_bytes(rx)}")
    if len(rows) > 25:
        lines.append(f"Показано 25 из {len(rows)}")
    lines.append(f"Всего: {fmt_bytes(sum(sum(values) for values in peers.values()))}")
    return "\n".join(lines)


def dashboard_canvas(height):
    image = Image.new("RGB", (760, height), BACKGROUND)
    draw = ImageDraw.Draw(image)

    def text(x, y, value, size, color=WHITE, bold=False, anchor="lt"):
        suffix = "-Bold" if bold else ""
        font = ImageFont.truetype(f"{FONT_DIR}/DejaVuSans{suffix}.ttf", size)
        draw.text((x, y), value, fill=color, font=font, anchor=anchor)

    def box(x, y, width, height, color=PANEL, radius=18):
        draw.rounded_rectangle((x, y, x + width, y + height), radius, fill=color)

    return image, draw, text, box


def as_png(image):
    buffer = io.BytesIO()
    image.save(buffer, format="PNG")
    return buffer.getvalue()


def status_card(metrics, protocols):
    image, draw, text, box = dashboard_canvas(600)
    text(28, 28, "Крот · Состояние", 36, bold=True)
    for index, (value, label) in enumerate(metrics):
        x, y = 28 + (index % 2) * 359, 86 + (index // 2) * 103
        box(x, y, 345, 90)
        text(x + 20, y + 14, value, 32, bold=True)
        text(x + 20, y + 56, label, 20, MUTED)
    text(28, 312, "ПРОТОКОЛЫ И ПРОКСИ", 21, MUTED, True)
    box(28, 350, 704, 222)
    for index, (label, color, state) in enumerate(protocols):
        y = 350 + index * 44
        if index:
            draw.line((47, y, 713, y), fill="#344451")
        box(48, y + 16, 12, 12, color, 4)
        text(73, y + 22, label, 22, anchor="lm")
        message, state_color = HEALTH[state]
        text(711, y + 22, message, 20, state_color, anchor="rm")
    return as_png(image)


def traffic_card(days, labels, bins, active, registered):
    image, draw, text, box = dashboard_canvas(654)
    selected = "#356a9e"
    totals = {protocol: sum(values.get(protocol, 0) for values in bins) for protocol in PROTOCOLS}
    text(28, 28, "Крот · Трафик", 36, bold=True)
    for index, (period, label) in enumerate(PERIODS.items()):
        x = 28 + 238 * index
        box(x, 86, 228, 48, selected if days == period else PANEL, 13)
        text(x + 114, 110, label, 23, anchor="mm")
    box(28, 154, 345, 103)
    box(387, 154, 345, 103)
    text(48, 174, fmt_bytes(sum(totals.values())), 37, bold=True)
    text(48, 223, "Трафик за период", 20, MUTED)
    text(407, 174, f"{active} из {registered}", 37, bold=True)
    text(407, 223, "Профили AWG с трафиком", 20, MUTED)
    text(28, 287, "ТРАФИК ПО ЧАСАМ" if days == 1 else "ТРАФИК ПО ДНЯМ", 21, MUTED, True)
    box(28, 325, 704, 225)
    left, right, baseline = 52, 708, 508
    step = (right - left) / len(bins)
    peak = max((sum(values.values()) for values in bins), default=0)
    if peak:
        for y in (371, 438, baseline):
            draw.line((left, y, right, y), fill="#344451", width=1)
        for index, values in enumerate(bins):
            total = sum(values.values())
            if not total:
                continue
            width = min(58, step * 0.74)
            x = left + (index + 0.5) * step - width / 2
            scale = max(2, round(total * 153 / peak)) / total
            cumulative, bottom = 0, 0
            for protocol, (_, color) in PROTOCOLS.items():
                cumulative += values.get(protocol, 0)
                top = round(cumulative * scale)
                if top > bottom:
                    box(x, baseline - top, width, top - bottom, color, min(5, (top - bottom) // 2))
                bottom = top
    else:
        text(380, 421, "За этот период трафика нет", 24, MUTED, anchor="mm")
    for i in sorted({round(i * (len(labels) - 1) / 4) for i in range(5)}):
        x = max(58, min(702, left + (i + 0.5) * step))
        text(x, 524, labels[i], 16, MUTED, anchor="mt")
    for index, (protocol, (label, color)) in enumerate(PROTOCOLS.items()):
        x, y = 28 + (index % 2) * 359, 575 + (index // 2) * 37
        box(x, y, 15, 15, color, 4)
        text(x + 27, y - 4, f"{label}  {fmt_bytes(totals[protocol])}", 21)
    return as_png(image)


def card_text(name):
    rc, out = run([GEN, "show", name])
    if rc:
        return out, None
    c = json.loads(out)
    lines = [f"Клиент {name}"]
    if c.get("compat_url"):
        lines += ["", "Единая подписка INCY с AmneziaWG:", c["sub_url"],
                  "", "Подписка без AWG:", c["compat_url"]]
    elif c.get("sub_url"):
        lines += ["", "Подписка (Happ, v2rayNG, Hiddify):", c["sub_url"]]
    for k, v in (c.get("routing") or {}).items():
        lines.append(f"Маршруты {k}: {v}")
    return "\n".join(lines), c


def button(text, callback, style=None):
    return {"text": text, "callback_data": callback, **({"style": style} if style else {})}


def menu(section=None, days=7):
    nav = [button("👥 Клиенты", "clients", "primary" if section == "clients" else None),
           button("📊 Трафик", "st:7", "primary" if section == "traffic" else None),
           button("⚙ Состояние", "status", "primary" if section == "status" else None)]
    rows = []
    if section == "traffic":
        rows = [[button(label, f"st:{period}", "primary" if days == period else None)
                 for period, label in PERIODS.items()],
                [button("↻ Обновить", f"st:{days}"), button("Профили AWG", f"sd:{days}")]]
    elif section == "status":
        rows = [[button("↻ Обновить", "status"), button("Подробности", "status:details")]]
    return {"inline_keyboard": rows + [nav]}


def clients_view():
    names, peers = sub_names(), awg_peers()
    rows = [[{"text": n, "callback_data": f"c:{n}"}] for n in names]
    rows.append([button("➕ Новый клиент подписки", "add", "success")])
    text = f"Клиенты подписки: {len(names)}\nПрофили AmneziaWG: {len(peers)} (заводятся из пульта: krot clients add)"
    if peers:
        text += "\n" + ", ".join(sorted(peers))
    return text, {"inline_keyboard": rows + menu("clients")["inline_keyboard"]}


def send(chat, text, kb=None):
    p = {"chat_id": chat, "text": text, "disable_web_page_preview": True}
    if kb:
        p["reply_markup"] = kb
    api("sendMessage", **p)


def send_dashboard(chat, png, caption, keyboard, message):
    if message and message.get("photo"):
        try:
            api("editMessageMedia", png=png, chat_id=chat, message_id=message["message_id"],
                media={"type": "photo", "media": "attach://snapshot", "caption": caption}, reply_markup=keyboard)
            return
        except urllib.error.HTTPError as error:
            if error.code != 400:
                raise
            if b"message is not modified" in error.read():
                return
    api("sendPhoto", png=png, chat_id=chat, photo="attach://snapshot", caption=caption, reply_markup=keyboard)


def show_traffic(chat, days, message=None):
    try:
        labels, bins, peers = traffic(days)
        registered = awg_peers().keys()
        active = len(registered & peers.keys())
        png = traffic_card(days, labels, bins, active, len(registered))
    except (sqlite3.Error, OSError):
        send(chat, "Не удалось загрузить статистику трафика.", menu("traffic", days))
        return
    totals = {protocol: sum(values.get(protocol, 0) for values in bins) for protocol in PROTOCOLS}
    caption = (f"Трафик · {PERIODS[days]}\n"
               f"Всего: {fmt_bytes(sum(totals.values()))} · Профили AWG: {active} из {len(registered)}\n"
               + "\n".join(f"{label}: {fmt_bytes(totals[key])}" for key, (label, _) in PROTOCOLS.items()))
    send_dashboard(chat, png, caption, menu("traffic", days), message)


def show_status(chat, message=None, details=False):
    keyboard = menu("status")
    try:
        metrics, services, interfaces = status_snapshot()
    except (OSError, ValueError, KeyError):
        send(chat, "Не удалось прочитать состояние узла.", keyboard)
        return
    if details:
        send(chat, status_text(metrics, services, interfaces), keyboard)
        return
    protocols = protocol_states(services, interfaces)
    try:
        png = status_card(metrics, protocols)
    except OSError:
        send(chat, status_text(metrics, services, interfaces), keyboard)
        return
    caption = "\n".join(["Состояние служб на узле", *(f"{label}: {value}" for value, label in metrics),
                         *(f"{label}: {HEALTH[state][0]}" for label, _, state in protocols)])
    send_dashboard(chat, png, caption, keyboard, message)


def on_callback(cb):
    chat = cb["message"]["chat"]["id"]
    data = cb["data"]
    api("answerCallbackQuery", callback_query_id=cb["id"])
    if data == "main":
        send(chat, "Крот: что показать?", menu())
    elif data == "status":
        show_status(chat, cb["message"])
    elif data == "status:details":
        show_status(chat, details=True)
    elif data == "clients":
        send(chat, *clients_view())
    elif data in {f"st:{period}" for period in PERIODS}:
        show_traffic(chat, int(data[3:]), cb["message"])
    elif data in {f"sd:{period}" for period in PERIODS}:
        days = int(data[3:])
        send(chat, stats_text(days), menu("traffic", days))
    elif data == "add":
        send(chat, "Имя нового клиента (латиница, цифры, . _ -):", {"force_reply": True})
    elif data.startswith("c:"):
        name = data[2:]
        text, _ = card_text(name)
        send(chat, text, {"inline_keyboard": [[button("🗑 Удалить", f"d:{name}", "danger"),
                                               button("← Клиенты", "clients")]]})
    elif data.startswith("d:"):
        name = data[2:]
        send(chat, f"Удалить клиента {name}? Его подписка перестанет работать.",
             {"inline_keyboard": [[button("Да, удалить", f"D:{name}", "danger"),
                                   button("Отмена", "clients")]]})
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
        show_status(chat)
    elif text.startswith("/clients"):
        send(chat, *clients_view())
    elif text.startswith("/stats"):
        show_traffic(chat, 7)
    else:
        send(chat, "Крот: что показать?", menu())


def main():
    try:
        api("setMyCommands", commands=[{"command": "menu", "description": "Меню"},
                                       {"command": "status", "description": "Состояние узла"},
                                       {"command": "clients", "description": "Клиенты"},
                                       {"command": "stats", "description": "График трафика"}])
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
