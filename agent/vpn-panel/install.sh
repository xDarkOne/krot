# vpn-panel: Telegram Mini App with nginx HTTPS and a local API.
: "${VK_SUB_NAME:?}" "${VK_TIMEZONE:=Europe/Moscow}"
for file in vpn-panel.py panel.html panel.js panel.css; do
  [ -s "$VK_RUN/src/$file" ] || die "нет $file от пульта"
done
put /usr/local/sbin/vpnkit-panel.py 755 < "$VK_RUN/src/vpn-panel.py"
put /var/www/vpnkit-panel/index.html 644 < "$VK_RUN/src/panel.html"
put /var/www/vpnkit-panel/panel.js 644 < "$VK_RUN/src/panel.js"
put /var/www/vpnkit-panel/panel.css 644 < "$VK_RUN/src/panel.css"
printf '%s\n' "https://$VK_SUB_NAME/panel/" | put /etc/vpnkit/panel.url 600
cat <<'UNIT' | put /etc/systemd/system/vpnkit-panel.service 644
[Unit]
Description=Krot Telegram Mini App
After=network.target
[Service]
Type=simple
ExecStart=/usr/bin/python3 /usr/local/sbin/vpnkit-panel.py
Restart=on-failure
RestartSec=3
ProtectHome=yes
ProtectKernelModules=yes
NoNewPrivileges=yes
PrivateTmp=yes
UMask=0077
[Install]
WantedBy=multi-user.target
UNIT
cat <<UNIT | put /etc/systemd/system/vpnkit-panel-reminders.service 644
[Unit]
Description=Krot payment reminders
Wants=network-online.target
After=network-online.target
[Service]
Type=oneshot
Environment=KROT_TIMEZONE=$VK_TIMEZONE
ExecStart=/usr/bin/python3 /usr/local/sbin/vpnkit-panel.py reminders
ProtectHome=yes
NoNewPrivileges=yes
PrivateTmp=yes
UMask=0077
UNIT
cat <<'UNIT' | put /etc/systemd/system/vpnkit-panel-reminders.timer 644
[Unit]
Description=Check Krot payment reminders
[Timer]
OnCalendar=*:0/5
Persistent=yes
[Install]
WantedBy=timers.target
UNIT
cat <<'CONF' | put "/etc/nginx/vpnkit.d/$VK_SUB_NAME-site-panel.conf" 644
location = /panel { return 302 /panel/; }
location ^~ /g/ {
    access_log off;
    error_log /dev/null crit;
    if ($uri !~ "^/g/[0-9a-f]+/incy$") { return 404; }
    proxy_read_timeout 3s;
    proxy_pass http://127.0.0.1:19194;
}
location ^~ /panel/ {
    alias /var/www/vpnkit-panel/;
    index index.html;
    add_header Cache-Control "no-store" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "no-referrer" always;
    add_header Content-Security-Policy "default-src 'self'; script-src 'self' https://telegram.org; object-src 'none'; base-uri 'none'; form-action 'none'" always;
}
location = /panel/api/ {
    access_log off;
    client_max_body_size 4k;
    proxy_read_timeout 165s;
    proxy_set_header X-Telegram-Init-Data $http_x_telegram_init_data;
    proxy_pass http://127.0.0.1:19194;
}
location = /__krot_observe {
    internal;
    access_log off;
    error_log /dev/null crit;
    if ($http_hwid = "") { return 204; }
    proxy_method POST;
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
    proxy_set_header X-Krot-Path $request_uri;
    proxy_set_header X-Krot-Device $http_hwid;
    proxy_connect_timeout 1s;
    proxy_read_timeout 2s;
    rewrite ^ /observe break;
    proxy_pass http://127.0.0.1:19194;
}
CONF
printf 'mirror /__krot_observe;\nmirror_request_body off;\n' |
  put "/etc/nginx/vpnkit.d/$VK_SUB_NAME-subscription-mirror-panel.conf" 644
nginx -t -q
systemctl daemon-reload
for unit in vpnkit-panel.service vpnkit-panel-reminders.timer; do
  systemctl is-enabled -q "$unit" 2>/dev/null || echo "$unit" >> "$VK_RUN/created.list"
done
systemctl enable -q --now vpnkit-panel.service vpnkit-panel-reminders.timer
if changed /usr/local/sbin/vpnkit-panel.py || changed /etc/systemd/system/vpnkit-panel.service; then
  systemctl restart vpnkit-panel.service
fi
systemctl reload nginx
say "Панель доступна из личного чата с ботом"
