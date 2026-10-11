#!/bin/bash
# Сквозная проверка ссылок подписки с ДРУГОГО узла: для каждой ссылки vless:// / hysteria2://
# поднимается временный клиент (xray или sing-box) на петле, через него curl к api.ipify.org.
# Ничего не ставит в систему: бинарники — во временный каталог по пину, всё удаляется.
# Запуск: LINKS="<ссылка>\n<ссылка>" bash link-test.sh   (ссылки — секрет: только через stdin/env)
set -uo pipefail
: "${LINKS:?}" "${XRAY_VER:=26.3.27}" "${XRAY_SHA:=23cd9af937744d97776ee35ecad4972cf4b2109d1e0fe6be9930467608f7c8ae}"
: "${SB_VER:=1.13.18}" "${SB_SHA:=c011a13c48e1fe66e39973b1dd7ec01d61ac7a0ac9e541024be9d452121c5761}"
T=$(mktemp -d /tmp/.vk-e2e.XXXXXX); trap 'kill $(jobs -p) 2>/dev/null; rm -rf "$T"' EXIT
cd "$T" || exit 1
need_xray() {
  [ -x xray ] && return 0
  curl -fsSL -m 300 -o x.zip "https://github.com/XTLS/Xray-core/releases/download/v$XRAY_VER/Xray-linux-64.zip" || return 1
  [ "$(sha256sum x.zip | cut -d' ' -f1)" = "$XRAY_SHA" ] || { echo "sha256 xray не совпал"; return 1; }
  python3 -c "import zipfile;zipfile.ZipFile('x.zip').extract('xray','.')" && chmod +x xray
}
need_sb() {
  [ -x sing-box ] && return 0
  curl -fsSL -m 300 -o sb.deb "https://github.com/SagerNet/sing-box/releases/download/v$SB_VER/sing-box_${SB_VER}_linux_amd64.deb" || return 1
  [ "$(sha256sum sb.deb | cut -d' ' -f1)" = "$SB_SHA" ] || { echo "sha256 sing-box не совпал"; return 1; }
  dpkg-deb -x sb.deb sbx && cp sbx/usr/bin/sing-box .
}
port=20800; rc=0
while IFS= read -r link; do
  [ -n "$link" ] || continue
  port=$((port+1)); name=$(python3 -c 'import sys,urllib.parse as u;print(u.unquote(sys.argv[1].split("#",1)[1]) if "#" in sys.argv[1] else "?")' "$link")
  case "$link" in
    vless://*) need_xray || { echo "✗ $name: нет xray"; rc=1; continue; }
      python3 - "$link" "$port" > c$port.json <<'PY'
import json, sys, urllib.parse as up
link, port = sys.argv[1], int(sys.argv[2])
u = up.urlsplit(link); q = dict(up.parse_qsl(u.query))
ss = {"network": q.get("type", "tcp"), "security": q.get("security", "none")}
if ss["network"] == "xhttp":
    ss["xhttpSettings"] = {"path": q.get("path", "/")}
if ss["security"] == "tls":
    ss["tlsSettings"] = {"serverName": q.get("sni", u.hostname), "fingerprint": q.get("fp", "chrome")}
print(json.dumps({"log": {"loglevel": "warning"},
  "inbounds": [{"listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": {"udp": True}}],
  "outbounds": [{"protocol": "vless", "settings": {"vnext": [{"address": u.hostname, "port": u.port,
     "users": [{"id": u.username, "encryption": q.get("encryption", "none")}]}]}, "streamSettings": ss}]}))
PY
      ./xray run -config c$port.json >log$port 2>&1 & ;;
    hysteria2://*) need_sb || { echo "✗ $name: нет sing-box"; rc=1; continue; }
      python3 - "$link" "$port" > c$port.json <<'PY'
import json, sys, urllib.parse as up
link, port = sys.argv[1], int(sys.argv[2])
u = up.urlsplit(link); q = dict(up.parse_qsl(u.query))
print(json.dumps({"log": {"level": "warn"},
  "inbounds": [{"type": "socks", "listen": "127.0.0.1", "listen_port": port}],
  "outbounds": [{"type": "hysteria2", "server": u.hostname, "server_port": u.port,
     "password": up.unquote(u.username), "tls": {"enabled": True, "server_name": q.get("sni", u.hostname)}}]}))
PY
      ./sing-box run -c c$port.json >log$port 2>&1 & ;;
    *) echo "? неизвестная ссылка: ${link%%:*}"; continue ;;
  esac
  sleep 2
  t0=$(date +%s.%N)
  ip=$(curl -s -m 20 -x "socks5h://127.0.0.1:$port" https://api.ipify.org || true)
  ms=$(python3 -c "import time;print(int((time.time()-$t0)*1000))")
  sp=$(curl -s -m 30 -x "socks5h://127.0.0.1:$port" -o /dev/null -w '%{speed_download}' https://speed.cloudflare.com/__down?bytes=20000000 || echo 0)
  if [ -n "$ip" ]; then
    printf '✓ %-28s внешний IP %-15s  %4d мс  %s Мбит/с\n' "$name" "$ip" "$ms" "$(python3 -c "print(round($sp*8/1e6,1))")"
  else
    echo "✗ $name: нет ответа через туннель"; tail -3 log$port | sed 's/^/    /'; rc=1
  fi
  kill %% 2>/dev/null; wait 2>/dev/null
done <<<"$(printf '%b' "$LINKS")"
exit $rc
