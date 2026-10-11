# backup: проверка — свежая копия есть, sha256 сходится, таймер включён. Расшифровать узел не может (и не должен).
f=$(ls -1t /var/backups/vpnkit/*.tar.gz.age 2>/dev/null | head -1)
[ -n "$f" ] || die "нет ни одной копии в /var/backups/vpnkit"
age_min=$(( ($(date +%s) - $(stat -c %Y "$f")) / 60 ))
[ "$age_min" -lt 60 ] || die "последняя копия старше часа ($age_min мин)"
[ "$(sha256sum "$f" | awk '{print $1}')" = "$(cat "${f%.tar.gz.age}.sha256")" ] || die "sha256 копии не сходится"
[ "$(stat -c %a "$f")" = 600 ] || die "права копии не 600"
grep -q "age-encryption.org" <<<"$(head -c 40 "$f")" || die "файл не похож на шифр age"
systemctl is-enabled -q vpnkit-backup.timer || die "таймер не включён"
say "проверка backup: $(basename "$f") ($(du -h "$f" | cut -f1)), зашифрована, sha256 ок"
