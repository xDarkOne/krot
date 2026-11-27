if grep -qxF aivpn-server.service "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now aivpn-server.service 2>/dev/null || true
fi
restore_snapshot
systemctl daemon-reload
[ -e /etc/nftables.d/31-vpnkit-aivpn.nft ] || nft delete table ip vpnkit_aivpn 2>/dev/null || true
systemctl reload nftables 2>/dev/null || true
if systemctl is-active -q aivpn-server; then systemctl restart aivpn-server || true; fi
if changed "/etc/dnsmasq.d/31-vpnkit-$VK_MODULE.conf"; then systemctl restart dnsmasq || true; fi
