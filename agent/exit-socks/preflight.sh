: "${VK_BIND_IP:?}" "${VK_ENTRY_TUN:?}" "${VK_PORT:?}"
if systemctl is-active -q danted; then
  grep -qs '^# vpnkit exit-socks' /etc/danted.conf ||
    die "обнаружен Dante вне управления Крота"
fi
