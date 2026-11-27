: "${VK_SUB_NAME:?}"
systemctl disable -q --now vpnkit-panel.service vpnkit-panel-reminders.timer 2>/dev/null || true
rm -f /etc/vpnkit/panel.url /usr/local/sbin/vpnkit-panel.py \
  /etc/systemd/system/vpnkit-panel.service /etc/systemd/system/vpnkit-panel-reminders.{service,timer} \
  "/etc/nginx/vpnkit.d/$VK_SUB_NAME-site-panel.conf" \
  "/etc/nginx/vpnkit.d/$VK_SUB_NAME-subscription-mirror-panel.conf" \
  /var/www/vpnkit-panel/{index.html,panel.js,panel.css}
systemctl daemon-reload
nginx -t -q
systemctl reload nginx
