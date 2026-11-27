: "${VK_BACKEND_PORT:?}" "${VK_IFACE:?}"
systemctl is-active -q "wg-quick@$VK_IFACE" || die "сначала установите клиентский WireGuard"
if systemctl is-active -q vk-turn-proxy; then
  [[ "$(systemctl show -p Description --value vk-turn-proxy)" == Krot* ]] ||
    die "обнаружен VK TURN Proxy вне управления Крота"
fi
