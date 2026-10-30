# traffic-guard: откат прогона — защита снимается, если её ставил этот прогон.
if grep -qxF traffic-guard.timer "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now traffic-guard.timer 2>/dev/null || true
  systemctl disable -q traffic-guard.service 2>/dev/null || true
  nft delete table inet guard 2>/dev/null || true
fi
restore_snapshot
systemctl daemon-reload
say "откат traffic-guard выполнен"
