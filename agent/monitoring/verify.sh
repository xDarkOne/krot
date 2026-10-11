# monitoring: прогон проверок вручную. Провалы проверок — не провал модуля (их покажут
# оповещения), провал — если скрипт не отработал или таймер не включён.
out=$(WATCH_STATE=/run/vpnkit-watch.verify /usr/local/sbin/vpnkit-watch.sh 2>&1) || die "vpnkit-watch упал: $out"
rm -f /run/vpnkit-watch.verify
grep -q '^проверок: ' <<<"$out" || die "vpnkit-watch не дошёл до конца: $out"
while IFS= read -r l; do say "$l"; done <<<"$out"
systemctl is-enabled -q vpnkit-watch.timer || die "таймер не включён"
