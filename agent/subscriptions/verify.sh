# subscriptions: каждая подписка отдаётся через 443 и содержит ссылки; чужой токен — 404.
: "${VK_SUB_NAME:?}"
port=443
n=0
for t in $(python3 -c 'import json;[print(c["token"]) for c in json.load(open("/etc/vpnkit/users.json"))["clients"].values()]'); do
  body=$(curl -s -m 10 --resolve "$VK_SUB_NAME:$port:127.0.0.1" "https://$VK_SUB_NAME/u/$t")
  k=$(base64 -d <<<"$body" 2>/dev/null | grep -c '://' || true)
  [ "$k" -ge 2 ] || die "подписка …${t: -4} отдаёт $k ссылок"
  n=$((n+1))
done
c=$(curl -s -m 10 -o /dev/null -w '%{http_code}' --resolve "$VK_SUB_NAME:$port:127.0.0.1" "https://$VK_SUB_NAME/u/$(head -c16 /dev/urandom | xxd -p)")
[ "$c" = 404 ] || die "чужой токен отвечает $c, а не 404"
r=""
if [ "${VK_ROUTING:-1}" = 1 ]; then
  for pa in happ:h incy:i; do
    loc=$(curl -s -m 10 -o /dev/null -w '%{redirect_url}' --resolve "$VK_SUB_NAME:$port:127.0.0.1" "https://$VK_SUB_NAME/${pa#*:}" || true)
    case "$loc" in "${pa%%:*}://routing/onadd/"*) r="$r ${pa%%:*}" ;; *) die "/${pa#*:} не отдаёт правила ${pa%%:*}:// (${loc:0:40})" ;; esac
  done
  r="; правила маршрутизации:$r"
fi
say "проверка подписок: $n отдаются, чужой токен — 404$r"
