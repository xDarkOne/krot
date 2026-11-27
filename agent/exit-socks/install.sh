: "${VK_SOCKS_PASSWORD:?}" "${VK_PORT:?}" "${VK_BIND_IP:?}" "${VK_ENTRY_TUN:?}"
# Package postinst may start Dante. Supply our own restricted config first.
wan=$(ip -4 route get 1.1.1.1 | awk '{for(i=1;i<NF;i++) if($i=="dev"){print $(i+1);exit}}')
[[ "$wan" =~ ^[a-zA-Z0-9._-]+$ ]] || die "не найден внешний интерфейс выхода"
if getent passwd krot-socks >/dev/null; then
  grep -qs '^# vpnkit exit-socks' /etc/danted.conf || die "системный пользователь krot-socks уже занят"
else
  useradd -r -M -s /usr/sbin/nologin krot-socks
  echo krot-socks >> "$VK_RUN/created.list"
  printf 'krot-socks:%s\n' "$VK_SOCKS_PASSWORD" | chpasswd
fi
cat <<CONFIG | put /etc/danted.conf 640
# vpnkit exit-socks
logoutput: syslog
internal: $VK_BIND_IP port = $VK_PORT
external: $wan
clientmethod: none
socksmethod: username
user.privileged: root
user.unprivileged: nobody

client pass {
    from: $VK_ENTRY_TUN/32 to: $VK_BIND_IP/32
}
socks block {
    from: 0.0.0.0/0 to: 127.0.0.0/8
}
socks block {
    from: 0.0.0.0/0 to: 10.0.0.0/8
}
socks block {
    from: 0.0.0.0/0 to: 172.16.0.0/12
}
socks block {
    from: 0.0.0.0/0 to: 192.168.0.0/16
}
socks block {
    from: 0.0.0.0/0 to: 169.254.0.0/16
}
socks pass {
    from: $VK_ENTRY_TUN/32 to: 0.0.0.0/0
    command: connect
    user: krot-socks
}
CONFIG
cat <<'OVERRIDE' | put /etc/systemd/system/danted.service.d/10-vpnkit-awg.conf 644
[Unit]
Requires=awg-quick@awg0.service
After=awg-quick@awg0.service
OVERRIDE
systemctl is-enabled -q danted || echo danted.service >> "$VK_RUN/created.list"
apt_install dante-server
systemctl daemon-reload
/usr/sbin/danted -V -f /etc/danted.conf
systemctl enable -q danted
if changed /etc/danted.conf || changed /etc/systemd/system/danted.service.d/; then
  systemctl restart danted
else
  systemctl start danted
fi
