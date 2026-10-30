# vpn-bot: проверка — служба работает не просто «стартовала», а продолжает жить, токен принят Telegram.
sleep 5
systemctl is-active -q vpn-bot || die "служба vpn-bot не работает: $(journalctl -u vpn-bot -n 5 --no-pager -o cat | tail -3)"
systemctl is-enabled -q vpn-stats.timer || die "таймер статистики не включён"
[ -s /var/lib/vpnkit/stats.db ] || die "статистика не собрана (нет /var/lib/vpnkit/stats.db)"
set -a; . /etc/vpnkit/alert.env; set +a
code=$(curl -s -m 15 -o /dev/null -w '%{http_code}' "https://api.telegram.org/bot${TG_TOKEN}/getMe" || true)
[ "$code" = 200 ] || die "Telegram не принял токен (код ${code:-нет})"
say "vpn-bot работает; напишите боту /menu в Telegram"
