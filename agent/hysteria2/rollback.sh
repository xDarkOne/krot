# hysteria2: откат прогона. Пакет sing-box остаётся (гасится служба, если её поставил этот прогон).
grep -qxF sing-box "$VK_RUN/created.list" 2>/dev/null && systemctl disable -q --now sing-box 2>/dev/null || true
restore_snapshot
systemctl daemon-reload
systemctl reload nftables 2>/dev/null || true
[ -x /usr/local/sbin/vpnkit-gen.py ] && /usr/local/sbin/vpnkit-gen.py build >/dev/null 2>&1 || true
systemctl is-enabled -q sing-box 2>/dev/null && systemctl restart sing-box 2>/dev/null || true
say "откат hysteria2 выполнен"
