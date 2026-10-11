# base-host: проверка. Любое «die» — модуль считается неустановленным, пульт откатывает.
# Вывод команд сначала в переменную: `cmd | grep -q` под pipefail даёт ложный провал
# (grep -q выходит рано, cmd получает SIGPIPE) — поймано 02.10.2026.
: "${VK_ROLE:?}" "${VK_SSH_PORT:=22}"
has() { grep -q -- "$2" <<<"$1"; }
[ "$(sysctl -n net.ipv4.ip_forward)" = 1 ] || die "ip_forward не 1"
[ "$(sysctl -n net.ipv4.tcp_congestion_control)" = bbr ] || die "не bbr"
[ "$VK_ROLE" = exit ] || [ "$(sysctl -n net.ipv4.ip_nonlocal_bind)" = 1 ] || die "ip_nonlocal_bind не 1"
[ "$(awk '/SwapTotal/{print int($2/1024)}' /proc/meminfo)" -ge 500 ] || die "swap меньше 500 МБ"
for s in nftables fail2ban chrony; do systemctl is-active -q "$s" || die "служба $s не работает"; done
fw=$(nft list chain inet vpnkit_fw input 2>&1) || die "нет таблицы inet vpnkit_fw: $fw"
has "$fw" 'policy drop' || die "в input нет policy drop"
has "$fw" "tcp dport $VK_SSH_PORT accept" || die "SSH не разрешён в фаерволе"
sshd_conf=$(sshd -T)
has "$sshd_conf" '^passwordauthentication no$' || die "вход по паролю не отключён (сначала vpnkit bootstrap)"
stop=$(systemctl show nftables -p ExecStop)
has "$stop" flush && die "ExecStop nftables всё ещё flush"
fail2ban-client status sshd >/dev/null || die "jail sshd не работает"
say "проверка base-host: ок"
