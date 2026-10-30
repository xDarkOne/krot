# split-routing: откат прогона.
systemctl disable -q --now vpnkit-ru-nets.timer vpnkit-ru-domains.timer vpnkit-ru-prune.timer 2>/dev/null || true
restore_snapshot
systemctl daemon-reload
[ -e /etc/nftables.d/25-vpnkit-split.nft ] || nft delete table inet vpnkit_split 2>/dev/null || true
systemctl reload nftables 2>/dev/null || true
if grep -qxF dnsmasq "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now dnsmasq 2>/dev/null || true   # пакет оставляем, службу гасим
else
  systemctl restart dnsmasq 2>/dev/null || true
fi
for t in vpnkit-ru-nets vpnkit-ru-domains vpnkit-ru-prune; do
  [ -e /etc/systemd/system/$t.timer ] && systemctl enable -q --now $t.timer 2>/dev/null || true
done
say "откат split-routing выполнен"
