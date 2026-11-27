: "${VK_PUBLIC_HOST:?}" "${VK_PORT:?}"
mkdir -p /etc/aivpn
chmod 700 /etc/aivpn
if [ ! -e /etc/aivpn/server.key ]; then
  head -c 32 /dev/urandom | put /etc/aivpn/server.key 600
fi
if [ -s "$VK_RUN/src/aivpn-server" ]; then
  put /usr/local/bin/aivpn-server 755 < "$VK_RUN/src/aivpn-server"
fi
[ -x /usr/local/bin/aivpn-server ] || die "нет проверенного бинарника AIVPN"
mkdir -p "$VK_RUN/masks"
tar -xzf "$VK_RUN/src/aivpn-masks.tar.gz" -C "$VK_RUN/masks" --no-same-owner
mapfile -t masks < <(find "$VK_RUN/masks" -type f -name '*.json' -print)
[ "${#masks[@]}" -gt 0 ] || die "в релизе AIVPN нет масок"
for mask in "${masks[@]}"; do
  target=/var/lib/aivpn/masks/${mask##*/}
  [ -e "$target" ] || put "$target" 644 < "$mask"
done
cat <<CONFIG | put /etc/aivpn/server.json 600
{
  "listen_addr": "0.0.0.0:$VK_PORT",
  "server_ip": "$VK_PUBLIC_HOST:$VK_PORT",
  "key_file": "/etc/aivpn/server.key",
  "clients_db": "/etc/aivpn/clients.json",
  "tun_name": "$VK_IFACE",
  "enable_nat": true,
  "network_config": {"server_vpn_ip": "$VK_GW", "prefix_len": 24, "mtu": 1260}
}
CONFIG
put "/usr/local/sbin/vpnkit-aivpn-route" 755 < "$VK_RUN/src/vpnkit-tun-route.sh"
. "$VK_RUN/src/vpnkit-tun-nft.sh"
route_lines=""
if [ "$VK_CASCADE" = 1 ]; then
  route_lines="ExecStartPost=/usr/local/sbin/vpnkit-aivpn-route up $VK_IFACE $VK_NET 218
ExecStopPost=-/usr/local/sbin/vpnkit-aivpn-route down $VK_IFACE $VK_NET 218"
fi
cat <<UNIT | put /etc/systemd/system/aivpn-server.service 644
[Unit]
Description=Krot AIVPN server
After=network-online.target nftables.service awg-quick@awg0.service
Wants=network-online.target
[Service]
ExecStart=/usr/local/bin/aivpn-server --config /etc/aivpn/server.json
$route_lines
Restart=on-failure
RestartSec=3
UMask=0077
[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl is-enabled -q aivpn-server.service || echo aivpn-server.service >> "$VK_RUN/created.list"
systemctl enable -q aivpn-server.service
if changed /etc/aivpn/ || changed /usr/local/bin/aivpn-server || changed /etc/systemd/system/aivpn-server.service; then
  systemctl restart aivpn-server.service
else
  systemctl start aivpn-server.service
fi

printf 'listen-address=%s\n' "$VK_GW" | put "/etc/dnsmasq.d/31-vpnkit-$VK_MODULE.conf" 644
dnsmasq --test
if changed "/etc/dnsmasq.d/31-vpnkit-$VK_MODULE.conf"; then systemctl restart dnsmasq; fi
