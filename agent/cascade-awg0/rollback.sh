# cascade-awg0: откат прогона.
if grep -qxF awg0 "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now awg-quick@awg0 2>/dev/null || true
fi
# Не оставлять новый watchdog, когда маршруты и файлы уже возвращены.
for unit in vpnkit-cascade-watch.service vpnkit-cascade-tun.service vpnkit-cascade-xray.service; do
  if grep -qxF "$unit" "$VK_RUN/created.list" 2>/dev/null; then
    systemctl disable -q --now "$unit" 2>/dev/null || true
  fi
done
restore_snapshot
systemctl daemon-reload
# своя таблица NAT/MSS: перезагрузка nftables её не удалит (flush ruleset нет) — снимаем явно,
# если файла модуля больше нет
[ -e /etc/nftables.d/20-vpnkit-cascade.nft ] || nft delete table ip vpnkit_cascade 2>/dev/null || true
systemctl reload nftables 2>/dev/null || true
# если конфиг был и вернулся из снимка — поднять туннель с прежними настройками
[ -e /etc/amnezia/amneziawg/awg0.conf ] && systemctl restart awg-quick@awg0 2>/dev/null || true

for unit in vpnkit-cascade-xray.service vpnkit-cascade-tun.service vpnkit-cascade-watch.service; do
  [ -e "/etc/systemd/system/$unit" ] || continue
  if grep -qxF "$unit" "$VK_RUN/disabled.list" 2>/dev/null; then
    systemctl enable -q --now "$unit" || true
  elif systemctl is-active -q "$unit"; then
    systemctl restart "$unit" || true
  fi
done
say "откат cascade-awg0 выполнен"
