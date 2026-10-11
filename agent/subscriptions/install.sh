# subscriptions: персональные подписки https://<имя>/u/<токен> — по одной на клиента.
# Параметры: VK_SUB_NAME (имя витрины для раздачи), VK_ROUTING (1 — правила Happ/INCY на /h и /i),
# VK_ROUTING_NAME (как набор правил подписан в приложении). Описание — docs/20-server-modules/subscriptions.md
: "${VK_SUB_NAME:?}" "${VK_ROUTING:=1}" "${VK_ROUTING_NAME:=VPN}"
[ -s /etc/vpnkit/xray.env ] || die "нет модуля xray — подписке нечего раздавать"
[ -f "/etc/nginx/sites-available/vpnkit-$VK_SUB_NAME" ] || die "нет витрины $VK_SUB_NAME — сначала nginx-tls"
[ -s "$VK_RUN/src/vpnkit-gen.py" ] || die "нет vpnkit-gen.py от пульта"
put /usr/local/sbin/vpnkit-gen.py 755 < "$VK_RUN/src/vpnkit-gen.py"
OUT=/var/www/vpnkit-sub/u
mkdir -p "$OUT"
printf '# vpnkit subscriptions. Пишет пульт.\nSUB_NAME=%s\nOUTDIR=%s\nROUTING=%s\nROUTING_NAME=%s\n' \
  "$VK_SUB_NAME" "$OUT" "$VK_ROUTING" "$VK_ROUTING_NAME" | put /etc/vpnkit/sub.env 600
snapshot "/etc/nginx/vpnkit.d/$VK_SUB_NAME-site-routing.conf"   # его пишет генератор
cat <<EOF | put "/etc/nginx/vpnkit.d/$VK_SUB_NAME-site-sub.conf" 644
# vpnkit subscriptions. Токены не пишутся в журнал (P-50); чужой токен — обычный 404 витрины.
location ^~ /u/ {
    access_log off;
    alias $OUT/;
    default_type text/plain;
    add_header Cache-Control "no-store" always;
    try_files \$uri =404;
}
EOF
nginx -t -q
systemctl reload nginx
/usr/local/sbin/vpnkit-gen.py build
say "подписки: https://$VK_SUB_NAME/u/<токен>"
