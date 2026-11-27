if grep -qxF vk-turn-proxy.service "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now vk-turn-proxy.service 2>/dev/null || true
fi
restore_snapshot
systemctl daemon-reload
systemctl reload nftables 2>/dev/null || true
if systemctl is-active -q vk-turn-proxy; then systemctl restart vk-turn-proxy || true; fi
