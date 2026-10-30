# warp-ru: проверки до установки — каскад поднят, модуль ядра AmneziaWG есть, split-routing и xray стоят.
ip link show awg0 >/dev/null 2>&1 || die "нет интерфейса awg0 — сначала cascade-awg0"
modinfo amneziawg >/dev/null 2>&1 || die "нет модуля ядра amneziawg — сначала amneziawg"
[ -d /etc/nftables.d ] && nft list table inet vpnkit_split >/dev/null 2>&1 || die "нет split-routing"
[ -s /etc/vpnkit/xray.env ] || die "нет модуля xray"
grep -q 'inet ' <<<"$(ip -4 -o addr show dev awg0)" || die "у awg0 нет адреса"
curl -s -m 8 --interface "$(ip -4 -o addr show dev awg0 | awk '{sub("/.*","",$4); print $4; exit}')" -o /dev/null https://api.cloudflareclient.com/ \
  || die "через каскад не открывается api.cloudflareclient.com — аккаунт WARP завести не получится"
