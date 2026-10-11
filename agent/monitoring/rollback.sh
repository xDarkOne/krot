# monitoring: откат прогона.
grep -qxF vpnkit-watch.timer "$VK_RUN/created.list" 2>/dev/null && systemctl disable -q --now vpnkit-watch.timer 2>/dev/null || true
restore_snapshot
systemctl daemon-reload
say "откат monitoring выполнен"
