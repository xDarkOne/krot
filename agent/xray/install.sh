# xray: входы подписки VLESS XHTTP (за nginx с TLS) и VLESS Encryption (PQ), петлевой SOCKS для hysteria2.
# Параметры: VK_XRAY_VERSION, VK_XRAY_SHA256_AMD64/ARM64 (пин), VK_TUNNEL_NAME, VK_XHTTP_PORT,
# VK_XH_PORT, VK_XH_PATH, VK_ENC_PORT, VK_ENC_PATH, VK_SOCKS_PORT, VK_CASCADE, VK_LABEL.
# Описание — docs/20-server-modules/xray.md
: "${VK_XRAY_VERSION:?}" "${VK_XRAY_SHA256_AMD64:?}" "${VK_XRAY_SHA256_ARM64:?}" "${VK_TUNNEL_NAME:?}"
: "${VK_XHTTP_PORT:?}" "${VK_XH_PORT:?}" "${VK_XH_PATH:?}" "${VK_ENC_PORT:?}" "${VK_ENC_PATH:?}"
: "${VK_SOCKS_PORT:=10801}" "${VK_CASCADE:=0}" "${VK_LABEL:=}"
[ -s "$VK_RUN/src/vpnkit-gen.py" ] || die "нет vpnkit-gen.py от пульта"
[ -f "/etc/nginx/sites-available/vpnkit-$VK_TUNNEL_NAME" ] || die "нет витрины $VK_TUNNEL_NAME — сначала nginx-tls"
if [ "$VK_CASCADE" = 1 ]; then
  [ -s /var/lib/vpnkit/ru-nets.txt ] || die "нет списка российских сетей — сначала split-routing"
  grep -q 'fwmark 0x3 lookup 200' <<<"$(ip rule)" || die "нет ip rule fwmark 0x3 → каскад — сначала cascade-awg0"
fi
apt_install python3 curl

# --- бинарник по пину: скачать, сверить sha256, только потом ставить
case "$(dpkg --print-architecture)" in
  amd64) asset=Xray-linux-64.zip; sum=$VK_XRAY_SHA256_AMD64 ;;
  arm64) asset=Xray-linux-arm64-v8a.zip; sum=$VK_XRAY_SHA256_ARM64 ;;
  *) die "архитектура $(dpkg --print-architecture) не поддерживается" ;;
esac
have=$(/usr/local/bin/xray version 2>/dev/null | awk 'NR==1{print $2}' || true)
if [ "$have" != "$VK_XRAY_VERSION" ]; then
  z=$VK_RUN/xray.zip
  curl -fsSL -m 300 -o "$z" "https://github.com/XTLS/Xray-core/releases/download/v$VK_XRAY_VERSION/$asset" \
    || die "не скачать $asset с GitHub"
  [ "$(sha256sum "$z" | cut -d' ' -f1)" = "$sum" ] || die "sha256 $asset не совпал с пином — не ставлю"
  mkdir -p "$VK_RUN/xray"
  python3 -c "import zipfile,sys;zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])" "$z" "$VK_RUN/xray"
  [ -x /usr/local/bin/xray ] && cp -a /usr/local/bin/xray "$VK_RUN/xray.prev"
  [ -x /usr/local/bin/xray ] || echo xray-bin >> "$VK_RUN/created.list"
  install -m 755 "$VK_RUN/xray/xray" /usr/local/bin/xray.new && mv /usr/local/bin/xray.new /usr/local/bin/xray
  mkdir -p /usr/local/share/xray
  install -m 644 "$VK_RUN/xray/geosite.dat" "$VK_RUN/xray/geoip.dat" /usr/local/share/xray/
  say "xray $VK_XRAY_VERSION поставлен (sha256 сверен)"
else
  say "xray $VK_XRAY_VERSION уже стоит"
fi

# --- реестр клиентов: создаётся один раз; ключ vlessenc при пересборке не меняется
mkdir -p /etc/vpnkit; chmod 700 /etc/vpnkit
if ! python3 -c 'import json,sys;sys.exit(0 if "vlessenc" in json.load(open("/etc/vpnkit/users.json")) else 1)' 2>/dev/null; then
  # пара с аутентификацией X25519: обмен ключами всё равно ML-KEM-768,
  # а ссылка короткая (74 знака против ~1,6 КБ) и влезает в QR
  pair=$(/usr/local/bin/xray vlessenc | awk '/^Authentication: X25519/{f=1;next} /^Authentication:/{f=0} f' )
  dec=$(sed -n 's/^"decryption": "\(.*\)"$/\1/p' <<<"$pair")
  enc=$(sed -n 's/^"encryption": "\(.*\)"$/\1/p' <<<"$pair")
  [ -n "$dec" ] && [ -n "$enc" ] || die "xray vlessenc: не разобрать вывод"
  snapshot /etc/vpnkit/users.json
  # реестр мог создать модуль mtproxy — дописываем ключ, остальное не трогаем
  VK_DEC=$dec VK_ENC=$enc python3 - <<'PY'
import json, os
p = "/etc/vpnkit/users.json"
u = json.load(open(p)) if os.path.exists(p) else {"schema": 2, "clients": {}, "mtproto": {}}
u["vlessenc"] = {"decryption": os.environ["VK_DEC"], "encryption": os.environ["VK_ENC"]}
u.setdefault("clients", {})
with open(os.open(p + ".new", os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w") as f:
    json.dump(u, f, ensure_ascii=False, indent=1)
os.replace(p + ".new", p)
PY
  say "ключ VLESS Encryption создан"
fi

put /usr/local/sbin/vpnkit-gen.py 755 < "$VK_RUN/src/vpnkit-gen.py"
cat <<EOF | put /etc/vpnkit/xray.env 600
# vpnkit xray. Пишет пульт.
TUNNEL_NAME=$VK_TUNNEL_NAME
XHTTP_PORT=$VK_XHTTP_PORT
XH_PORT=$VK_XH_PORT
XH_PATH=$VK_XH_PATH
ENC_PORT=$VK_ENC_PORT
ENC_PATH=$VK_ENC_PATH
SOCKS_PORT=$VK_SOCKS_PORT
CASCADE=$VK_CASCADE
LABEL=$VK_LABEL
EOF

# --- служба: nobody + CAP_NET_ADMIN (нужен для sockopt.mark каскада)
cat <<'EOF' | put /etc/systemd/system/xray.service 644
[Unit]
Description=Xray (vpnkit)
After=network-online.target nss-lookup.target dnsmasq.service
Wants=network-online.target

[Service]
User=nobody
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
NoNewPrivileges=true
Environment=XRAY_LOCATION_ASSET=/usr/local/share/xray
ExecStart=/usr/local/bin/xray run -config /usr/local/etc/xray/config.json
Restart=on-failure
RestartPreventExitStatus=23
LimitNOFILE=1000000

[Install]
WantedBy=multi-user.target
EOF
cat <<'EOF' | put /etc/systemd/system/vpnkit-gen.service 644
[Unit]
Description=vpnkit: пересборка xray/sing-box/подписок (списки РФ обновляются раз в сутки)
After=vpnkit-ru-nets.service
[Service]
Type=oneshot
ExecStart=/usr/local/sbin/vpnkit-gen.py build
EOF
printf '[Unit]\nDescription=vpnkit: суточная пересборка конфигов\n[Timer]\nOnCalendar=*-*-* 05:15:00\nRandomizedDelaySec=30min\nPersistent=true\n[Install]\nWantedBy=timers.target\n' \
  | put /etc/systemd/system/vpnkit-gen.timer 644
systemctl is-enabled -q xray 2>/dev/null || echo xray >> "$VK_RUN/created.list"
systemctl daemon-reload

snapshot /usr/local/etc/xray/config.json   # генератор пишет сам — снимок до него
systemctl enable -q xray vpnkit-gen.timer
systemctl start vpnkit-gen.timer
# перезапуск рвёт сессии клиентов — только если что-то поменялось: бинарник, юнит
# (конфиг генератор перезапускает сам, когда он изменился)
restart=0
[ "$have" != "$VK_XRAY_VERSION" ] && restart=1
grep -qxF /etc/systemd/system/xray.service "$VK_RUN/changed.list" 2>/dev/null && restart=1
/usr/local/sbin/vpnkit-gen.py build >/dev/null
if [ $restart = 1 ] || ! systemctl is-active -q xray; then systemctl restart xray; say "xray перезапущен"; fi

# --- снаружи: порт PQ; XHTTP — через nginx на $VK_XHTTP_PORT
cat <<EOF | put /etc/nftables.d/45-vpnkit-xray.nft 644
# vpnkit xray: VLESS Encryption (PQ) напрямую
table inet vpnkit_fw {
	chain input {
		tcp dport $VK_ENC_PORT accept
	}
}
EOF
nft -c -f /etc/nftables.conf
systemctl reload nftables
cat <<EOF | put "/etc/nginx/vpnkit.d/$VK_TUNNEL_NAME-xhttp-xray.conf" 644
# vpnkit xray: VLESS XHTTP. Путь похож на каталог статики; остальное на этом порту — витрина.
location $VK_XH_PATH/ {
    access_log off;   # туннель: адреса клиентов и время сессий журналу не нужны
    proxy_pass http://127.0.0.1:$VK_XH_PORT;
    proxy_http_version 1.1;
    proxy_set_header Host \$host;
    proxy_buffering off;
    proxy_request_buffering off;
    proxy_read_timeout 300s;
    proxy_send_timeout 300s;
}
EOF
nginx -t -q
systemctl reload nginx
say "xray: XHTTP $VK_TUNNEL_NAME:$VK_XHTTP_PORT, PQ tcp/$VK_ENC_PORT, каскад: $([ "$VK_CASCADE" = 1 ] && echo да || echo нет)"
