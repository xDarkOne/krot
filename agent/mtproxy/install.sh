# mtproxy: MTProto-прокси для Telegram за nginx stream на 443. Два вида (VK_BACKEND):
#  mtproxyl — MTProxyL по тегу (по умолчанию): бинарник telemt под systemd, бот, WEB Proxy
#             за нашим nginx (раскладка WEB_FRONTEND=haproxy), перебор carrier, zapret2, перезапуск
#             при падении DC, проверка доступности; пользователи — в MTProxyL (tg-* генератора);
#  telemt   — свой telemt (официальный релиз по пину), пользователи в /etc/vpnkit/users.json.
# FakeTLS под именем витрины; не-Telegram клиентов telemt отдаёт самой витрине (маска).
# Параметры: VK_BACKEND, VK_TELEMT_VERSION, VK_TELEMT_SHA256_AMD64/ARM64, VK_TLS_DOMAIN, VK_PUBLIC_HOST,
# VK_LISTEN_PORT, VK_API_PORT, VK_METRICS_PORT, VK_CASCADE; для mtproxyl — VK_MTPROXYL_TAG,
# VK_WEB_DOMAIN, VK_TGBOT_TOKEN/VK_TGBOT_ADMIN (необязательно). Описание — docs/20-server-modules/mtproxy.md
: "${VK_TELEMT_VERSION:?}" "${VK_TELEMT_SHA256_AMD64:?}" "${VK_TELEMT_SHA256_ARM64:?}" "${VK_TLS_DOMAIN:?}"
: "${VK_PUBLIC_HOST:=$VK_TLS_DOMAIN}" "${VK_LISTEN_PORT:=15443}" "${VK_API_PORT:=9091}" "${VK_METRICS_PORT:=9090}" "${VK_CASCADE:=0}"
: "${VK_BACKEND:=telemt}"
[ -s "$VK_RUN/src/vpnkit-gen.py" ] || die "нет vpnkit-gen.py от пульта"
grep -qx 'STREAM=1' /etc/vpnkit/nginx.env 2>/dev/null || die "нет nginx-tls с разбором 443 (entry) — сначала nginx-tls"
[ -f "/etc/nginx/sites-available/vpnkit-$VK_TLS_DOMAIN" ] || die "нет витрины $VK_TLS_DOMAIN — маске telemt нужен настоящий сайт"

# MSS 1200 на внешнем :443 (P-07) — общее для обоих видов. stun=1: STUN telemt через каскад
# (только свой telemt: он работает от пользователя telemt, MTProxyL — от root)
mss_table() {  # mss_table <stun 0|1>
  local wan
  wan=$(ip -o -4 route show default | awk '{print $5; exit}')
  [ -n "$wan" ] || die "не найден внешний интерфейс"
  {
    echo "# vpnkit mtproxy. Своя таблица: чужие не трогаются."
    echo "table inet vpnkit_mtproxy {"
    echo "}"
    echo "flush table inet vpnkit_mtproxy"
    echo "table inet vpnkit_mtproxy {"
    echo "	chain mss_in {"
    echo "		type filter hook prerouting priority mangle - 10; policy accept;"
    echo "		iifname \"$wan\" tcp dport 443 tcp flags & (syn | ack) == syn tcp option maxseg size > 1200 tcp option maxseg size set 1200"
    echo "	}"
    echo "	chain mss_out {"
    echo "		type filter hook postrouting priority mangle - 10; policy accept;"
    echo "		oifname \"$wan\" tcp sport 443 tcp flags & (syn | ack) == syn | ack tcp option maxseg size > 1200 tcp option maxseg size set 1200"
    echo "	}"
    if [ "$1" = 1 ]; then
      echo "	chain stun {"
      echo "		type route hook output priority mangle; policy accept;"
      echo "		meta skuid \"telemt\" udp dport { 3478, 3479, 5349, 19302 } meta mark set 0x00000003"
      echo "	}"
    fi
    echo "}"
  } | put /etc/nftables.d/47-vpnkit-mtproxy.nft 644
  nft -c -f /etc/nftables.conf
  systemctl reload nftables
}

if [ "$VK_BACKEND" = mtproxyl ]; then
  # shellcheck source=/dev/null
  . "$VK_RUN/src/mtproxyl-install.sh"
  exit 0
fi
[ "$VK_BACKEND" = telemt ] || die "VK_BACKEND=$VK_BACKEND — бывает mtproxyl или telemt"

for p in $VK_LISTEN_PORT $VK_API_PORT $VK_METRICS_PORT; do
  l=$(ss -Hltnp "sport = :$p")
  if [ -n "$l" ] && ! grep -q '"telemt"' <<<"$l"; then die "TCP $p занят: $l"; fi
done
apt_install python3 curl

# --- бинарник по пину (официальный релиз telemt; sha256 совпадает с его .sha256)
case "$(dpkg --print-architecture)" in
  amd64) a=x86_64-linux-gnu; sum=$VK_TELEMT_SHA256_AMD64 ;;
  arm64) a=aarch64-linux-gnu; sum=$VK_TELEMT_SHA256_ARM64 ;;
  *) die "архитектура $(dpkg --print-architecture) не поддерживается" ;;
esac
have=$(/usr/local/bin/telemt --version 2>/dev/null | awk '{print $2}' || true)
if [ "$have" != "$VK_TELEMT_VERSION" ]; then
  f=$VK_RUN/telemt.tgz
  curl -fsSL -m 300 -o "$f" "https://github.com/telemt/telemt/releases/download/$VK_TELEMT_VERSION/telemt-$a.tar.gz" \
    || die "не скачать telemt $VK_TELEMT_VERSION с GitHub"
  [ "$(sha256sum "$f" | cut -d' ' -f1)" = "$sum" ] || die "sha256 telemt не совпал с пином — не ставлю"
  mkdir -p "$VK_RUN/telemt" && tar -xzf "$f" -C "$VK_RUN/telemt" telemt
  if [ -x /usr/local/bin/telemt ]; then cp -a /usr/local/bin/telemt "$VK_RUN/telemt.prev"; else echo telemt-bin >> "$VK_RUN/created.list"; fi
  install -m 755 "$VK_RUN/telemt/telemt" /usr/local/bin/telemt.new && mv /usr/local/bin/telemt.new /usr/local/bin/telemt
  say "telemt $VK_TELEMT_VERSION поставлен (sha256 сверен)"
else
  say "telemt $VK_TELEMT_VERSION уже стоит"
fi
id telemt >/dev/null 2>&1 || useradd --system --no-create-home --home-dir /var/lib/telemt --shell /usr/sbin/nologin telemt
mkdir -p /etc/telemt && chown root:telemt /etc/telemt && chmod 750 /etc/telemt

put /usr/local/sbin/vpnkit-gen.py 755 < "$VK_RUN/src/vpnkit-gen.py"
cat <<EOF | put /etc/vpnkit/mtproxy.env 600
# vpnkit mtproxy. Пишет пульт.
TLS_DOMAIN=$VK_TLS_DOMAIN
PUBLIC_HOST=$VK_PUBLIC_HOST
LISTEN_PORT=$VK_LISTEN_PORT
API_PORT=$VK_API_PORT
METRICS_PORT=$VK_METRICS_PORT
EOF
cat <<'EOF' | put /etc/systemd/system/telemt.service 644
[Unit]
Description=telemt — MTProto-прокси (vpnkit)
After=network-online.target
Wants=network-online.target

[Service]
User=telemt
Group=telemt
StateDirectory=telemt
WorkingDirectory=/var/lib/telemt
ExecStart=/usr/local/bin/telemt /etc/telemt/telemt.toml
Restart=on-failure
RestartSec=3
LimitNOFILE=65536
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

mss_table "$VK_CASCADE"

snapshot /etc/telemt/telemt.toml /etc/nginx/stream.d/vpnkit-443.conf /etc/vpnkit/users.json
systemctl daemon-reload
systemctl enable -q telemt
restart=0
[ "$have" != "$VK_TELEMT_VERSION" ] && restart=1
grep -qxF /etc/systemd/system/telemt.service "$VK_RUN/changed.list" 2>/dev/null && restart=1
# генератор пишет конфиг telemt и переключает 443 на него (пользователи — без перезапуска)
out=$(/usr/local/sbin/vpnkit-gen.py build)
say "$out"
# генератор сам перезапускает telemt, если изменилась не только часть с пользователями —
# второй перезапуск подряд рвал бы ME-соединения на старте
if grep -q 'telemt перезапущен' <<<"$out"; then restart=0; fi
if [ $restart = 1 ] || ! systemctl is-active -q telemt; then systemctl restart telemt; fi
# порт появляется не сразу: telemt сначала опрашивает STUN (P-22)
i=0
until grep -q . <<<"$(ss -Hltn "sport = :$VK_LISTEN_PORT")" || [ $i -ge 30 ]; do sleep 1; i=$((i+1)); done
grep -q . <<<"$(ss -Hltn "sport = :$VK_LISTEN_PORT")" || die "telemt не открыл 127.0.0.1:$VK_LISTEN_PORT за 30 с: $(journalctl -u telemt -n 5 -o cat --no-pager)"
say "telemt: FakeTLS под именем $VK_TLS_DOMAIN, TCP/443 → telemt (порт открылся через $i с), STUN $([ "$VK_CASCADE" = 1 ] && echo 'через каскад' || echo 'напрямую')"
