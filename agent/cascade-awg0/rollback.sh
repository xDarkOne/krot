# cascade-awg0: откат прогона.
if grep -qxF awg0 "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now awg-quick@awg0 2>/dev/null || true
fi
restore_snapshot
# своя таблица NAT/MSS: перезагрузка nftables её не удалит (flush ruleset нет) — снимаем явно,
# если файла модуля больше нет
[ -e /etc/nftables.d/20-vpnkit-cascade.nft ] || nft delete table ip vpnkit_cascade 2>/dev/null || true
systemctl reload nftables 2>/dev/null || true
# если конфиг был и вернулся из снимка — поднять туннель с прежними настройками
[ -e /etc/amnezia/amneziawg/awg0.conf ] && systemctl restart awg-quick@awg0 2>/dev/null || true
say "откат cascade-awg0 выполнен"
