: "${VK_PORT:?}" "${VK_BACKEND_PORT:?}" "${VK_WRAP_KEY:?}"
if [ -s "$VK_RUN/src/vk-turn-source.tar.gz" ]; then
  apt_install golang-go
  mkdir -p "$VK_RUN/build"
  tar -xzf "$VK_RUN/src/vk-turn-source.tar.gz" --strip-components=1 --no-same-owner -C "$VK_RUN/build"
  (
    cd "$VK_RUN/build"
    export CGO_ENABLED=0 GOTOOLCHAIN=auto
    go build -trimpath -mod=readonly -o "$VK_RUN/vk-turn-server" ./server
  )
  put /usr/local/lib/vpnkit/vk-turn-server 755 < "$VK_RUN/vk-turn-server"
  printf '%s\n' "$VK_SOURCE_PIN" | put /usr/local/lib/vpnkit/vk-turn-source 644
fi
[ -x /usr/local/lib/vpnkit/vk-turn-server ] || die "нет собранного VK TURN Proxy"
printf 'WRAP_KEY=%s\n' "$VK_WRAP_KEY" | put /etc/vpnkit/vk-turn.env 600
cat <<EOF_UNIT | put /etc/systemd/system/vk-turn-proxy.service 644
[Unit]
Description=Krot VK TURN Proxy
After=wg-quick@$VK_IFACE.service network-online.target
Wants=network-online.target
[Service]
EnvironmentFile=/etc/vpnkit/vk-turn.env
ExecStart=/usr/local/lib/vpnkit/vk-turn-server -listen 0.0.0.0:$VK_PORT -connect 127.0.0.1:$VK_BACKEND_PORT -wrap-srtp -wrap-key \${WRAP_KEY} -uplink-reseq 20ms
DynamicUser=yes
NoNewPrivileges=yes
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
Restart=on-failure
RestartSec=3
[Install]
WantedBy=multi-user.target
EOF_UNIT
cat <<EOF_NFT | put /etc/nftables.d/32-vpnkit-vk-turn.nft 644
table inet vpnkit_fw {
  chain input {
    udp dport $VK_PORT accept
  }
}
EOF_NFT
nft -c -f /etc/nftables.conf
systemctl reload nftables
systemctl daemon-reload
systemctl is-enabled -q vk-turn-proxy.service || echo vk-turn-proxy.service >> "$VK_RUN/created.list"
systemctl enable -q vk-turn-proxy.service
if changed /etc/vpnkit/vk-turn.env || changed /usr/local/lib/vpnkit/vk-turn-server || changed /etc/systemd/system/vk-turn-proxy.service; then
  systemctl restart vk-turn-proxy.service
else
  systemctl start vk-turn-proxy.service
fi
