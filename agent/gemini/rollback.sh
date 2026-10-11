# gemini: откат прогона — таймер выключить, конфиг подмены вернуть как был, dnsmasq перезапустить (P-15).
systemctl disable -q --now ai-dns-switch.timer 2>/dev/null || true
restore_snapshot
systemctl daemon-reload
systemctl restart dnsmasq 2>/dev/null || true
say "откат gemini выполнен"
