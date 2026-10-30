# vpn-bot: проверки до установки — токен бота оповещений есть, Telegram доступен.
[ -s /etc/vpnkit/alert.env ] || die "бот не настроен: сначала задайте бота оповещений (krot alerts set) и примените мониторинг"
set -a; . /etc/vpnkit/alert.env; set +a
[ -n "${TG_TOKEN:-}" ] && [ -n "${TG_CHATS:-}" ] || die "в /etc/vpnkit/alert.env нет TG_TOKEN или TG_CHATS"
code=$(curl -s -m 15 -o /dev/null -w '%{http_code}' "https://api.telegram.org/bot${TG_TOKEN}/getMe" || true)
[ "$code" = 200 ] || die "api.telegram.org не отвечает 200 (получено ${code:-нет ответа}): проверьте токен и выход к Telegram"
