# traffic-guard: полное удаление — правила и службы убираются, кэш списков остаётся.
systemctl disable -q --now traffic-guard.timer 2>/dev/null || true
systemctl disable -q traffic-guard.service 2>/dev/null || true
nft delete table inet guard 2>/dev/null || true
rm -f /etc/systemd/system/traffic-guard.service /etc/systemd/system/traffic-guard.timer
systemctl daemon-reload
say "traffic-guard удалён"
