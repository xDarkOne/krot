systemctl is-active -q aivpn-server || die "AIVPN не запущен"
ip link show "$VK_IFACE" >/dev/null || die "нет TUN AIVPN"
[ -s /etc/aivpn/server.key ] || die "нет ключа AIVPN"
[ -n "$(ss -Hlun "sport = :$VK_PORT")" ] || die "AIVPN не слушает UDP/$VK_PORT"
