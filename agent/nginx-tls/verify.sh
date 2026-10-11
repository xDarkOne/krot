# nginx-tls: конфиг цел, сертификаты живы, витрины отвечают через те же порты, что видят клиенты.
: "${VK_NAMES:?}" "${VK_ROLE:?}" "${VK_XHTTP_PORT:=4443}"
: "${VK_TUNNEL_NAME:=${VK_NAMES%% *}}"
nginx -t -q 2>/dev/null || die "nginx -t не проходит"
systemctl is-active -q nginx || die "nginx не запущен"
code() { curl -s -m 10 -o /dev/null -w '%{http_code}' --resolve "$1:$2:127.0.0.1" "https://$1:$2/"; }
for n in $VK_NAMES; do
  c=/etc/letsencrypt/live/$n/fullchain.pem
  openssl x509 -checkend $((20*86400)) -noout -in "$c" >/dev/null || die "сертификат $n истекает раньше 20 дней"
  [ "$(code "$n" 443)" = 200 ] || die "https://$n/ через 443 не отвечает 200"
  r=$(curl -s -m 10 -o /dev/null -w '%{http_code} %{redirect_url}' --resolve "$n:80:127.0.0.1" "http://$n/x")
  [ "$r" = "301 https://$n/x" ] || die "http://$n/ не уводит на https ($r)"
done
if [ "$VK_ROLE" != exit ]; then
  [ "$(code "$VK_TUNNEL_NAME" "$VK_XHTTP_PORT")" = 200 ] || die "https://$VK_TUNNEL_NAME:$VK_XHTTP_PORT/ не отвечает 200"
fi
f=$(nft list chain inet vpnkit_fw input)
grep -q 'tcp dport { 80, 443' <<<"$f" || die "порты 80/443 не открыты в фаерволе"
say "проверка nginx-tls: $(for n in $VK_NAMES; do printf '%s до %s; ' "$n" "$(openssl x509 -enddate -noout -in /etc/letsencrypt/live/$n/fullchain.pem | cut -d= -f2 | awk '{print $1,$2,$4}')"; done)витрины отвечают"
