# mtproxy: откат прогона. 443 возвращается на витрину (генератор решает по mtproxy.env).
# MTProxyL, поставленный этим прогоном, не удаляется (его удаление — интерактивное), а
# останавливается целиком; прежний свой telemt, если его останавливали, поднимается.
if grep -qxF mtproxyl-installed "$VK_RUN/created.list" 2>/dev/null; then
  for u in $(systemctl list-unit-files --no-legend 'mtproxyl*' 2>/dev/null | awk '{print $1}'); do
    systemctl disable -q --now "$u" 2>/dev/null || true
  done
fi
if grep -qxF telemt-stopped "$VK_RUN/created.list" 2>/dev/null; then
  systemctl enable -q telemt 2>/dev/null || true
fi
if grep -qxF telemt-bin "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now telemt 2>/dev/null || true
fi
if [ -f "$VK_RUN/telemt.prev" ]; then cp -a "$VK_RUN/telemt.prev" /usr/local/bin/telemt; fi
restore_snapshot
if grep -qxF telemt-bin "$VK_RUN/created.list" 2>/dev/null; then rm -f /usr/local/bin/telemt; fi
systemctl daemon-reload
systemctl reload nftables 2>/dev/null || true
[ -e /etc/nftables.d/47-vpnkit-mtproxy.nft ] || nft delete table inet vpnkit_mtproxy 2>/dev/null || true
if [ -x /usr/local/sbin/vpnkit-gen.py ]; then /usr/local/sbin/vpnkit-gen.py build >/dev/null 2>&1 || true; fi
if nginx -t -q 2>/dev/null; then systemctl reload nginx 2>/dev/null || true; fi
if systemctl is-enabled -q telemt 2>/dev/null; then systemctl restart telemt 2>/dev/null || true; fi
say "откат mtproxy выполнен"
