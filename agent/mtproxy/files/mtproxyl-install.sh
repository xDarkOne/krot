# mtproxy, вид mtproxyl: MTProxyL за нашим nginx на 443.
# Источник подключается из install.sh (там проверки, put/snapshot/say/die, mss_table).
# Раскладка: nginx stream 443 → по SNI: WEB-домен → 127.0.0.1:15444 (TLS-сервер nginx) → 15080
# (WEB-вход telemt); остальное → 127.0.0.1:15443 (telemt, PROXY protocol). Не-Telegram клиентов
# telemt отдаёт витрине 127.0.0.1:8444 — иначе маска ушла бы на свой же домен (петля).
: "${VK_MTPROXYL_TAG:?}" "${VK_WEB_DOMAIN:?}"
M=/opt/mtproxyl/mtproxyl.sh
WEB_TLS_PORT=15444
WEB_PORT=15080
mtl() { TERM=dumb "$M" "$@" </dev/null 2>&1 | sed 's/\x1b\[[0-9;]*m//g'; }

[ -s "/etc/letsencrypt/live/$VK_WEB_DOMAIN/fullchain.pem" ] \
  || die "нет сертификата $VK_WEB_DOMAIN — имя должно быть в nginx-tls.names (DNS A на этот сервер)"
for p in $VK_LISTEN_PORT $WEB_PORT $WEB_TLS_PORT; do
  l=$(ss -Hltnp "sport = :$p")
  if [ -n "$l" ] && ! grep -qE '"(telemt|mtproxyl-telemt?|nginx)"' <<<"$l"; then die "TCP $p занят: $l"; fi
done
apt_install python3 curl

# --- прежний свой telemt: его пользователи переезжают в MTProxyL с теми же ключами
#     (ссылки клиентов не меняются: тот же домен, порт 443 и секрет)
secrets_args=()
if [ -f /etc/vpnkit/users.json ]; then
  while IFS=' ' read -r n k; do
    [ -n "$n" ] || continue
    secrets_args+=(--secret "$n:$k")
  done < <(python3 - <<'PY'
import json, re
u = json.load(open("/etc/vpnkit/users.json")).get("mtproto", {})
for n, k in sorted(u.items()):
    if not n.startswith("_") and re.fullmatch(r"[A-Za-z0-9_-]{1,32}", n) and re.fullmatch(r"[0-9a-f]{32}", k):
        print(n, k)
PY
)
fi
if systemctl is-enabled -q telemt 2>/dev/null; then
  snapshot /etc/systemd/system/telemt.service
  systemctl disable -q --now telemt
  echo telemt-stopped >> "$VK_RUN/created.list"
  say "прежний telemt остановлен; пользователей переносится с теми же ключами: $(( ${#secrets_args[@]} / 2 ))"
fi

snapshot /etc/nginx/stream.d/vpnkit-443.conf /etc/vpnkit/mtproxy.env \
  /etc/nginx/sites-available/vpnkit-mtproxyl-web /etc/nginx/sites-enabled/vpnkit-mtproxyl-web

# --- MTProxyL по тегу, без вопросов
if [ ! -x "$M" ]; then
  i=$VK_RUN/mtproxyl-install.sh
  curl -fsSL -m 60 -o "$i" "https://raw.githubusercontent.com/Liafanx/MTProxyL/$VK_MTPROXYL_TAG/install.sh" \
    || die "не скачать установщик MTProxyL $VK_MTPROXYL_TAG"
  args=(--mode manager --engine binary --engine-version "$VK_TELEMT_VERSION"
        --proxy-mode combined --port 443 --host "$VK_PUBLIC_HOST" --sni "$VK_TLS_DOMAIN" --mask on
        --api-port "$VK_API_PORT" --metrics-port "$VK_METRICS_PORT"
        --zapret2 yes --dc-restart on
        --web-domain "$VK_WEB_DOMAIN" --web-layout shared --web-frontend haproxy
        --web-carrier https --web-carriers websocket-lanes,websocket --web-decoy empty)
  if [ -n "${VK_TGBOT_TOKEN:-}" ]; then args+=(--tgbot-token "$VK_TGBOT_TOKEN" --tgbot-admin "$VK_TGBOT_ADMIN"); fi
  echo mtproxyl-installed >> "$VK_RUN/created.list"
  say "ставлю MTProxyL $VK_MTPROXYL_TAG (движок telemt $VK_TELEMT_VERSION, 1–3 минуты)…"
  if ! bash "$i" --branch "$VK_MTPROXYL_TAG" -- "${args[@]}" "${secrets_args[@]}" > "$VK_RUN/mtproxyl-install.log" 2>&1; then
    sed 's/\x1b\[[0-9;]*m//g' "$VK_RUN/mtproxyl-install.log" | grep -vE 'tg://|t\.me/proxy' | tail -25 >&2
    die "установщик MTProxyL не прошёл"
  fi
  # тег нужен только для воспроизводимой установки: обновления MTProxyL дальше — из main, как у всех
  rm -f /opt/mtproxyl/.branch
  # установщик печатает ссылки с секретами всех пользователей — в журнале прогона им не место
  sed -i -E '/tg:\/\/|t\.me\/proxy|secret=/d' "$VK_RUN/mtproxyl-install.log"
else
  say "MTProxyL уже стоит: $(grep -m1 '^VERSION=' "$M" | cut -d'"' -f2)"
fi

# --- маска — на витрину, а не на свой же домен; доступность из России; перебор carrier
mtl settings set MASKING_HOST 127.0.0.1 >/dev/null
mtl settings set MASKING_PORT 8444 >/dev/null
mtl availability on >/dev/null || say "проверку доступности включить не удалось (mtproxyl availability on)"

# --- TLS-сервер WEB Proxy: публичный 443 → nginx stream → сюда → WEB-вход telemt
cat <<EOF | put /etc/nginx/sites-available/vpnkit-mtproxyl-web 644
# vpnkit mtproxy (MTProxyL): WEB Proxy $VK_WEB_DOMAIN. Пишет пульт.
map \$http_upgrade \$vpnkit_mtl_upgrade {
    default upgrade;
    ""      "";
}
server {
    listen 127.0.0.1:$WEB_TLS_PORT ssl proxy_protocol default_server;
    server_name _;
    server_tokens off;
    access_log off;
    ssl_certificate     /etc/letsencrypt/live/$VK_WEB_DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$VK_WEB_DOMAIN/privkey.pem;
    return 421;
}
server {
    listen 127.0.0.1:$WEB_TLS_PORT ssl proxy_protocol;
    http2 on;
    server_name $VK_WEB_DOMAIN;
    server_tokens off;
    access_log off;
    set_real_ip_from 127.0.0.1;
    real_ip_header proxy_protocol;
    ssl_certificate     /etc/letsencrypt/live/$VK_WEB_DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$VK_WEB_DOMAIN/privkey.pem;
    client_max_body_size 2m;
    location / {
        proxy_pass http://127.0.0.1:$WEB_PORT;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Forwarded-For \$remote_addr;
        proxy_set_header If-None-Match "";
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection \$vpnkit_mtl_upgrade;
        proxy_hide_header ETag;
        proxy_hide_header Last-Modified;
        add_header Strict-Transport-Security "max-age=15552000" always;
        add_header X-Content-Type-Options nosniff always;
        proxy_connect_timeout 5s;
        proxy_send_timeout 65s;
        proxy_read_timeout 65s;
        proxy_request_buffering off;
        proxy_buffering off;
        proxy_next_upstream off;
    }
}
EOF
ln -sfn /etc/nginx/sites-available/vpnkit-mtproxyl-web /etc/nginx/sites-enabled/vpnkit-mtproxyl-web
# nginx 1.24 (Ubuntu 24.04) не знает «http2 on» (P-56)
if ! nginx -t -q 2>/dev/null; then
  sed -i '/^    http2 on;$/d' /etc/nginx/sites-available/vpnkit-mtproxyl-web
  sed -i "s|listen 127.0.0.1:$WEB_TLS_PORT ssl proxy_protocol;|listen 127.0.0.1:$WEB_TLS_PORT ssl http2 proxy_protocol;|" /etc/nginx/sites-available/vpnkit-mtproxyl-web
fi
nginx -t -q 2>"$VK_RUN/nginx-t.log" || die "nginx -t: $(tail -3 "$VK_RUN/nginx-t.log")"

# --- 443 через генератор (map: WEB-домен → TLS-сервер, остальное → telemt), MSS
put /usr/local/sbin/vpnkit-gen.py 755 < "$VK_RUN/src/vpnkit-gen.py"
cat <<EOF | put /etc/vpnkit/mtproxy.env 600
# vpnkit mtproxy. Пишет пульт.
BACKEND=mtproxyl
TLS_DOMAIN=$VK_TLS_DOMAIN
PUBLIC_HOST=$VK_PUBLIC_HOST
LISTEN_PORT=$VK_LISTEN_PORT
API_PORT=$VK_API_PORT
METRICS_PORT=$VK_METRICS_PORT
WEB_DOMAIN=$VK_WEB_DOMAIN
WEB_TLS_PORT=$WEB_TLS_PORT
EOF
systemctl reload nginx
say "$(/usr/local/sbin/vpnkit-gen.py build)"
mss_table 0

# --- перезапуск движка (маска поменялась) и ожидание портов; STUN сначала (P-22)
mtl restart >/dev/null
i=0
until { grep -q . <<<"$(ss -Hltn "sport = :$VK_LISTEN_PORT")" && grep -q . <<<"$(ss -Hltn "sport = :$WEB_PORT")"; } || [ $i -ge 60 ]; do
  sleep 1; i=$((i+1))
done
grep -q . <<<"$(ss -Hltn "sport = :$VK_LISTEN_PORT")" \
  || die "telemt MTProxyL не открыл 127.0.0.1:$VK_LISTEN_PORT за 60 с: $(journalctl -u mtproxyl-telemt -n 5 -o cat --no-pager)"
say "MTProxyL $(grep -m1 '^VERSION=' "$M" | cut -d'"' -f2), telemt $(cat /opt/mtproxyl/engine/.version 2>/dev/null): FakeTLS $VK_TLS_DOMAIN, WEB Proxy $VK_WEB_DOMAIN, порты через $i с$([ -n "${VK_TGBOT_TOKEN:-}" ] && echo ', бот включён')"
