# xray: конфиг цел, порты слушаются, маршрутизация ведёт себя как задумано.
: "${VK_XH_PORT:?}" "${VK_ENC_PORT:?}" "${VK_SOCKS_PORT:=10801}" "${VK_CASCADE:=0}" "${VK_EXIT_IP:=}"
: "${VK_TUNNEL_NAME:?}" "${VK_XHTTP_PORT:?}" "${VK_XH_PATH:?}"
/usr/local/bin/xray run -test -config /usr/local/etc/xray/config.json >/dev/null || die "xray -test не проходит"
systemctl is-active -q xray || die "xray не запущен: $(journalctl -u xray -n 3 -o cat --no-pager)"
for i in 1 2 3 4 5; do l=$(ss -Hltn); grep -q "127.0.0.1:$VK_SOCKS_PORT " <<<"$l" && break; sleep 1; done
grep -q "127.0.0.1:$VK_XH_PORT " <<<"$l" || die "XHTTP 127.0.0.1:$VK_XH_PORT не слушается"
grep -qE "(\*|0\.0\.0\.0):$VK_ENC_PORT " <<<"$l" || die "PQ :$VK_ENC_PORT не слушается"
S="socks5h://127.0.0.1:$VK_SOCKS_PORT"
if [ "$VK_CASCADE" = 1 ]; then
  ip=$(curl -s -m 15 -x "$S" https://api.ipify.org || true)
  [ "$ip" = "$VK_EXIT_IP" ] || die "зарубежный сайт видит ${ip:-ничего}, а не выход $VK_EXIT_IP"
  ip=$(curl -s -m 15 -x "$S" https://ipv4-internet.yandex.net/api/v0/ip | tr -d '"' || true)
  [ -n "$ip" ] && [ "$ip" != "$VK_EXIT_IP" ] || die "российский сайт видит ${ip:-ничего} — должен видеть сам вход"
  say "маршруты: зарубежное → каскад ($VK_EXIT_IP), российское → напрямую ($ip)"
fi
c=$(curl -s -m 5 -x "$S" -o /dev/null -w '%{http_code}' http://127.0.0.1:8444/ || true)
[ "$c" = 000 ] || die "клиенту доступна петля узла (127.0.0.1:8444 ответил $c) — частные сети должны быть закрыты"
# XHTTP за nginx: путь отдан xray, а не витрине (у витрины на этом пути 404 от nginx)
c=$(curl -s -m 10 -o /dev/null -w '%{http_code}' --resolve "$VK_TUNNEL_NAME:$VK_XHTTP_PORT:127.0.0.1" \
     "https://$VK_TUNNEL_NAME:$VK_XHTTP_PORT$VK_XH_PATH/" || true)
srv=$(curl -s -m 10 -D - -o /dev/null --resolve "$VK_TUNNEL_NAME:$VK_XHTTP_PORT:127.0.0.1" \
     "https://$VK_TUNNEL_NAME:$VK_XHTTP_PORT$VK_XH_PATH/" | tr -d '\r' | grep -i '^server:' || true)
case "$c" in 404|000) die "XHTTP-путь $VK_XH_PATH за nginx отвечает $c — до xray не доходит" ;; esac
say "XHTTP за nginx: путь отдан xray (голый GET → $c); PQ tcp/$VK_ENC_PORT; петля закрыта"
f=$(nft list chain inet vpnkit_fw input)
grep -q "tcp dport $VK_ENC_PORT accept" <<<"$f" || die "порт PQ $VK_ENC_PORT не открыт"
