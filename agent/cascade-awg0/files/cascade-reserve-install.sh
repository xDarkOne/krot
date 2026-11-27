# Исключительно файлы резервного каскада. Бинарники отделены от клиентских служб.
reserve=/etc/vpnkit/cascade
bin=/usr/local/lib/vpnkit/cascade
units=/etc/systemd/system

reserve_service() {
  local unit=$1
  shift
  systemctl is-enabled -q "$unit" 2>/dev/null || echo "$unit" >> "$VK_RUN/created.list"
  systemctl enable -q "$unit"
  if changed "$units/$unit" || ! systemctl is-active -q "$unit" ||
     { [ "$#" -ge 1 ] && changed "$1"; } ||
     { [ "$#" -ge 2 ] && changed "$2"; }; then
    systemctl restart "$unit"
  fi
}

if [ -n "${VK_REALITY_NAME:-}" ]; then
  mkdir -p "$reserve"
  chmod 700 "$reserve"
  xhave=$("$bin/xray" version 2>/dev/null | awk 'NR==1{print $2}' || true)
  if [ "$xhave" != "$VK_XRAY_VERSION" ]; then
    zip=$VK_RUN/src/cascade-xray.zip
    [ -s "$zip" ] || die "нет проверенного архива Xray от пульта"
    python3 -c 'import shutil,sys,zipfile; shutil.copyfileobj(zipfile.ZipFile(sys.argv[1]).open("xray"), sys.stdout.buffer)' "$zip" |
      put "$bin/xray" 755
  fi
  python3 "$VK_RUN/src/cascade-config.py" xray | put "$reserve/xray.json" 600
  "$bin/xray" run -test -config "$reserve/xray.json" >/dev/null
  # systemd загружает конфиг через приватные credentials, Xray работает непривилегированно.
  cat <<'UNIT' | put "$units/vpnkit-cascade-xray.service" 644
[Unit]
Description=Krot cascade Reality
Wants=network-online.target
After=network-online.target
[Service]
DynamicUser=yes
NoNewPrivileges=yes
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
LoadCredential=config:/etc/vpnkit/cascade/xray.json
ExecStart=/usr/local/lib/vpnkit/cascade/xray run -format json -config %d/config
Restart=on-failure
RestartSec=3
[Install]
WantedBy=multi-user.target
UNIT
  if [ "$VK_SIDE" = client ]; then
    shave=$("$bin/sing-box" version 2>/dev/null | awk 'NR==1{print $3}' || true)
    if [ "$shave" != "$VK_SINGBOX_VERSION" ]; then
      deb=$VK_RUN/src/cascade-sing-box.deb
      [ -s "$deb" ] || die "нет проверенного пакета sing-box от пульта"
      dpkg-deb -x "$deb" "$VK_RUN/cascade-sing-box"
      put "$bin/sing-box" 755 < "$VK_RUN/cascade-sing-box/usr/bin/sing-box"
    fi
    python3 "$VK_RUN/src/cascade-config.py" tun | put "$reserve/tun.json" 600
    "$bin/sing-box" check -c "$reserve/tun.json"
    cat <<'UNIT' | put "$units/vpnkit-cascade-tun.service" 644
[Unit]
Description=Krot cascade backup TUN
Wants=vpnkit-cascade-xray.service
After=vpnkit-cascade-xray.service
[Service]
ExecStart=/usr/local/lib/vpnkit/cascade/sing-box run -c /etc/vpnkit/cascade/tun.json
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
NoNewPrivileges=yes
Restart=on-failure
RestartSec=3
[Install]
WantedBy=multi-user.target
UNIT
  fi
fi

if [ "$VK_SIDE" = client ]; then
  python3 "$VK_RUN/src/cascade-config.py" watch | put "$reserve/watch.json" 600
  put "$bin/watch" 755 < "$VK_RUN/src/cascade-watch.py"
  cat <<'UNIT' | put "$units/vpnkit-cascade-watch.service" 644
[Unit]
Description=Krot cascade watchdog
Wants=network-online.target
After=network-online.target awg-quick@awg0.service vpnkit-cascade-tun.service
[Service]
ExecStart=/usr/local/lib/vpnkit/cascade/watch /etc/vpnkit/cascade/watch.json
ExecStopPost=-/usr/local/lib/vpnkit/cascade/watch /etc/vpnkit/cascade/watch.json restore
Restart=always
RestartSec=2
[Install]
WantedBy=multi-user.target
UNIT
fi

systemctl daemon-reload
if [ -n "${VK_REALITY_NAME:-}" ]; then
  reserve_service vpnkit-cascade-xray.service "$reserve/xray.json" "$bin/xray"
  if [ "$VK_SIDE" = client ]; then
    reserve_service vpnkit-cascade-tun.service "$reserve/tun.json" "$bin/sing-box"
  fi
elif [ -e "$units/vpnkit-cascade-xray.service" ]; then
  for unit in vpnkit-cascade-tun.service vpnkit-cascade-xray.service; do
    [ -e "$units/$unit" ] || continue
    systemctl is-enabled -q "$unit" && echo "$unit" >> "$VK_RUN/disabled.list"
    systemctl disable -q --now "$unit"
  done
fi
if [ "$VK_SIDE" = client ]; then
  reserve_service vpnkit-cascade-watch.service "$reserve/watch.json" "$bin/watch"
fi
