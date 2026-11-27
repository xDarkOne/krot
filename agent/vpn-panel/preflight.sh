# This panel uses the same bot token and private Telegram ID as the administrator bot.
: "${VK_SUB_NAME:?}"
[ -s /etc/vpnkit/sub.env ] || die "сначала установите subscriptions"
[ -s /etc/vpnkit/alert.env ] || die "сначала настройте Telegram-бота в оповещениях"
[ -f "/etc/nginx/vpnkit.d/$VK_SUB_NAME-site-sub.conf" ] || die "конфигурация подписок не найдена"
grep -qF 'subscription-mirror-*.conf' "/etc/nginx/vpnkit.d/$VK_SUB_NAME-site-sub.conf" ||
  die "обновите модуль subscriptions перед установкой панели"
. /etc/vpnkit/alert.env
[[ " ${TG_CHATS:-} " =~ [[:space:]]0*[1-9][0-9]*[[:space:]] ]] || die "для Mini App нужен личный Telegram ID администратора"
