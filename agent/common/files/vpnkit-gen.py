#!/usr/bin/env python3
"""vpnkit-gen — конфиги xray и sing-box (hysteria2) и подписки из одного реестра клиентов.

Реестр: /etc/vpnkit/users.json (schema 2):
  {"schema": 2, "vlessenc": {"decryption", "encryption"}, "clients": {имя: {uuid, token, hy2, created}}}
Настройки модулей (пишет пульт): /etc/vpnkit/{nginx,xray,hy2,sub,mtproxy}.env — есть файл, значит модуль стоит.
Ещё он единственный владелец /etc/nginx/stream.d/vpnkit-443.conf: куда идёт TCP/443, зависит от
того, стоит ли MTProxy, — пишут его не модули, а генератор по факту.

Команды:
  build            собрать всё; службы перезапускаются, только если их конфиг изменился
  add <имя>        новый клиент → JSON со ссылками
  del <имя>        удалить клиента (его подписка перестаёт отвечать)
  show <имя>       JSON со ссылками клиента
  list             имена клиентов
  tg-add|tg-del|tg-show <имя>, tg-list   то же для пользователей MTProxy (telemt)
MTProxy бывает двух видов (mtproxy.env, BACKEND): telemt, который ставит сам vpnkit, или
MTProxyL (BACKEND=mtproxyl, вид по умолчанию) — тогда пользователи живут в MTProxyL
(`mtproxyl secret …`), его конфиг telemt генератор не трогает, а tg-* лишь переводят команды.
Списки для маршрутизации берутся у split-routing (та же правда, что у клиентов AWG):
  сети — /var/lib/vpnkit/ru-nets.txt, зоны — server=/…/ из /etc/dnsmasq.d/10-vpnkit-split.conf.
"""
import base64
import datetime
import json
import os
import re
import secrets
import subprocess
import sys
import uuid
from urllib.parse import quote

ETC = "/etc/vpnkit"
USERS = ETC + "/users.json"
XRAY_BIN = "/usr/local/bin/xray"
XRAY_CONF = "/usr/local/etc/xray/config.json"
SB_CONF = "/etc/sing-box/config.json"
RU_NETS = "/var/lib/vpnkit/ru-nets.txt"
SPLIT_CONF = "/etc/dnsmasq.d/10-vpnkit-split.conf"
WARP_DIR = "/etc/warp-ru"
NAME_RE = re.compile(r"^[A-Za-z0-9._-]{1,32}$")
PRIVATE = ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "100.64.0.0/10"]

# Разбор имени из TLS/QUIC: доменные правила работают и для клиентов, что резолвят сами.
# googlevideo и gvt1 исключены: в ссылках на видео зашит адрес узла, свой резолв даёт 403 (P-18).
SNIFFING = {"enabled": True, "destOverride": ["tls", "quic"], "metadataOnly": False,
            "routeOnly": False, "domainsExcluded": ["googlevideo.com", "gvt1.com"]}


def env(name):
    path = f"{ETC}/{name}.env"
    if not os.path.exists(path):
        return None
    out = {}
    for line in open(path):
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, v = line.split("=", 1)
            out[k.strip()] = v.strip().strip('"')
    return out


def load_users():
    if not os.path.exists(USERS):   # первым модулем может оказаться и mtproxy, и xray
        os.makedirs(ETC, mode=0o700, exist_ok=True)
        save_users({"schema": 2, "clients": {}, "mtproto": {}})
    with open(USERS) as f:
        u = json.load(f)
    if u.get("schema") != 2:
        sys.exit(f"{USERS}: ожидалась schema 2")
    u.setdefault("clients", {})
    u.setdefault("mtproto", {})
    return u


def save_users(u):
    tmp = USERS + ".new"
    with open(os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w") as f:
        json.dump(u, f, ensure_ascii=False, indent=1)
    os.replace(tmp, USERS)


def write_if_changed(path, data, mode, test=None, group=None):
    """Записать атомарно, если содержимое другое; test(путь) проверяет до замены. → изменился ли."""
    if os.path.exists(path) and open(path).read() == data:
        return False
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp = path + ".new.json"   # xray определяет формат по расширению
    with open(os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w") as f:
        f.write(data)
    if group:
        import grp
        os.chown(tmp, 0, grp.getgrnam(group).gr_gid)
    os.chmod(tmp, mode)
    if test:
        r = subprocess.run(test(tmp), capture_output=True, text=True)
        if r.returncode != 0:
            os.unlink(tmp)
            sys.exit(f"{path}: новая версия отвергнута проверкой:\n{r.stdout}{r.stderr}")
    os.replace(tmp, path)
    return True


def ru_lists():
    nets, zones = [], []
    try:
        nets = [l.strip() for l in open(RU_NETS) if l.strip()]
    except OSError:
        pass
    try:
        for line in open(SPLIT_CONF):
            m = re.match(r"^server=/([^/]+)/", line)
            if m:
                zones.append(m.group(1))
    except OSError:
        pass
    return nets, sorted(set(zones))


def xray_config(u, x):
    cascade = x.get("CASCADE") == "1"
    clients = [{"id": c["uuid"], "email": n} for n, c in sorted(u["clients"].items())]
    nets, zones = ru_lists()
    if cascade and len(nets) < 5000:
        sys.exit(f"{RU_NETS}: {len(nets)} сетей — без списка российский трафик ушёл бы в каскад; нужен split-routing")
    rules = [
        {"protocol": ["bittorrent"], "outboundTag": "block"},
    ]
    if os.path.exists("/usr/local/share/xray/geosite.dat"):
        rules.append({"domain": ["geosite:category-ads-all"], "outboundTag": "block"})
    # свои DNS-запросы xray к dnsmasq на петле — до запрета частных сетей (P-16)
    rules.append({"ip": ["127.0.0.1/32"], "port": 53, "outboundTag": "direct"})
    # клиентам нечего делать во внутренних сетях узла: панели, socks, соседние службы (P-17)
    rules.append({"ip": PRIVATE, "outboundTag": "block"})
    # модуль warp-ru: российское выходит через Cloudflare WARP (outbound с меткой 4), а сайты из
    # bypass-domains, не открывающиеся с адресов Cloudflare, — напрямую
    warp = cascade and os.path.exists(WARP_DIR + "/account.json")
    ru_out = "ru-exit" if warp else "direct"
    if warp:
        try:
            bypass = [l.split("#")[0].strip() for l in open(WARP_DIR + "/bypass-domains")]
        except OSError:
            bypass = []
        bypass = [d for d in bypass if d]
        if bypass:
            rules.append({"domain": bypass, "outboundTag": "direct"})
    if cascade:
        if zones:
            rules.append({"domain": ["domain:" + z for z in zones], "outboundTag": ru_out})
        rules.append({"ip": nets, "outboundTag": ru_out})
    dnsmasq = subprocess.run(["systemctl", "is-active", "--quiet", "dnsmasq"]).returncode == 0
    out_cascade = {"tag": "cascade", "protocol": "freedom", "settings": {"domainStrategy": "UseIPv4"},
                   "streamSettings": {"sockopt": {"mark": 3}}}
    out_direct = {"tag": "direct", "protocol": "freedom", "settings": {"domainStrategy": "UseIPv4"}}
    out_ru = {"tag": "ru-exit", "protocol": "freedom", "settings": {"domainStrategy": "UseIPv4"},
              "streamSettings": {"sockopt": {"mark": 4}}}
    return {
        "log": {"loglevel": "warning"},
        # dnsmasq узла уже делит DNS (российские зоны → российский резолвер) — одна правда с AWG
        "dns": {"servers": ["127.0.0.1"] if dnsmasq else ["1.1.1.1", "8.8.8.8"], "queryStrategy": "UseIPv4"},
        "inbounds": [
            {"tag": "xh-in", "listen": "127.0.0.1", "port": int(x["XH_PORT"]), "protocol": "vless",
             "settings": {"clients": clients, "decryption": "none"},
             "streamSettings": {"network": "xhttp", "security": "none", "xhttpSettings": {"path": x["XH_PATH"]}},
             "sniffing": SNIFFING},
            {"tag": "enc-in", "listen": "0.0.0.0", "port": int(x["ENC_PORT"]), "protocol": "vless",
             "settings": {"clients": clients, "decryption": u["vlessenc"]["decryption"]},
             "streamSettings": {"network": "xhttp", "security": "none", "xhttpSettings": {"path": x["ENC_PATH"]}},
             "sniffing": SNIFFING},
            # петлевой SOCKS: сюда sing-box сдаёт hysteria2 — маршрутизация одна на всех
            {"tag": "hy2-in", "listen": "127.0.0.1", "port": int(x["SOCKS_PORT"]), "protocol": "socks",
             "settings": {"auth": "noauth", "udp": True}, "sniffing": SNIFFING},
        ],
        # первый outbound — по умолчанию: на entry всё не российское уходит в каскад
        "outbounds": ([out_cascade, out_direct] if cascade else [out_direct]) + ([out_ru] if warp else []) +
                     [{"tag": "block", "protocol": "blackhole"}],
        # AsIs: домены ловятся доменными правилами, адреса — адресными, без резолва на каждое соединение
        "routing": {"domainStrategy": "AsIs", "rules": rules},
    }


def singbox_config(u, x, h):
    name = h["TUNNEL_NAME"]
    live = f"/etc/letsencrypt/live/{name}"
    return {
        "log": {"level": "warn", "timestamp": True},
        "inbounds": [{
            "type": "hysteria2", "tag": "hy2-in", "listen": "::", "listen_port": int(h["HY2_PORT"]),
            "users": [{"name": n, "password": c["hy2"]} for n, c in sorted(u["clients"].items())],
            # непрошенный запрос получает настоящую витрину, а не отказ
            "masquerade": "https://127.0.0.1:8444",
            "tls": {"enabled": True, "server_name": name,
                    "certificate_path": live + "/fullchain.pem", "key_path": live + "/privkey.pem"},
        }],
        "outbounds": [{"type": "socks", "tag": "to-xray", "server": "127.0.0.1",
                       "server_port": int(x["SOCKS_PORT"]), "version": "5"}],
    }


def links(name, c, u, x, h):
    label = quote(f"{name} · {x.get('LABEL', '')}".strip(" ·"))
    host = x["TUNNEL_NAME"]
    out = []
    if h:
        out.append(f"hysteria2://{quote(c['hy2'], safe='')}@{host}:{h['HY2_PORT']}/?sni={host}#{label}")
    # fp=ios — ClientHello влезает в один сегмент мобильной сети (P-51)
    q = {"type": "xhttp", "security": "tls", "sni": host, "fp": "ios",
         "path": quote(x["XH_PATH"], safe=""), "encryption": "none"}
    out.append(f"vless://{c['uuid']}@{host}:{x['XHTTP_PORT']}?" + "&".join(f"{k}={v}" for k, v in q.items())
               + f"#{label}%20XHTTP")
    q = {"type": "xhttp", "security": "none", "path": quote(x["ENC_PATH"], safe=""),
         "encryption": quote(u["vlessenc"]["encryption"], safe="")}
    out.append(f"vless://{c['uuid']}@{host}:{x['ENC_PORT']}?" + "&".join(f"{k}={v}" for k, v in q.items())
               + f"#{label}%20PQ")
    return out


def subs(u, x, h, s):
    outdir = s["OUTDIR"]
    os.makedirs(outdir, exist_ok=True)
    # права — явно, а не через umask: генератор зовут и под umask 077 (восстановление),
    # и тогда nginx (www-data) не прочтёт подписку — 403 вместо 200
    os.chmod(outdir, 0o755)
    os.chmod(os.path.dirname(outdir), 0o755)
    want = set()
    for n, c in sorted(u["clients"].items()):
        body = base64.b64encode("\n".join(links(n, c, u, x, h)).encode()).decode()
        path = os.path.join(outdir, c["token"])
        if not os.path.exists(path) or open(path).read() != body:
            with open(os.open(path + ".new", os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o644), "w") as f:
                f.write(body)
            os.replace(path + ".new", path)
        os.chmod(path, 0o644)
        want.add(c["token"])
    # подписки удалённых клиентов не должны отвечать; в каталоге только наши файлы
    gone = 0
    for f in set(os.listdir(outdir)) - want:
        os.unlink(os.path.join(outdir, f))
        gone += 1
    return len(want), gone


STREAM_CONF = "/etc/nginx/stream.d/vpnkit-443.conf"
TELEMT_CONF = "/etc/telemt/telemt.toml"
TELEMT_STATIC = "/var/lib/vpnkit/telemt.static.sha256"


def stream_conf(m):
    """TCP/443 на entry. Без MTProxy — всё на витрину; с ним — всё в telemt (PROXY protocol
    несёт адрес клиента), а не-Telegram клиентов telemt сам отдаёт витрине (маска).
    У MTProxyL с WEB Proxy его домен уходит в TLS-сервер nginx (WEB_TLS_PORT), оттуда в WEB-вход."""
    web = ""
    if m:
        target, pp = f"127.0.0.1:{m['LISTEN_PORT']}", "    proxy_protocol on;\n"
        note = "всё — в telemt (MTProxy); чужих он отдаёт витрине 127.0.0.1:8444"
        if m.get("BACKEND") == "mtproxyl" and m.get("WEB_DOMAIN"):
            web = f"    {m['WEB_DOMAIN']} 127.0.0.1:{m.get('WEB_TLS_PORT', '15444')};\n"
            note += f"; {m['WEB_DOMAIN']} — WEB Proxy"
    else:
        target, pp, note = "127.0.0.1:8444", "", "всё — на витрину 127.0.0.1:8444"
    return (f"# vpnkit: TCP/443 по имени из ClientHello. Пишет vpnkit-gen.py — руками не править.\n"
            f"# Сейчас {note}.\n"
            f"map $ssl_preread_server_name $vpnkit_443 {{\n{web}    default {target};\n}}\n"
            f"server {{\n    listen 443;\n    ssl_preread on;\n    proxy_pass $vpnkit_443;\n{pp}"
            f"    proxy_connect_timeout 10s;\n}}\n")


def apply_stream(m):
    n = env("nginx")
    if not n or n.get("STREAM") != "1":
        return ""
    data = stream_conf(m)
    if os.path.exists(STREAM_CONF) and open(STREAM_CONF).read() == data:
        return ""
    old = open(STREAM_CONF).read() if os.path.exists(STREAM_CONF) else None
    with open(STREAM_CONF, "w") as f:
        f.write(data)
    os.chmod(STREAM_CONF, 0o644)
    r = subprocess.run(["nginx", "-t", "-q"], capture_output=True, text=True)
    if r.returncode != 0:
        if old is None:
            os.unlink(STREAM_CONF)
        else:
            with open(STREAM_CONF, "w") as f:
                f.write(old)
        sys.exit(f"nginx -t после смены 443 не прошёл, вернул как было:\n{r.stderr}")
    subprocess.run(["systemctl", "reload", "nginx"], check=True)
    return "443 → " + ("telemt" if m else "витрина")


def telemt_config(u, m):
    """Конфиг своего telemt (без WEB Proxy). Возвращает (статическая часть, пользователи)."""
    static = f'''# vpnkit mtproxy: конфиг telemt. Пишет vpnkit-gen.py — руками не править.
[general]
prefer_ipv6 = false
tg_connect = 30
fast_mode = true
use_middle_proxy = true
log_level = "normal"

[general.modes]
classic = false
secure = false
tls = true

[general.links]
public_host = "{m['PUBLIC_HOST']}"
public_port = 443

# наружу telemt не слушает: TCP/443 держит nginx stream и передаёт сюда с PROXY protocol
[server]
port = {m['LISTEN_PORT']}
listen_addr_ipv4 = "127.0.0.1"
listen_addr_ipv6 = "::1"
proxy_protocol = true
proxy_protocol_trusted_cidrs = ["127.0.0.1/32"]
metrics_listen = "127.0.0.1:{m['METRICS_PORT']}"
metrics_whitelist = ["127.0.0.1/32", "::1/128"]

[[server.listeners]]
ip = "127.0.0.1"
port = {m['LISTEN_PORT']}
transport = "mtproxy"
proxy_protocol = true

[server.api]
enabled = true
listen = "127.0.0.1:{m['API_PORT']}"
whitelist = ["127.0.0.1/32", "::1/128"]

[timeouts]
client_handshake = 90
client_keepalive = 120
client_ack = 90

# не-Telegram клиент (сканер, браузер) получает настоящую витрину с её сертификатом
[censorship]
mask_host = "127.0.0.1"
tls_domain = "{m['TLS_DOMAIN']}"
unknown_sni_action = "mask"
mask = true
mask_port = 8444
fake_cert_len = 912
mask_proxy_protocol = 0

[access]
replay_check_len = 65536
replay_window_secs = 1800
ignore_time_skew = false

[[upstreams]]
type = "direct"
weight = 10
'''
    # пользователи меняются на лету (hot-reload telemt) — без перезапуска и обрыва соединений
    users = "\n[access.users]\n" + "".join(f'"{n}" = "{s}"\n' for n, s in sorted(u["mtproto"].items()))
    return static, users


def mtproxyl(*args):
    """Команда MTProxyL. Он печатает с цветами — снимаем их."""
    r = subprocess.run(["/opt/mtproxyl/mtproxyl.sh", *args], capture_output=True, text=True,
                       stdin=subprocess.DEVNULL, env={**os.environ, "TERM": "dumb"})
    ansi = re.compile(r"\x1b\[[0-9;]*m")
    out = ansi.sub("", r.stdout)
    if r.returncode != 0:
        why = ansi.sub("", r.stderr or out).strip()[-300:]
        sys.exit("mtproxyl " + " ".join(args) + ": " + why)
    return out


def mtproxyl_users():
    return {x["label"]: x for x in json.loads(mtproxyl("secret", "list", "--json") or "[]")}


def mtproxyl_link(name):
    """tg://proxy и tg://webproxy пользователя → то же, что tg_link у своего telemt (+ web)."""
    out = {"name": name}
    for line in mtproxyl("secret", "link", name).splitlines():
        line = line.strip()
        if line.startswith("tg://proxy?") and "tg" not in out:
            out["tg"] = line
            out["https"] = "https://t.me/proxy?" + line.split("?", 1)[1]
        elif line.startswith("tg://webproxy?"):
            out["web"] = line
    if "tg" not in out and "web" not in out:
        sys.exit(f"mtproxyl не выдал ссылок для {name}")
    return out


def apply_telemt(u, m):
    import hashlib
    # без пользователей telemt подставил бы общеизвестный нулевой секрет — служебный держим всегда
    if "_probe" not in u["mtproto"]:
        u["mtproto"]["_probe"] = secrets.token_hex(16)
        save_users(u)
    static, users = telemt_config(u, m)
    h = hashlib.sha256(static.encode()).hexdigest()
    changed = write_if_changed(TELEMT_CONF, static + users, 0o640, group="telemt")
    old = open(TELEMT_STATIC).read().strip() if os.path.exists(TELEMT_STATIC) else ""
    note = ""
    if h != old:
        note = restart_if("telemt", True)
        with open(TELEMT_STATIC, "w") as f:
            f.write(h + "\n")
    elif changed:
        note = ", пользователи перечитаны на лету"
    n = sum(1 for k in u["mtproto"] if not k.startswith("_"))
    return f"telemt: {n} пользователей{note}"


def tg_link(name, u, m):
    domain_hex = m["TLS_DOMAIN"].encode().hex()
    secret = "ee" + u["mtproto"][name] + domain_hex
    q = f"server={m['PUBLIC_HOST']}&port=443&secret={secret}"
    return {"name": name, "tg": f"tg://proxy?{q}", "https": f"https://t.me/proxy?{q}"}


ROUTING_SRC = "https://raw.githubusercontent.com/hydraponique/roscomvpn-routing/main/{}/DEFAULT.JSON"
ROUTING_CACHE = "/var/lib/vpnkit/routing"


def routing(s):
    """Готовые правила маршрутизации для Happ (/h) и INCY (/i): клиентское приложение само
    пускает российское напрямую с адреса пользователя (Госуслуги видят абонента, а не дата-центр).
    Список ведёт сообщество (roscomvpn-routing); у нас — копия, чтобы ссылка не зависела от
    чужого домена. Источник опрашивается не чаще раза в 20 часов, иначе — кэш."""
    import base64 as b64
    import time
    import urllib.request
    os.makedirs(ROUTING_CACHE, exist_ok=True)
    out, notes = [], []
    for app, path in (("happ", "h"), ("incy", "i")):
        cache = f"{ROUTING_CACHE}/{app}.json"
        conf = None
        if not os.path.exists(cache) or time.time() - os.path.getmtime(cache) > 20 * 3600:
            try:
                req = urllib.request.Request(ROUTING_SRC.format(app.upper()), headers={"User-Agent": "vpnkit"})
                with urllib.request.urlopen(req, timeout=30) as r:
                    conf = json.loads(r.read().decode())
                if not conf.get("DirectSites") or not conf.get("BlockSites"):
                    raise ValueError("в ответе нет маршрутных списков")
                with open(cache + ".new", "w") as f:
                    json.dump(conf, f, ensure_ascii=False)
                os.replace(cache + ".new", cache)
            except Exception as e:
                conf = None
                notes.append(f"{app}: источник недоступен ({e})")
        if conf is None:
            if not os.path.exists(cache):
                notes.append(f"{app}: нет и кэша — ссылка /{path} не раздаётся")
                continue
            conf = json.load(open(cache))
        conf["Name"] = s.get("ROUTING_NAME") or "VPN"
        conf["LastUpdated"] = str(int(os.path.getmtime(cache)))
        body = json.dumps(conf, ensure_ascii=False, separators=(",", ":"))
        link = f"{app}://routing/onadd/" + b64.b64encode(body.encode()).decode()
        out.append(f'location = /{path} {{\n    access_log off;\n    add_header Cache-Control "no-store" always;\n'
                   f'    return 302 "{link}";\n}}\n')
    snippet = f"/etc/nginx/vpnkit.d/{s['SUB_NAME']}-site-routing.conf"
    data = "# vpnkit: правила маршрутизации для Happ (/h) и INCY (/i). Пишет vpnkit-gen.py.\n" + "".join(out)
    if os.path.exists(snippet) and open(snippet).read() == data:
        return "", notes
    old = open(snippet).read() if os.path.exists(snippet) else None
    with open(snippet, "w") as f:
        f.write(data)
    os.chmod(snippet, 0o644)
    r = subprocess.run(["nginx", "-t", "-q"], capture_output=True, text=True)
    if r.returncode != 0:
        if old is None:
            os.unlink(snippet)
        else:
            with open(snippet, "w") as f:
                f.write(old)
        sys.exit(f"nginx отверг правила маршрутизации, вернул как было:\n{r.stderr}")
    subprocess.run(["systemctl", "reload", "nginx"], check=False)
    return "правила Happ/INCY обновлены", notes


def restart_if(unit, changed):
    if not changed:
        return ""
    if subprocess.run(["systemctl", "is-enabled", "--quiet", unit]).returncode != 0:
        return ""
    r = subprocess.run(["systemctl", "restart", unit], capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit(f"{unit}: перезапуск не удался: {r.stderr.strip()}")
    return f", {unit} перезапущен"


def build(quiet=False):
    u = load_users()
    x, h, s, m = env("xray"), env("hy2"), env("sub"), env("mtproxy")
    msg = []
    st = apply_stream(m)
    if st:
        msg.append(st)
    if x:
        changed = False
        for c in u["clients"].values():   # недостающие секреты (клиент старой схемы, новый модуль)
            for k, gen in (("token", lambda: secrets.token_hex(16)), ("hy2", lambda: secrets.token_urlsafe(16))):
                if not c.get(k):
                    c[k] = gen()
                    changed = True
        if changed:
            save_users(u)
        if "vlessenc" not in u:
            sys.exit(f"{USERS}: нет ключа vlessenc — переустановите модуль xray")
        xc = write_if_changed(XRAY_CONF, json.dumps(xray_config(u, x), ensure_ascii=False, indent=1), 0o640,
                              test=lambda p: [XRAY_BIN, "run", "-test", "-config", p], group="nogroup")
        msg.append(f"xray: {len(u['clients'])} клиентов{restart_if('xray', xc)}")
        if h:
            sc = write_if_changed(SB_CONF, json.dumps(singbox_config(u, x, h), ensure_ascii=False, indent=1), 0o640,
                                  test=lambda p: ["/usr/bin/sing-box", "check", "-c", p], group="sing-box")
            msg.append(f"hysteria2: udp/{h['HY2_PORT']}{restart_if('sing-box', sc)}")
        if s:
            n, gone = subs(u, x, h, s)
            msg.append(f"подписок: {n}" + (f", отозвано {gone}" if gone else ""))
            if s.get("ROUTING", "1") == "1":
                r, notes = routing(s)
                msg += [t for t in [r] + notes if t]
    if m and m.get("BACKEND") == "mtproxyl":
        msg.append(f"MTProxyL: {len(mtproxyl_users())} пользователей")
    elif m:
        msg.append(apply_telemt(u, m))
    if not quiet:
        print("; ".join(msg) if msg else "нечего собирать: ни один модуль не стоит")


def card(name, u):
    x, h, s = env("xray"), env("hy2"), env("sub")
    c = u["clients"][name]
    out = {"name": name, "links": links(name, c, u, x, h)}
    if s:
        out["sub_url"] = f"https://{s['SUB_NAME']}/u/{c['token']}"
        if s.get("ROUTING", "1") == "1":
            out["routing"] = {"happ": f"https://{s['SUB_NAME']}/h", "incy": f"https://{s['SUB_NAME']}/i"}
    return out


def tg_mtproxyl(cmd, a):
    """tg-* для MTProxyL: реестр пользователей — его, вывод — как у своего telemt."""
    if cmd == "list":
        for n in sorted(mtproxyl_users()):
            print(n)
        return
    if len(a) != 2 or cmd not in ("add", "del", "show"):
        sys.exit("использование: vpnkit-gen.py tg-list | tg-add|tg-del|tg-show <имя>")
    name = a[1]
    if not re.match(r"^[A-Za-z0-9_-]{1,32}$", name):
        sys.exit("имя пользователя MTProxyL: латиница, цифры, _ -, до 32 знаков")
    have = mtproxyl_users()
    if cmd == "add":
        if name in have:
            sys.exit(f"клиент {name} уже есть")
        mtproxyl("secret", "add", name)
        print(json.dumps(mtproxyl_link(name), ensure_ascii=False))
    elif cmd == "del":
        if name not in have:
            sys.exit(f"клиента {name} нет")
        mtproxyl("secret", "remove", name)
        print(f"клиент {name} удалён")
    else:
        if name not in have:
            sys.exit(f"клиента {name} нет")
        print(json.dumps(mtproxyl_link(name), ensure_ascii=False))


def main():
    a = sys.argv[1:]
    if not a or a[0] in ("-h", "--help"):
        print(__doc__)
        return
    cmd = a[0]
    if cmd == "build":
        build()
        return
    tg = cmd.startswith("tg-")
    if tg:
        m = env("mtproxy")
        if not m:
            sys.exit("нет /etc/vpnkit/mtproxy.env — модуль mtproxy не стоит")
        cmd = cmd[3:]
    elif not env("xray"):
        sys.exit("нет /etc/vpnkit/xray.env — модуль xray не стоит")
    if tg and m.get("BACKEND") == "mtproxyl":
        return tg_mtproxyl(cmd, a)
    reg = "mtproto" if tg else "clients"
    if cmd == "list":
        for n in sorted(load_users()[reg]):
            if not n.startswith("_"):
                print(n)
        return
    if len(a) != 2 or cmd not in ("add", "del", "show"):
        sys.exit("использование: vpnkit-gen.py build | [tg-]list | [tg-]add|del|show <имя>")
    name = a[1]
    if not NAME_RE.match(name) or name.startswith("_"):
        sys.exit("имя клиента: латиница, цифры, . _ -, до 32 знаков, не с «_»")
    u = load_users()
    if cmd == "add":
        if name in u[reg]:
            sys.exit(f"клиент {name} уже есть")
        if tg:
            u[reg][name] = secrets.token_hex(16)
        else:
            u[reg][name] = {"uuid": str(uuid.uuid4()), "token": secrets.token_hex(16),
                            "hy2": secrets.token_urlsafe(16),
                            "created": datetime.date.today().isoformat()}
        save_users(u)
        build(quiet=True)
        print(json.dumps(tg_link(name, u, m) if tg else card(name, u), ensure_ascii=False))
    elif cmd == "del":
        if name not in u[reg]:
            sys.exit(f"клиента {name} нет")
        del u[reg][name]
        save_users(u)
        build(quiet=True)
        print(f"клиент {name} удалён")
    else:
        if name not in u[reg]:
            sys.exit(f"клиента {name} нет")
        print(json.dumps(tg_link(name, u, m) if tg else card(name, u), ensure_ascii=False))


if __name__ == "__main__":
    main()
