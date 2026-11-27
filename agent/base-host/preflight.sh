# Замена базового фаервола на узле с ручным каскадом может остановить его до установки awg0.
conf=/etc/amnezia/amneziawg/awg0.conf
if [ "${VK_ROLE:-}" = entry ] || [ "${VK_ROLE:-}" = exit ]; then
  if { [ -e "$conf" ] || ip link show awg0 >/dev/null 2>&1; } &&
     ! grep -qs '^# vpnkit cascade-awg0 ' "$conf"; then
    die "awg0 управляется вне Крота; для этого узла сначала нужен импорт конфигурации"
  fi
fi
