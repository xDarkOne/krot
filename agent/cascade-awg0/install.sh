# cascade-awg0: туннель AmneziaWG между узлами. VK_SIDE=server (exit) | client (entry).
# Ключи, общие секреты, порт и обфускацию готовит пульт (одни и те же на обоих концах).
# Описание — docs/20-server-modules/cascade-awg0.md
: "${VK_SIDE:?}" "${VK_ADDR:?}" "${VK_TUN_NET:?}" "${VK_MTU:?}" "${VK_PORT:?}" "${VK_PRIVATE:?}" \
  "${VK_PEER_PUBLIC:?}" "${VK_PSK:?}" "${VK_HPK:?}" "${VK_OBFS:?}"
command -v awg-quick >/dev/null || die "нет awg-quick — сначала модуль amneziawg"
conf=/etc/amnezia/amneziawg/awg0.conf
tunip=${VK_ADDR%/*}
mkdir -p /etc/amnezia/amneziawg; chmod 700 /etc/amnezia/amneziawg
[ -e "$conf" ] || echo "awg0" >> "$VK_RUN/created.list"

{
  echo "# vpnkit cascade-awg0 ($VK_SIDE). Сгенерировано пультом vpnkit — руками не править."
  echo "[Interface]"
  echo "Address = $VK_ADDR"
  echo "Table = off"
  echo "MTU = $VK_MTU"
  [ "$VK_SIDE" = server ] && echo "ListenPort = $VK_PORT"
  echo "PrivateKey = $VK_PRIVATE"
  printf '%s\n' "$VK_OBFS"
  echo "HeaderProtectionKey = $VK_HPK"
  echo "ContentPaddingAddition = 0-16"
  if [ "$VK_SIDE" = client ]; then
    # всё, что entry отправляет в каскад, уходит через таблицу 200 (формат строк читает cascade-watch)
    echo "PostUp = ip rule add from $tunip table 200 priority 200"
    echo "PostUp = ip rule add fwmark 0x3 table 200 priority 207"
    echo "PostUp = ip route replace default dev awg0 table 200"
    for p in ${VK_TG_PREFIXES:-}; do echo "PostUp = ip route replace $p dev awg0"; done
    echo "PostDown = ip rule del from $tunip table 200 priority 200 || true"
    echo "PostDown = ip rule del fwmark 0x3 table 200 priority 207 || true"
    echo "PostDown = ip route del default dev awg0 table 200 || true"
  fi
  echo
  echo "[Peer]"
  echo "PublicKey = $VK_PEER_PUBLIC"
  echo "PresharedKey = $VK_PSK"
  echo "AdvancedSecurity = on"
  echo "AllowedIPs = 0.0.0.0/0"
  if [ "$VK_SIDE" = client ]; then
    echo "Endpoint = ${VK_ENDPOINT:?}"
    echo "PersistentKeepalive = 25"
  fi
} | put "$conf" 600
awg-quick strip awg0 >/dev/null 2>"$VK_RUN/strip.err" || die "awg-quick не разобрал конфиг: $(cat "$VK_RUN/strip.err")"

# --- фаервол: правила в общей таблице vpnkit_fw (разрешение должно лежать в той же
# цепочке, где policy drop — P-03) + своя таблица для NAT и MSS.
if [ "$VK_SIDE" = server ]; then
  fw_in="		udp dport $VK_PORT accept
		iifname \"awg0\" accept"
  fw_fwd='		iifname "awg0" oifname != "awg0" accept'
  nat="		ip saddr $VK_TUN_NET oifname != \"awg0\" masquerade"
else
  fw_in='		iifname "awg0" accept'
  fw_fwd=''
  nat='		meta mark 0x00000003 oifname "awg0" masquerade'
fi
cat <<EOF | put /etc/nftables.d/20-vpnkit-cascade.nft 644
# vpnkit cascade-awg0 ($VK_SIDE). Применяется вместе со всем /etc/nftables.conf:
# базовый файл 10-* очищает inet vpnkit_fw, поэтому правила ниже не задваиваются.
table inet vpnkit_fw {
	chain input {
$fw_in
	}
	chain forward {
$fw_fwd
	}
}
table ip vpnkit_cascade {
}
flush table ip vpnkit_cascade
table ip vpnkit_cascade {
	chain postrouting {
		type nat hook postrouting priority srcnat; policy accept;
$nat
	}
	chain mss {
		type filter hook forward priority mangle; policy accept;
		iifname "awg0" tcp flags syn tcp option maxseg size set rt mtu
		oifname "awg0" tcp flags syn tcp option maxseg size set rt mtu
	}
}
EOF
nft -c -f /etc/nftables.conf
systemctl reload nftables
say "фаервол: $( [ "$VK_SIDE" = server ] && echo "udp/$VK_PORT открыт, NAT $VK_TUN_NET" || echo "NAT меченого (0x3) в awg0")"

# --- туннель
systemctl enable -q awg-quick@awg0
if grep -qxF "$conf" "$VK_RUN/changed.list" 2>/dev/null; then
  systemctl restart awg-quick@awg0
else
  systemctl start awg-quick@awg0
fi
say "туннель awg0 ($VK_SIDE): $(ip -br addr show awg0 | awk '{print $3}')"
