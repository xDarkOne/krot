# relocate: проверка — команды на месте и исполняются, настройки читаются.
[ -x /usr/local/sbin/relocate ] && [ -x /usr/local/sbin/vpnkit-reip ] || die "нет команд relocate/vpnkit-reip"
bash -n /usr/local/sbin/relocate && bash -n /usr/local/sbin/vpnkit-reip || die "скрипты переезда повреждены"
[ -s /etc/vpnkit/relocate.conf ] || die "нет /etc/vpnkit/relocate.conf"
# пробный прогон без изменений: любой адрес на замену, ничего не пишется
out=$(/usr/local/sbin/vpnkit-reip --map 192.0.2.1 192.0.2.2) || die "пробный прогон vpnkit-reip не удался"
say "relocate готов (пробный прогон: $(head -1 <<<"$out"))"
