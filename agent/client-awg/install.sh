# client-awg (entry): клиентский вход AmneziaWG. Параметры готовит пульт.
# VK_IFACE (awg2), VK_NET (10.9.2.0/24), VK_GW (10.9.2.1), VK_PORT, VK_MTU, VK_MSS, VK_PRIVATE, VK_HPK, VK_OBFS,
# VK_RULE_MAIN / VK_RULE_CASCADE — приоритеты ip rule. VK_CASCADE: 1 — вход каскада (российское
# напрямую, остальное в awg0), 0 — один сервер (всё напрямую). Описание — docs/20-server-modules/client-awg.md
: "${VK_IFACE:?}" "${VK_NET:?}" "${VK_GW:?}" "${VK_PORT:?}" "${VK_MTU:?}" "${VK_MSS:?}" "${VK_PRIVATE:?}" \
  "${VK_HPK:?}" "${VK_OBFS:?}" "${VK_RULE_MAIN:?}" "${VK_RULE_CASCADE:?}" "${VK_CASCADE:=1}"
command -v awg-quick >/dev/null || die "нет awg-quick — сначала модуль amneziawg"
if [ "$VK_CASCADE" = 1 ]; then
  nft list table inet vpnkit_split >/dev/null 2>&1 || die "нет split-routing — сначала модуль split-routing"
  ip link show awg0 >/dev/null 2>&1 || die "нет каскада awg0 — сначала cascade-awg0"
fi
I=$VK_IFACE
conf=/etc/amnezia/amneziawg/$I.conf
pfx=${VK_NET#*/}
[ -e "$conf" ] || echo "$I" >> "$VK_RUN/created.list"

# секция [Interface] — от пульта; пиры (клиенты) переносятся из текущего конфига как есть
iface=$(
  echo "# vpnkit client-awg $I. [Interface] пишет пульт; пиры ниже ведёт vpnkit clients — руками не править."
  echo "[Interface]"
  echo "Address = $VK_GW/$pfx"
  echo "ListenPort = $VK_PORT"
  echo "MTU = $VK_MTU"
  echo "Table = off"
  echo "PrivateKey = $VK_PRIVATE"
  printf '%s\n' "$VK_OBFS"
  echo "HeaderProtectionKey = $VK_HPK"
  echo "ContentPaddingAddition = 0-16"
  if [ "$VK_CASCADE" = 1 ]; then
    echo "PostUp = ip rule add from $VK_NET fwmark 0x1 table main priority $VK_RULE_MAIN"
    echo "PostUp = ip rule add from $VK_NET table 200 priority $VK_RULE_CASCADE"
    echo "PostUp = ip route replace $VK_NET dev $I table 200"
    echo "PostDown = ip rule del from $VK_NET fwmark 0x1 table main priority $VK_RULE_MAIN || true"
    echo "PostDown = ip rule del from $VK_NET table 200 priority $VK_RULE_CASCADE || true"
  fi
)
peers=""
old_iface=""
if [ -f "$conf" ]; then
  peers=$(awk 'f||/^# name:|^\[Peer\]/{f=1; print}' "$conf")
  old_iface=$(awk '/^# name:|^\[Peer\]/{exit} {print}' "$conf")
fi
mkdir -p /etc/amnezia/amneziawg; chmod 700 /etc/amnezia/amneziawg
{ printf '%s\n' "$iface"; [ -n "$peers" ] && { echo; printf '%s\n' "$peers"; }; true; } | put "$conf" 600
awg-quick strip "$I" >/dev/null 2>"$VK_RUN/strip.err" || die "awg-quick не разобрал конфиг: $(cat "$VK_RUN/strip.err")"
[ -n "$peers" ] && say "клиентов в конфиге сохранено: $(grep -c '^\[Peer\]' <<<"$peers")"

# --- фаервол и разделение трафика (в каскаде российское метится 0x1 и идёт мимо awg0)
split=""
if [ "$VK_CASCADE" = 1 ]; then
  split="table inet vpnkit_split {
	chain prerouting {
		iifname \"$I\" ip daddr @ru_nets meta mark set 0x00000001
		iifname \"$I\" ip daddr @ru_domains meta mark set 0x00000001
	}
}"
fi
cat <<EOF | put /etc/nftables.d/30-vpnkit-client-$I.nft 644
# vpnkit client-awg $I
table inet vpnkit_fw {
	chain input {
		udp dport $VK_PORT accept
		iifname "$I" accept
	}
	chain forward {
		iifname "$I" accept
	}
}
$split
table ip vpnkit_client_$I {
}
flush table ip vpnkit_client_$I
table ip vpnkit_client_$I {
	chain postrouting {
		type nat hook postrouting priority srcnat; policy accept;
		ip saddr $VK_NET oifname != "$I" masquerade
	}
	chain mss {
		type filter hook forward priority mangle; policy accept;
		iifname "$I" tcp flags & (syn | rst) == syn tcp option maxseg size > $VK_MSS tcp option maxseg size set $VK_MSS
		oifname "$I" tcp flags & (syn | rst) == syn tcp option maxseg size > $VK_MSS tcp option maxseg size set $VK_MSS
	}
}
EOF
nft -c -f /etc/nftables.conf
systemctl reload nftables

# --- DNS для клиентов этого входа. В каскаде dnsmasq с разделением ставит split-routing;
# на одном сервере — свой простой: слушает только петлю и шлюзы входов, наружу — 1.1.1.1 и 8.8.8.8.
if [ "$VK_CASCADE" != 1 ] && [ ! -f /etc/dnsmasq.d/10-vpnkit-split.conf ]; then
  printf '%s\n' "# vpnkit client-awg: DNS для клиентов (один сервер). Входы добавляют listen-address в 30-vpnkit-*.conf" \
    bind-dynamic listen-address=127.0.0.1 no-resolv no-hosts cache-size=10000 dns-forward-max=300 \
    server=1.1.1.1 server=8.8.8.8 | put /etc/dnsmasq.d/10-vpnkit-dns.conf 644
  apt_install dnsmasq bind9-dnsutils
  if grep -qxF dnsmasq "$VK_RUN/packages.installed" 2>/dev/null; then echo dnsmasq >> "$VK_RUN/created.list"; fi
  systemctl enable -q dnsmasq
fi
printf '# vpnkit client-awg %s\nlisten-address=%s\n' "$I" "$VK_GW" | put "/etc/dnsmasq.d/30-vpnkit-$I.conf" 644

# --- туннель: перезапуск только если изменилась секция [Interface] (клиенты переподключатся сами)
systemctl enable -q "awg-quick@$I"
if [ "$old_iface" != "$iface" ] || ! systemctl is-active -q "awg-quick@$I"; then
  systemctl restart "awg-quick@$I"
  say "вход $I поднят/перезапущен"
fi
if changed /etc/dnsmasq.d/ || ! systemctl is-active -q dnsmasq; then systemctl restart dnsmasq; fi
say "вход $I: $VK_GW/$pfx, udp/$VK_PORT, MTU $VK_MTU, MSS $VK_MSS"
