for unit in vpnkit-panel-reminders.timer vpnkit-panel.service; do
  if grep -qxF "$unit" "$VK_RUN/created.list" 2>/dev/null; then
    systemctl disable -q --now "$unit" 2>/dev/null || true
  fi
done
restore_snapshot
systemctl daemon-reload
if systemctl is-active -q vpnkit-panel.service; then
  systemctl restart vpnkit-panel.service || true
fi
nginx -t -q && systemctl reload nginx || true
