# vpn-bot (entry/solo): Telegram-бот администратора — состояние узла, клиенты подписки, трафик.
# Использует токен и получателей оповещений (/etc/vpnkit/alert.env). Описание — docs/20-server-modules/vpn-bot.md
for f in vpn-bot.py vpn-stats.py; do [ -s "$VK_RUN/src/$f" ] || die "нет файла $f от пульта"; done
apt_install python3 curl
put /usr/local/sbin/vpn-bot.py 755 < "$VK_RUN/src/vpn-bot.py"
put /usr/local/sbin/vpn-stats.py 755 < "$VK_RUN/src/vpn-stats.py"
mkdir -p /var/lib/vpnkit

cat <<'U' | put /etc/systemd/system/vpn-bot.service 644
[Unit]
Description=Крот: Telegram-бот администратора узла
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/python3 /usr/local/sbin/vpn-bot.py
Restart=always
RestartSec=10
# бот вызывает vpnkit-gen.py (перезапускает xray) и читает конфиги AmneziaWG — нужен root
User=root

[Install]
WantedBy=multi-user.target
U
cat <<'U' | put /etc/systemd/system/vpn-stats.service 644
[Unit]
Description=Крот: сбор статистики трафика клиентов

[Service]
Type=oneshot
ExecStart=/usr/bin/python3 /usr/local/sbin/vpn-stats.py
U
cat <<'U' | put /etc/systemd/system/vpn-stats.timer 644
[Unit]
Description=Крот: сбор статистики каждые 5 минут

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min
Persistent=true

[Install]
WantedBy=timers.target
U
[ -e /etc/systemd/system/multi-user.target.wants/vpn-bot.service ] || echo vpn-bot.service >> "$VK_RUN/created.list"
[ -e /etc/systemd/system/timers.target.wants/vpn-stats.timer ] || echo vpn-stats.timer >> "$VK_RUN/created.list"
systemctl daemon-reload
systemctl enable -q --now vpn-stats.timer
systemctl enable -q vpn-bot.service
if changed /usr/local/sbin/vpn-bot.py || changed /etc/systemd/system/vpn-bot.service || ! systemctl is-active -q vpn-bot; then
  systemctl restart vpn-bot
fi
systemctl start vpn-stats.service || true
