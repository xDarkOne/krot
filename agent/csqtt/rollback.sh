if grep -qxF csqtt.service "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now csqtt.service 2>/dev/null || true
fi
restore_snapshot
systemctl daemon-reload
[ -e /etc/nftables.d/31-vpnkit-csqtt.nft ] || nft delete table ip vpnkit_csqtt 2>/dev/null || true
systemctl reload nftables 2>/dev/null || true
if systemctl is-active -q csqtt; then systemctl restart csqtt || true; fi
if changed "/etc/dnsmasq.d/31-vpnkit-$VK_MODULE.conf"; then systemctl restart dnsmasq || true; fi
