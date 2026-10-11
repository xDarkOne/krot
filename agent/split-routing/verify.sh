# split-routing: проверка — DNS отвечает, российский домен попадает в ru_domains, ru_nets наполнен.
systemctl is-active -q dnsmasq || die "dnsmasq не работает"
nft list set inet vpnkit_split ru_nets >/dev/null 2>&1 || die "нет набора ru_nets"
n=$(nft -j list set inet vpnkit_split ru_nets | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["nftables"][1]["set"].get("elem",[])))')
[ "$n" -gt 5000 ] || die "ru_nets почти пуст ($n) — российский трафик ушёл бы в каскад"
a=$(dig +short +time=3 +tries=2 @127.0.0.1 ya.ru A | grep -E '^[0-9.]+$' | head -1)
[ -n "$a" ] || die "dnsmasq не разрешил ya.ru"
d=$(nft list set inet vpnkit_split ru_domains)
grep -qF "$a" <<<"$d" || die "адрес ya.ru ($a) не попал в ru_domains — nftset не работает"
w=$(dig +short +time=3 +tries=2 @127.0.0.1 github.com A | grep -E '^[0-9.]+$' | head -1)
[ -n "$w" ] || die "dnsmasq не разрешил github.com (зарубежный резолвер через каскад)"
systemctl is-enabled -q vpnkit-ru-nets.timer || die "таймер списка сетей не включён"
say "проверка split-routing: ru_nets $n, ya.ru → $a в ru_domains, github.com → $w"
