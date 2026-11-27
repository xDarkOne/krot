systemctl is-active -q vk-turn-proxy || die "VK TURN Proxy не запущен"
[ -n "$(ss -Hlun "sport = :$VK_PORT")" ] || die "VK TURN Proxy не слушает UDP/$VK_PORT"
