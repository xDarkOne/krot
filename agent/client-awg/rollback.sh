# client-awg: откат прогона.
: "${VK_IFACE:?}"
I=$VK_IFACE
if grep -qxF "$I" "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now "awg-quick@$I" 2>/dev/null || true
fi
if grep -qxF dnsmasq "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now dnsmasq 2>/dev/null || true   # пакет оставляем, службу гасим
fi
restore_snapshot
[ -e "/etc/nftables.d/30-vpnkit-client-$I.nft" ] || nft delete table ip "vpnkit_client_$I" 2>/dev/null || true
systemctl reload nftables 2>/dev/null || true
[ -e "/etc/amnezia/amneziawg/$I.conf" ] && systemctl restart "awg-quick@$I" 2>/dev/null || true
if systemctl is-enabled -q dnsmasq 2>/dev/null; then systemctl restart dnsmasq 2>/dev/null || true; fi
say "откат client-awg ($I) выполнен"
