# relocate: проверка — команды на месте и исполняются, настройки читаются.
[ -x /usr/local/sbin/relocate ] && [ -x /usr/local/sbin/vpnkit-reip ] && [ -x /usr/local/sbin/vpnkit-dns ] || die "нет команд relocate/vpnkit-reip/vpnkit-dns"
bash -n /usr/local/sbin/relocate && bash -n /usr/local/sbin/vpnkit-reip && python3 -m py_compile /usr/local/sbin/vpnkit-dns || die "скрипты переезда повреждены"
[ -s /etc/vpnkit/relocate.conf ] || die "нет /etc/vpnkit/relocate.conf"
# пробный прогон без изменений: любой адрес на замену, ничего не пишется
out=$(/usr/local/sbin/vpnkit-reip --map 192.0.2.1 192.0.2.2) || die "пробный прогон vpnkit-reip не удался"
say "relocate готов (пробный прогон: $(head -1 <<<"$out"))"
