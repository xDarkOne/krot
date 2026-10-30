# vpn-bot: откат прогона.
for u in vpn-bot.service vpn-stats.timer; do
  if grep -qxF "$u" "$VK_RUN/created.list" 2>/dev/null; then systemctl disable -q --now "$u" 2>/dev/null || true; fi
done
restore_snapshot
systemctl daemon-reload
say "откат vpn-bot выполнен"
