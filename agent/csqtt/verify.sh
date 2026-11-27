systemctl is-active -q csqtt || die "CSQTT не запущен"
ip link show "$VK_IFACE" >/dev/null || die "нет TUN CSQTT"
[ -n "$(ss -Hlun "sport = :$VK_PORT")" ] || die "CSQTT не слушает UDP/$VK_PORT"
[ -n "$(ss -Hlt "sport = :$VK_WEB_PORT")" ] || die "нет WEB-панели CSQTT"
