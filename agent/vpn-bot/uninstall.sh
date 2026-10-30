# vpn-bot: удаление. База статистики остаётся в /var/lib/vpnkit/stats.db.
systemctl disable -q --now vpn-bot.service vpn-stats.timer 2>/dev/null || true
rm -f /etc/systemd/system/vpn-bot.service /etc/systemd/system/vpn-stats.service /etc/systemd/system/vpn-stats.timer \
  /usr/local/sbin/vpn-bot.py /usr/local/sbin/vpn-stats.py
systemctl daemon-reload
say "vpn-bot удалён"
