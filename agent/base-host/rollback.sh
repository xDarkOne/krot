# base-host: откат прогона. Пакеты не удаляются (безвредны), файлы — из снимка.
restore_snapshot
for sf in /swapfile /swapfile.vpnkit; do
  grep -qxF "$sf" "$VK_RUN/created.list" 2>/dev/null || continue
  swapoff "$sf" 2>/dev/null || true; rm -f "$sf"
done
# fstab восстанавливается из снимка выше (restore_snapshot)
systemctl daemon-reload
# фаервол: снять свою таблицу; если был прежний nftables.conf — он восстановлен снимком
nft delete table inet vpnkit_fw 2>/dev/null || true
sysctl -q --system >/dev/null || true
systemctl restart fail2ban 2>/dev/null || true
systemctl restart systemd-journald 2>/dev/null || true
say "откат base-host выполнен"
