# xray: откат прогона. Реестр клиентов, если он был до прогона, остаётся как был.
if [ -f "$VK_RUN/xray.prev" ]; then cp -a "$VK_RUN/xray.prev" /usr/local/bin/xray; fi
if grep -qxF xray "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now xray vpnkit-gen.timer 2>/dev/null || true
fi
grep -qxF xray-bin "$VK_RUN/created.list" 2>/dev/null && rm -f /usr/local/bin/xray
restore_snapshot
systemctl daemon-reload
systemctl reload nftables 2>/dev/null || true
nginx -t -q 2>/dev/null && systemctl reload nginx 2>/dev/null || true
if [ -f /usr/local/etc/xray/config.json ] && systemctl is-enabled -q xray 2>/dev/null; then
  systemctl restart xray 2>/dev/null || true
fi
say "откат xray выполнен"
