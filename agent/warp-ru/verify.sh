# warp-ru: проверка — служба работает, интерфейс поднят. Если WARP сейчас недоступен, это не провал модуля:
# сторож держит российский трафик на прямом выходе и вернёт WARP, когда связь появится.
systemctl is-active -q warp-ru || die "служба warp-ru не работает"
ip link show awgw >/dev/null 2>&1 || die "нет интерфейса awgw"
grep -q 'fwmark 0x1 lookup 201' <<<"$(ip rule show)" || die "нет правила fwmark 0x1 → таблица 201"
if grep -q 'dev awgw' <<<"$(ip route show default table 201)"; then
  t=$(curl -s -m 8 --interface awgw https://www.cloudflare.com/cdn-cgi/trace | grep '^warp=' || true)
  say "WARP работает ($t); российский трафик выходит через Cloudflare"
else
  say "WARP пока не подтвердил связь — российский трафик идёт напрямую, сторож повторит проверку сам"
fi
grep -q '"ru-exit"' /usr/local/etc/xray/config.json || die "в конфиге xray нет outbound ru-exit"
