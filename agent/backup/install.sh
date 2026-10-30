# backup: ежедневная зашифрованная копия состояния узла (age на открытый ключ пульта).
# Параметры: VK_NODE, VK_RECIPIENT (age1…), VK_TIME (ЧЧ:ММ), VK_KEEP, VK_EXTRA_PATHS, VK_SQLITE.
# Описание — docs/20-server-modules/backup.md
: "${VK_NODE:?}" "${VK_RECIPIENT:?}" "${VK_TIME:=04:30}" "${VK_KEEP:=7}" "${VK_EXTRA_PATHS:=}" "${VK_SQLITE:=}"
case "$VK_RECIPIENT" in age1*) ;; *) die "VK_RECIPIENT не похож на открытый ключ age";; esac
[ -s "$VK_RUN/src/vpnkit-backup.sh" ] || die "нет файла vpnkit-backup.sh от пульта"
apt_install age

put /usr/local/sbin/vpnkit-backup.sh 755 < "$VK_RUN/src/vpnkit-backup.sh"
mkdir -p /etc/vpnkit; chmod 700 /etc/vpnkit
echo "$VK_RECIPIENT" | put /etc/vpnkit/backup.recipients 600
# всё, чем управляет vpnkit; несуществующее скрипт пропускает
paths="/etc/amnezia /etc/wireguard /etc/nftables.conf /etc/nftables.d /etc/dnsmasq.d /etc/systemd/system
/etc/sysctl.d /etc/ssh/sshd_config.d /root/.ssh /etc/fail2ban/jail.d /etc/vpnkit /var/lib/vpnkit/modules
/var/lib/vpnkit/ru-nets.txt /usr/local/sbin /etc/letsencrypt /etc/nginx /usr/local/etc/xray /etc/xray /etc/sing-box
/var/www /etc/network/interfaces /etc/warp-ru /var/lib/warp-ru $VK_EXTRA_PATHS"
cat <<EOF | put /etc/vpnkit/backup.conf 600
# vpnkit backup ($VK_NODE). Пишет пульт; описание полей — /usr/local/sbin/vpnkit-backup.sh
NODE=$VK_NODE
PATHS="$(echo $paths)"
EXCLUDES="*__pycache__* *.log *.log.[0-9]* */venv"
SQLITE="$VK_SQLITE"
KEEP=$VK_KEEP
PEER=""
EOF
printf '[Unit]\nDescription=vpnkit: зашифрованная резервная копия узла\nAfter=network-online.target\n[Service]\nType=oneshot\nNice=10\nIOSchedulingClass=idle\nExecStart=/usr/local/sbin/vpnkit-backup.sh\n' \
  | put /etc/systemd/system/vpnkit-backup.service 644
printf '[Unit]\nDescription=vpnkit: ежедневная резервная копия\n[Timer]\nOnCalendar=*-*-* %s:00\nRandomizedDelaySec=10min\nPersistent=true\n[Install]\nWantedBy=timers.target\n' "$VK_TIME" \
  | put /etc/systemd/system/vpnkit-backup.timer 644
[ -e /etc/systemd/system/timers.target.wants/vpnkit-backup.timer ] || echo vpnkit-backup.timer >> "$VK_RUN/created.list"
systemctl daemon-reload
systemctl enable -q --now vpnkit-backup.timer
systemctl start vpnkit-backup.service
last=$(journalctl -u vpnkit-backup -n 8 --no-pager -o cat | grep -vE '^(Starting|Finished|vpnkit-backup\.service)' | tail -1 || true)
say "${last:-первая копия сделана}"
say "по расписанию: ежедневно около $VK_TIME"
