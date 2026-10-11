# backup: откат прогона. Уже сделанные копии в /var/backups/vpnkit не удаляются.
grep -qxF vpnkit-backup.timer "$VK_RUN/created.list" 2>/dev/null && systemctl disable -q --now vpnkit-backup.timer 2>/dev/null || true
restore_snapshot
systemctl daemon-reload
say "откат backup выполнен"
