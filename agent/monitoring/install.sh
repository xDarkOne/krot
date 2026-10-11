# monitoring: vpnkit-watch раз в 5 минут, оповещения в Telegram (если пульт знает токен).
# Параметры: VK_NODE, VK_EXIT_IP, VK_PEER_NAME, VK_PEER_TUN, VK_PEER_PUBLIC, VK_PEER_TCP,
# VK_SERVICES_EXTRA, VK_TG_TOKEN, VK_TG_CHATS (последние два — из секретов пульта).
# Описание — docs/20-server-modules/monitoring.md
: "${VK_NODE:?}" "${VK_EXIT_IP:=}" "${VK_PEER_NAME:=}" "${VK_PEER_TUN:=}" "${VK_PEER_PUBLIC:=}" "${VK_PEER_TCP:=}"
: "${VK_SERVICES_EXTRA:=}" "${VK_TG_TOKEN:=}" "${VK_TG_CHATS:=}"
[ -s "$VK_RUN/src/vpnkit-watch.sh" ] || die "нет файла vpnkit-watch.sh от пульта"
apt_install curl bind9-dnsutils openssl iputils-ping

put /usr/local/sbin/vpnkit-watch.sh 755 < "$VK_RUN/src/vpnkit-watch.sh"
mkdir -p /etc/vpnkit; chmod 700 /etc/vpnkit
cat <<CONF | put /etc/vpnkit/watch.conf 600
# vpnkit-watch ($VK_NODE). Пишет пульт.
NODE=$VK_NODE
EXIT_IP=$VK_EXIT_IP
PEER_NAME=$VK_PEER_NAME
PEER_TUN=$VK_PEER_TUN
PEER_PUBLIC=$VK_PEER_PUBLIC
PEER_TCP="$VK_PEER_TCP"
SERVICES_EXTRA="$VK_SERVICES_EXTRA"
CONF
if [ -n "$VK_TG_TOKEN" ] && [ -n "$VK_TG_CHATS" ]; then
  printf 'TG_TOKEN=%s\nTG_CHATS="%s"\n' "$VK_TG_TOKEN" "$VK_TG_CHATS" | put /etc/vpnkit/alert.env 600
  say "оповещения: Telegram, получателей $(echo $VK_TG_CHATS | wc -w)"
else
  say "оповещения: бот Telegram не настроен — пока только журнал (настроить: «Оповещения» в приложении или vpnkit alerts set)"
fi
printf '[Unit]\nDescription=vpnkit: проверка узла\nAfter=network-online.target\n[Service]\nType=oneshot\nTimeoutStartSec=4min\nExecStart=/usr/local/sbin/vpnkit-watch.sh\n' \
  | put /etc/systemd/system/vpnkit-watch.service 644
printf '[Unit]\nDescription=vpnkit: проверка узла каждые 5 минут\n[Timer]\nOnBootSec=3min\nOnUnitActiveSec=5min\n[Install]\nWantedBy=timers.target\n' \
  | put /etc/systemd/system/vpnkit-watch.timer 644
[ -e /etc/systemd/system/timers.target.wants/vpnkit-watch.timer ] || echo vpnkit-watch.timer >> "$VK_RUN/created.list"
systemctl daemon-reload
systemctl enable -q --now vpnkit-watch.timer
