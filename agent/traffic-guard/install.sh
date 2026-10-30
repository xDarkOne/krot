# traffic-guard: блок входящих соединений от сканеров и госсетей (nftables, таблица inet guard).
# Своя таблица — чужие правила не трогаются. Списки обновляются раз в сутки, при сбое остаётся кэш.
# В исключения попадают адреса всех узлов, адрес пульта (откуда идёт эта установка) и VK_EXEMPT.
# Описание — docs/20-server-modules/traffic-guard.md
[ -s "$VK_RUN/src/traffic-guard" ] || die "нет файла traffic-guard от пульта"
apt_install python3 nftables ca-certificates
mkdir -p /var/lib/traffic-guard

put /usr/local/sbin/traffic-guard 755 < "$VK_RUN/src/traffic-guard"

{
  echo "# Адреса, которые traffic-guard не трогает никогда (дополнительно к частным сетям)."
  echo "# Файл пересоздаёт Крот; свои адреса добавляйте в inventory: modules.traffic-guard.exempt"
  for a in $VK_NODES $VK_EXEMPT; do echo "$a"; done
  # пульт: SSH_CONNECTION = «клиент порт сервер порт»; без этого можно запереть себя
  client=$(awk '{print $1}' <<<"${SSH_CONNECTION:-}")
  [ -n "$client" ] && echo "$client # пульт"
} | put /etc/traffic-guard.exempt 644

cat <<'U' | put /etc/systemd/system/traffic-guard.service 644
[Unit]
Description=Блок сканеров и госсетей на входе (nftables, таблица inet guard)
Wants=network-online.target
After=network-online.target nftables.service

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/traffic-guard apply

[Install]
WantedBy=multi-user.target
U
cat <<'U' | put /etc/systemd/system/traffic-guard.timer 644
[Unit]
Description=Ежедневное обновление списков traffic-guard

[Timer]
OnCalendar=*-*-* 04:20:00
RandomizedDelaySec=30min
Persistent=true

[Install]
WantedBy=timers.target
U
[ -e /etc/systemd/system/multi-user.target.wants/traffic-guard.service ] || echo traffic-guard.service >> "$VK_RUN/created.list"
[ -e /etc/systemd/system/timers.target.wants/traffic-guard.timer ] || echo traffic-guard.timer >> "$VK_RUN/created.list"
systemctl daemon-reload
systemctl enable -q traffic-guard.service
systemctl enable -q --now traffic-guard.timer
/usr/local/sbin/traffic-guard apply || die "traffic-guard не применил правила (списки недоступны или nft отказал)"
