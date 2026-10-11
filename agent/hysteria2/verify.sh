# hysteria2: конфиг цел, служба жива, порт слушается, в журнале нет ошибок после старта.
: "${VK_HY2_PORT:=443}"
sing-box check -c /etc/sing-box/config.json || die "sing-box check не проходит"
sleep 2
systemctl is-active -q sing-box || die "sing-box не запущен: $(journalctl -u sing-box -n 3 -o cat --no-pager)"
l=$(ss -Hlunp "sport = :$VK_HY2_PORT")
grep -q '"sing-box"' <<<"$l" || die "UDP $VK_HY2_PORT слушает не sing-box"
e=$(journalctl -u sing-box --since "-1min" -o cat --no-pager | grep -ci 'error\|fatal' || true)
[ "$e" = 0 ] || die "в журнале sing-box ошибки: $(journalctl -u sing-box --since -1min -o cat --no-pager | grep -i 'error\|fatal' | tail -2)"
grep -q "udp dport $VK_HY2_PORT accept" <<<"$(nft list chain inet vpnkit_fw input)" || die "UDP $VK_HY2_PORT не открыт"
say "проверка hysteria2: sing-box $(sing-box version | awk 'NR==1{print $3}'), udp/$VK_HY2_PORT, журнал чист"
