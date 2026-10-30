# split-routing (entry): российское напрямую, остальное в каскад.
# Наборы inet vpnkit_split (ru_nets, ru_domains), dnsmasq с разделением DNS, обновление списка сетей.
# Клиентские модули добавляют в цепочку prerouting свои правила «iifname X daddr ∈ наборы → mark 0x1».
# Описание — docs/20-server-modules/split-routing.md
: "${VK_RU_RESOLVER:=77.88.8.8}"
# российские домены в чужих зонах (проверенный список; mail.com — американский GMX, исключён)
RU_EXTRA="alfabank.com avito.st dzeninfra.ru gosuslugi.com imgsmail.ru mradx.net mycdn.me ozon.com ozonru.net
rutube.io sber.com sberbank.com tbank.com tinkoff.com userapi.com vk-portal.net vk.com vkuser.net vtb.com
wildberries.com ya.cc yandex.com yandex.net yastatic.net"

# --- dnsmasq: конфиг ДО пакета. На Ubuntu 24.04 127.0.0.53:53 держит systemd-resolved:
# без bind-dynamic и явных адресов dnsmasq падает прямо при установке пакета.
{
  echo "# vpnkit split-routing. Клиентские входы добавляют свои listen-address в 30-vpnkit-*.conf"
  echo "bind-dynamic"
  echo "listen-address=127.0.0.1"
  echo "no-resolv"
  echo "no-hosts"
  echo "cache-size=10000"
  echo "dns-forward-max=300"
  echo "filter-AAAA"
  echo "server=1.1.1.1"
  echo "server=8.8.8.8"
  for z in ru su xn--p1ai $RU_EXTRA; do
    echo "server=/$z/$VK_RU_RESOLVER"
    echo "nftset=/$z/4#inet#vpnkit_split#ru_domains"
  done
} | put /etc/dnsmasq.d/10-vpnkit-split.conf 644
apt_install dnsmasq bind9-dnsutils
if grep -qxF dnsmasq "$VK_RUN/packages.installed" 2>/dev/null; then echo dnsmasq >> "$VK_RUN/created.list"; fi

# --- nftables: наборы и цепочки. "flush table" при перезагрузке правил не трогает содержимое наборов.
cat <<'EOF' | put /etc/nftables.d/25-vpnkit-split.nft 644
# vpnkit split-routing: российские адреса → метка 0x1 → ip rule … lookup main → напрямую.
# Правила по интерфейсам добавляют клиентские модули (30-vpnkit-client-*.nft).
table inet vpnkit_split {
}
flush table inet vpnkit_split
table inet vpnkit_split {
	set ru_nets {
		type ipv4_addr
		flags interval
		auto-merge
		elements = { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16 }
	}
	set ru_domains {
		type ipv4_addr
		flags timeout
		timeout 1d
	}
	chain prerouting {
		type filter hook prerouting priority mangle; policy accept;
	}
	# собственный DNS узла к зарубежным резолверам — через каскад (метка 0x3 → таблица 200)
	chain dnsout {
		type route hook output priority mangle; policy accept;
		ip daddr { 1.0.0.1, 1.1.1.1, 8.8.4.4, 8.8.8.8 } udp dport 53 meta mark set 0x00000003
		ip daddr { 1.0.0.1, 1.1.1.1, 8.8.4.4, 8.8.8.8 } tcp dport 53 meta mark set 0x00000003
	}
}
EOF
nft -c -f /etc/nftables.conf
systemctl reload nftables

# --- список российских сетей: загрузка + кэш (если источники недоступны — из кэша)
cat <<'EOF' | put /usr/local/sbin/vpnkit-ru-nets.sh 755
#!/bin/bash
# vpnkit: наполняет inet vpnkit_split ru_nets. Меньше 5000 строк — источник считается сломанным,
# тогда берётся кэш; без кэша набор не трогается (иначе весь трафик уйдёт в каскад).
set -u
CACHE=/var/lib/vpnkit/ru-nets.txt
TMP=$(mktemp); trap 'rm -f "$TMP"' EXIT
for u in https://www.ipdeny.com/ipblocks/data/aggregated/ru-aggregated.zone \
         https://raw.githubusercontent.com/ipverse/rir-ip/master/country/ru/ipv4-aggregated.txt; do
  curl -sf --max-time 40 "$u" 2>/dev/null | grep -E '^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$' > "$TMP"
  [ "$(wc -l < "$TMP")" -gt 5000 ] && { mkdir -p /var/lib/vpnkit; cp "$TMP" "$CACHE"; src=$u; break; }
done
if [ "$(wc -l < "$TMP")" -le 5000 ]; then
  [ -s "$CACHE" ] || { logger -t vpnkit-ru-nets "источники недоступны, кэша нет — набор не изменён"; exit 0; }
  cp "$CACHE" "$TMP"; src="кэш $CACHE"
fi
printf '10.0.0.0/8\n172.16.0.0/12\n192.168.0.0/16\n127.0.0.0/8\n' >> "$TMP"
{ echo "flush set inet vpnkit_split ru_nets"; echo "add element inet vpnkit_split ru_nets {"; paste -sd, "$TMP"; echo "}"; } | nft -f - \
  || { logger -t vpnkit-ru-nets "nft отверг список"; exit 1; }
logger -t vpnkit-ru-nets "загружено $(wc -l < "$TMP") префиксов ($src)"
echo "загружено $(wc -l < "$TMP") префиксов ($src)"
EOF
printf '[Unit]\nDescription=vpnkit: список российских сетей для split-routing\nAfter=network-online.target nftables.service\nWants=network-online.target\n[Service]\nType=oneshot\nExecStart=/usr/local/sbin/vpnkit-ru-nets.sh\n' \
  | put /etc/systemd/system/vpnkit-ru-nets.service 644
printf '[Unit]\nDescription=vpnkit: обновление списка российских сетей\n[Timer]\nOnBootSec=1min\nOnUnitActiveSec=24h\nRandomizedDelaySec=1h\n[Install]\nWantedBy=timers.target\n' \
  | put /etc/systemd/system/vpnkit-ru-nets.timer 644
systemctl daemon-reload
systemctl enable -q --now vpnkit-ru-nets.timer
say "ru_nets: $(/usr/local/sbin/vpnkit-ru-nets.sh)"

# --- российские домены из v2fly (раз в сутки) и чистка адресов общих облаков/CDN (каждые 10 минут)
for f in vpnkit-ru-domains.sh vpnkit-ru-prune.py; do [ -s "$VK_RUN/src/$f" ] || die "нет файла $f от пульта"; done
put /usr/local/sbin/vpnkit-ru-domains.sh 755 < "$VK_RUN/src/vpnkit-ru-domains.sh"
put /usr/local/sbin/vpnkit-ru-prune.py 755 < "$VK_RUN/src/vpnkit-ru-prune.py"
printf 'RU_RESOLVER=%s\n' "$VK_RU_RESOLVER" | put /etc/vpnkit/split.conf 644
printf '[Unit]\nDescription=vpnkit: российские домены из v2fly\nAfter=network-online.target nftables.service\nWants=network-online.target\n[Service]\nType=oneshot\nExecStart=/usr/local/sbin/vpnkit-ru-domains.sh\n' \
  | put /etc/systemd/system/vpnkit-ru-domains.service 644
printf '[Unit]\nDescription=vpnkit: ежедневное обновление российских доменов\n[Timer]\nOnCalendar=daily\nRandomizedDelaySec=2h\nPersistent=true\n[Install]\nWantedBy=timers.target\n' \
  | put /etc/systemd/system/vpnkit-ru-domains.timer 644
printf '[Unit]\nDescription=vpnkit: чистка ru_domains от адресов облаков\nAfter=nftables.service\n[Service]\nType=oneshot\nExecStart=/usr/local/sbin/vpnkit-ru-prune.py\n' \
  | put /etc/systemd/system/vpnkit-ru-prune.service 644
printf '[Unit]\nDescription=vpnkit: регулярная чистка ru_domains\n[Timer]\nOnBootSec=5min\nOnUnitActiveSec=10min\nPersistent=true\n[Install]\nWantedBy=timers.target\n' \
  | put /etc/systemd/system/vpnkit-ru-prune.timer 644
apt_install python3 curl
snapshot /etc/dnsmasq.d/20-vpnkit-v2fly.conf
systemctl daemon-reload
systemctl enable -q --now vpnkit-ru-domains.timer vpnkit-ru-prune.timer
for t in vpnkit-ru-domains.timer vpnkit-ru-prune.timer; do echo $t >> "$VK_RUN/created.list"; done

systemctl enable -q dnsmasq
# не reload: dnsmasq по SIGHUP не перечитывает конфиг (P-15); и только если конфиг менялся
if changed /etc/dnsmasq.d/ || ! systemctl is-active -q dnsmasq; then systemctl restart dnsmasq; say "dnsmasq перезапущен"; fi
say "dnsmasq: 127.0.0.1, российские зоны → $VK_RU_RESOLVER с пометкой в ru_domains"
/usr/local/sbin/vpnkit-ru-domains.sh >/dev/null 2>&1 && say "v2fly: российские домены загружены" || say "v2fly пока недоступен — список подтянется таймером"
