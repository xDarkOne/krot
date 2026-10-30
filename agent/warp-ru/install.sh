# warp-ru (entry): российский трафик клиентов выходит через Cloudflare WARP, а не с адреса хостинга.
# Работает поверх split-routing: ядерные клиенты (метка 0x1) и xray (outbound ru-exit, метка 0x4) идут в таблицу 201.
# При обрыве WARP сторож сам возвращает российский трафик на прямой выход; нового аккаунта хватает без участия человека.
# Описание — docs/20-server-modules/warp-ru.md
for f in warp-ru i1.txt endpoints bypass-domains; do [ -s "$VK_RUN/src/$f" ] || die "нет файла $f от пульта"; done
apt_install python3 curl
mkdir -p /etc/warp-ru /var/lib/warp-ru; chmod 700 /etc/warp-ru

put /usr/local/sbin/warp-ru 755 < "$VK_RUN/src/warp-ru"
put /etc/warp-ru/i1.txt 600 < "$VK_RUN/src/i1.txt"
put /etc/warp-ru/endpoints 644 < "$VK_RUN/src/endpoints"
# список обхода — свой у пользователя, повторная установка его не затирает
[ -e /etc/warp-ru/bypass-domains ] || put /etc/warp-ru/bypass-domains 644 < "$VK_RUN/src/bypass-domains"

snapshot /etc/warp-ru/account.json
if [ ! -s /etc/warp-ru/account.json ]; then
  say "завожу анонимный аккаунт WARP (запрос идёт через каскад)…"
  ok=0
  for i in 1 2 3; do /usr/local/sbin/warp-ru init && { ok=1; break; }; sleep 5; done
  [ $ok = 1 ] || die "не удалось завести аккаунт WARP: через каскад Cloudflare не ответил или не пропустил трафик"
fi

cat <<'U' | put /etc/systemd/system/warp-ru.service 644
[Unit]
Description=Российский трафик клиентов через Cloudflare WARP (AmneziaWG)
After=network-online.target nftables.service awg-quick@awg0.service
Wants=network-online.target

[Service]
Type=simple
ExecStartPre=/usr/local/sbin/warp-ru stop
ExecStartPre=/usr/local/sbin/warp-ru start
ExecStart=/usr/local/sbin/warp-ru watch
ExecStopPost=/usr/local/sbin/warp-ru stop
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
U
[ -e /etc/systemd/system/multi-user.target.wants/warp-ru.service ] || echo warp-ru.service >> "$VK_RUN/created.list"
systemctl daemon-reload
systemctl enable -q warp-ru.service
if changed /usr/local/sbin/warp-ru || changed /etc/warp-ru/ || changed /etc/systemd/system/warp-ru.service || ! systemctl is-active -q warp-ru; then
  systemctl restart warp-ru
fi

# xray узнаёт о WARP по /etc/warp-ru/account.json: перестраиваем конфиг и перезапускаем только при изменении
before=$(sha256sum /usr/local/etc/xray/config.json 2>/dev/null | cut -c1-64)
/usr/local/sbin/vpnkit-gen.py build >/dev/null
after=$(sha256sum /usr/local/etc/xray/config.json 2>/dev/null | cut -c1-64)
if [ "$before" != "$after" ]; then systemctl restart xray; say "xray: российское — через outbound ru-exit"; fi

# ждём первой проверки связи (сторож включает WARP после двух успешных проб подряд)
for i in $(seq 1 12); do
  grep -q 'dev awgw' <<<"$(ip route show default table 201 2>/dev/null)" && break
  sleep 5
done
