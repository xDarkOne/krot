: "${VK_CSQTT_PASSWORD:?}" "${VK_CSQTT_ADMIN_PASSWORD:?}" "${VK_PORT:?}" "${VK_WEB_PORT:?}"
mkdir -p /etc/csqtt
chmod 700 /etc/csqtt
if [ -s "$VK_RUN/src/csqtt" ]; then
  put /usr/local/bin/csqtt 755 < "$VK_RUN/src/csqtt"
fi
[ -x /usr/local/bin/csqtt ] || die "нет проверенного бинарника CSQTT"
cat <<CONFIG | put /etc/csqtt/csqtt.env 600
CSQTT_MAIN_PASSWORD=$VK_CSQTT_PASSWORD
CSQTT_WEB_USER=admin
CSQTT_WEB_PASS=$VK_CSQTT_ADMIN_PASSWORD
CSQTT_DNS=$VK_GW
CONFIG
put /usr/local/sbin/vpnkit-csqtt-route 755 < "$VK_RUN/src/vpnkit-tun-route.sh"
. "$VK_RUN/src/vpnkit-tun-nft.sh"
route_lines=""
if [ "$VK_CASCADE" = 1 ]; then
  route_lines="ExecStartPost=/usr/local/sbin/vpnkit-csqtt-route up $VK_IFACE $VK_NET 216
ExecStopPost=-/usr/local/sbin/vpnkit-csqtt-route down $VK_IFACE $VK_NET 216"
fi
cat <<UNIT | put /etc/systemd/system/csqtt.service 644
[Unit]
Description=Krot CSQTT server
After=network-online.target nftables.service awg-quick@awg0.service
Wants=network-online.target
[Service]
EnvironmentFile=/etc/csqtt/csqtt.env
Environment=CSQTT_SERVICE_MANAGER=systemd
ExecStart=/usr/local/bin/csqtt --listen 0.0.0.0:$VK_PORT --web-port $VK_WEB_PORT --config-dir /etc/csqtt
$route_lines
Restart=on-failure
RestartSec=3
UMask=0077
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl is-enabled -q csqtt.service || echo csqtt.service >> "$VK_RUN/created.list"
systemctl enable -q csqtt.service
if changed /etc/csqtt/csqtt.env || changed /usr/local/bin/csqtt || changed /etc/systemd/system/csqtt.service; then
  systemctl restart csqtt.service
else
  systemctl start csqtt.service
fi

printf 'listen-address=%s\n' "$VK_GW" | put "/etc/dnsmasq.d/31-vpnkit-$VK_MODULE.conf" 644
dnsmasq --test
if changed "/etc/dnsmasq.d/31-vpnkit-$VK_MODULE.conf"; then systemctl restart dnsmasq; fi
