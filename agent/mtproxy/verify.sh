# mtproxy: telemt жив и слушает только петлю, 443 ведёт в него, маска отдаёт настоящую витрину.
: "${VK_TLS_DOMAIN:?}" "${VK_LISTEN_PORT:=15443}" "${VK_API_PORT:=9091}" "${VK_BACKEND:=telemt}"
# не-Telegram клиент на 443 должен видеть настоящую витрину с настоящим сертификатом
mask_ok() {
  cn=$(echo | timeout 10 openssl s_client -connect 127.0.0.1:443 -servername "$VK_TLS_DOMAIN" 2>/dev/null | openssl x509 -noout -subject 2>/dev/null || true)
  grep -q "$VK_TLS_DOMAIN" <<<"$cn" || die "маска: на 443 нет сертификата $VK_TLS_DOMAIN (${cn:-нет ответа})"
  c=$(curl -s -m 10 -o /dev/null -w '%{http_code}' --resolve "$VK_TLS_DOMAIN:443:127.0.0.1" "https://$VK_TLS_DOMAIN/" || true)
  [ "$c" = 200 ] || die "витрина через маску telemt отвечает $c"
}
if [ "$VK_BACKEND" = mtproxyl ]; then
  : "${VK_WEB_DOMAIN:?}"
  systemctl is-active -q mtproxyl-telemt || die "движок MTProxyL не запущен: $(journalctl -u mtproxyl-telemt -n 3 -o cat --no-pager)"
  l=$(ss -Hltnp | grep '"mtproxyl-telem' || true)
  grep -q "127.0.0.1:$VK_LISTEN_PORT " <<<"$l" || die "telemt MTProxyL не слушает 127.0.0.1:$VK_LISTEN_PORT"
  grep -q "127.0.0.1:15080 " <<<"$l" || die "WEB-вход MTProxyL не слушает 127.0.0.1:15080"
  outside=$(grep -vE '(127\.0\.0\.1|\[::1\]):[0-9]+ ' <<<"$l" || true)
  [ -z "$outside" ] || die "telemt MTProxyL слушает не только петлю: $outside"
  grep -q "default 127.0.0.1:$VK_LISTEN_PORT;" /etc/nginx/stream.d/vpnkit-443.conf || die "TCP/443 не ведёт в telemt"
  grep -q "$VK_WEB_DOMAIN 127.0.0.1:15444;" /etc/nginx/stream.d/vpnkit-443.conf || die "TCP/443 не ведёт $VK_WEB_DOMAIN в WEB Proxy"
  mask_ok
  cn=$(echo | timeout 10 openssl s_client -connect 127.0.0.1:443 -servername "$VK_WEB_DOMAIN" 2>/dev/null | openssl x509 -noout -subject 2>/dev/null || true)
  grep -q "$VK_WEB_DOMAIN" <<<"$cn" || die "WEB Proxy: на 443 нет сертификата $VK_WEB_DOMAIN (${cn:-нет ответа})"
  grep -q "^MASKING_HOST='127.0.0.1'" /opt/mtproxyl/settings.conf || die "маска MTProxyL не на витрине 127.0.0.1:8444 — будет петля на свой домен"
  n=$(/usr/local/sbin/vpnkit-gen.py tg-list | wc -l)
  say "проверка mtproxy: MTProxyL $(grep -m1 '^VERSION=' /opt/mtproxyl/mtproxyl.sh | cut -d'"' -f2), telemt $(cat /opt/mtproxyl/engine/.version 2>/dev/null), только петля, маска отдаёт витрину, WEB Proxy $VK_WEB_DOMAIN с сертификатом, пользователей $n"
  exit 0
fi
systemctl is-active -q telemt || die "telemt не запущен: $(journalctl -u telemt -n 3 -o cat --no-pager)"
l=$(ss -Hltnp | grep '"telemt"' || true)
grep -q "127.0.0.1:$VK_LISTEN_PORT " <<<"$l" || die "telemt не слушает 127.0.0.1:$VK_LISTEN_PORT"
outside=$(grep -vE '(127\.0\.0\.1|\[::1\]):[0-9]+ ' <<<"$l" || true)
[ -z "$outside" ] || die "telemt слушает не только петлю: $outside"
grep -q "default 127.0.0.1:$VK_LISTEN_PORT;" /etc/nginx/stream.d/vpnkit-443.conf || die "TCP/443 не ведёт в telemt"
mask_ok
api=$(curl -s -m 10 "http://127.0.0.1:$VK_API_PORT/v1/health" || true)
say "проверка mtproxy: telemt $(/usr/local/bin/telemt --version | awk '{print $2}'), только петля, маска отдаёт витрину; API: ${api:0:160}"
