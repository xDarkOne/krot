systemctl is-active -q danted || die "Dante не запущен"
ss -Hlt "sport = :$VK_PORT" | grep -F "$VK_BIND_IP:$VK_PORT" >/dev/null || die "нет сокета Dante в awg0"
