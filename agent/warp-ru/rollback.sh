# warp-ru: откат прогона — российский трафик снова выходит напрямую.
if [ -x /usr/local/sbin/warp-ru ]; then
  systemctl disable -q --now warp-ru 2>/dev/null || true
  /usr/local/sbin/warp-ru stop 2>/dev/null || true
fi
restore_snapshot
systemctl daemon-reload
[ -x /usr/local/sbin/vpnkit-gen.py ] && /usr/local/sbin/vpnkit-gen.py build >/dev/null 2>&1 && systemctl restart xray 2>/dev/null || true
say "откат warp-ru выполнен"
