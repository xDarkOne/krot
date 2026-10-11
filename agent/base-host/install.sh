# base-host: основа сервера. Параметры: VK_ROLE, VK_SSH_PORT, VK_TIMEZONE, VK_SWAP_MB.
# Описание — docs/20-server-modules/base-host.md
: "${VK_ROLE:?}" "${VK_SSH_PORT:=22}" "${VK_TIMEZONE:=Europe/Moscow}" "${VK_SWAP_MB:=2048}"

apt_install nftables curl ca-certificates jq python3 chrony fail2ban unattended-upgrades needrestart logrotate age

# --- время
if [ "$(timedatectl show -p Timezone --value 2>/dev/null)" != "$VK_TIMEZONE" ]; then
  snapshot /etc/timezone /etc/localtime
  timedatectl set-timezone "$VK_TIMEZONE"
  say "часовой пояс $VK_TIMEZONE"
fi

# --- sysctl
{
  echo "# vpnkit base-host"
  echo "net.ipv4.ip_forward=1"
  echo "net.ipv4.conf.all.rp_filter=2"
  echo "net.ipv4.conf.default.rp_filter=2"
  echo "net.core.default_qdisc=fq"
  echo "net.ipv4.tcp_congestion_control=bbr"
  echo "vm.swappiness=10"
  echo "vm.vfs_cache_pressure=50"
  # вход (entry или один сервер): службы слушают адреса туннелей, которые появляются позже (P-05)
  [ "$VK_ROLE" != exit ] && echo "net.ipv4.ip_nonlocal_bind=1"
  true
} | put /etc/sysctl.d/99-vpnkit.conf 644
sysctl -q --system >/dev/null
say "sysctl применён"

# --- swap: если всего меньше 1 ГБ — добавить файл до VK_SWAP_MB.
# Свой swap хостера (часто /swapfile на 512 МБ) не трогаем: добавляем второй файл.
swap_mb=$(awk '/SwapTotal/{print int($2/1024)}' /proc/meminfo)
if [ "$swap_mb" -lt 1000 ] && [ "$VK_SWAP_MB" -gt 0 ]; then
  sf=/swapfile; [ -e "$sf" ] && sf=/swapfile.vpnkit
  if [ ! -e "$sf" ]; then
    need=$(( VK_SWAP_MB - swap_mb )); [ "$need" -lt 1024 ] && need=1024
    free_mb=$(df -Pm / | awk 'NR==2{print $4}')
    [ "$free_mb" -gt $((need + 3000)) ] || die "мало места для swap ($free_mb МБ)"
    fallocate -l "${need}M" "$sf"
    chmod 600 "$sf"; mkswap -q "$sf" >/dev/null; swapon "$sf"
    echo "$sf" >> "$VK_RUN/created.list"
    grep -q "^$sf " /etc/fstab || { snapshot /etc/fstab; echo "$sf none swap sw 0 0" >> /etc/fstab; }
    say "swap: добавлен $sf на ${need} МБ (было $swap_mb МБ)"
  fi
fi

# --- журнал не больше 200 МБ
printf '# vpnkit: журнал не больше 200 МБ\n[Journal]\nSystemMaxUse=200M\n' | put /etc/systemd/journald.conf.d/50-vpnkit.conf 644
grep -qxF /etc/systemd/journald.conf.d/50-vpnkit.conf "$VK_RUN/changed.list" 2>/dev/null && systemctl restart systemd-journald

# --- fail2ban: sshd через journald, бан в nftables
cat <<EOF | put /etc/fail2ban/jail.d/vpnkit.conf 644
# vpnkit base-host
[DEFAULT]
banaction = nftables
banaction_allports = nftables[type=allports]
[sshd]
enabled = true
backend = systemd
port = $VK_SSH_PORT
EOF
systemctl enable -q fail2ban; systemctl restart fail2ban

# --- автообновления безопасности (без автоперезагрузки: перезагрузка — через vpnkit reboot)
printf 'APT::Periodic::Update-Package-Lists "1";\nAPT::Periodic::Unattended-Upgrade "1";\n' | put /etc/apt/apt.conf.d/20auto-upgrades 644
systemctl enable -q --now chrony 2>/dev/null || true

# --- фаервол: свой каркас без "flush ruleset" (P-01)
cat <<EOF | put /etc/nftables.d/10-vpnkit-fw.nft 644
# vpnkit base-host: каркас фаервола. Таблица своя; чужие (Docker, fail2ban) не трогаются.
table inet vpnkit_fw {
}
flush table inet vpnkit_fw
table inet vpnkit_fw {
	chain input {
		type filter hook input priority filter; policy drop;
		ct state established,related accept
		ct state invalid drop
		iif "lo" accept
		ip protocol icmp accept
		ip6 nexthdr ipv6-icmp accept
		tcp dport $VK_SSH_PORT accept
	}
	chain forward {
		type filter hook forward priority filter; policy drop;
		ct state established,related accept
	}
	chain output {
		type filter hook output priority filter; policy accept;
	}
}
EOF
cat <<'EOF' | put /etc/nftables.conf 755
#!/usr/sbin/nft -f
# vpnkit: без "flush ruleset" — каждый файл в nftables.d очищает только свою таблицу.
include "/etc/nftables.d/*.nft"
EOF
printf '# vpnkit: остановка nftables не стирает чужие таблицы\n[Service]\nExecStop=\n' | put /etc/systemd/system/nftables.service.d/10-vpnkit-no-flush.conf 644
systemctl daemon-reload
nft -c -f /etc/nftables.conf
systemctl enable -q nftables
systemctl restart nftables   # ExecStop пустой: restart = повторное применение файла, чужое не трогает
say "фаервол: inet vpnkit_fw (вход: established, lo, icmp, tcp/$VK_SSH_PORT)"
