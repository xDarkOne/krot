if grep -qxF danted.service "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now danted 2>/dev/null || true
fi
restore_snapshot
systemctl daemon-reload
if grep -qxF krot-socks "$VK_RUN/created.list" 2>/dev/null; then
  userdel krot-socks 2>/dev/null || true
fi
if systemctl is-active -q danted; then systemctl restart danted || true; fi
