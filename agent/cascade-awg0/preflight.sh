# Живой awg0, настроенный вручную, требует импорта существующих ключей и маршрутов.
conf=/etc/amnezia/amneziawg/awg0.conf
if [ -e "$conf" ] && ! grep -q '^# vpnkit cascade-awg0 ' "$conf"; then
  die "найден существующий awg0 под внешним управлением; сначала перенесите его конфигурацию"
fi
if [ ! -e "$conf" ] && ip link show awg0 >/dev/null 2>&1; then
  die "активен awg0 без конфигурации Крот"
fi
