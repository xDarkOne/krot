# client-awg: откат прогона.
: "${VK_IFACE:?}"
I=$VK_IFACE
: "${VK_WG_TOOL:=awg}" "${VK_CONF_DIR:=/etc/amnezia/amneziawg}"
if grep -qxF "$I" "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now "${VK_WG_TOOL}-quick@$I" 2>/dev/null || true
fi
if grep -qxF dnsmasq "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now dnsmasq 2>/dev/null || true   # пакет оставляем, службу гасим
fi
restore_snapshot
[ -e "/etc/nftables.d/30-vpnkit-client-$I.nft" ] || nft delete table ip "vpnkit_client_$I" 2>/dev/null || true
systemctl reload nftables 2>/dev/null || true
[ -e "$VK_CONF_DIR/$I.conf" ] && systemctl restart "${VK_WG_TOOL}-quick@$I" 2>/dev/null || true
if systemctl is-enabled -q dnsmasq 2>/dev/null; then systemctl restart dnsmasq 2>/dev/null || true; fi
say "откат client-awg ($I) выполнен"
