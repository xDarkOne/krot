# nginx-tls: сертификаты Let's Encrypt (webroot, ECDSA), витрина на каждом имени,
# на entry — разбор TCP/443 по SNI (stream) и TLS-порт для XHTTP.
# Параметры: VK_NAMES (через пробел; первое — основное), VK_TUNNEL_NAME, VK_HOST, VK_ROLE,
# VK_EMAIL, VK_XHTTP_PORT, VK_SITE_{LANG,TITLE,TAGLINE,P1,P2}.
# Описание — docs/20-server-modules/nginx-tls.md
: "${VK_NAMES:?}" "${VK_HOST:?}" "${VK_ROLE:?}" "${VK_EMAIL:=}" "${VK_XHTTP_PORT:=4443}"
: "${VK_TUNNEL_NAME:=${VK_NAMES%% *}}"
: "${VK_SITE_LANG:=en}" "${VK_SITE_TITLE:?}" "${VK_SITE_TAGLINE:=}" "${VK_SITE_P1:=}" "${VK_SITE_P2:=}"
[ -s "$VK_RUN/src/index.html" ] || die "нет шаблона витрины от пульта"
entry=1; [ "$VK_ROLE" = exit ] && entry=0

dpkg -s nginx >/dev/null 2>&1 || echo nginx >> "$VK_RUN/created.list"
apt_install nginx libnginx-mod-stream certbot
systemctl enable -q nginx

# штатный сайт Ubuntu слушает :80 как default — убрать (снимок вернёт при откате)
if [ -e /etc/nginx/sites-enabled/default ]; then snapshot /etc/nginx/sites-enabled/default; rm -f /etc/nginx/sites-enabled/default; fi
mkdir -p /var/www/acme /etc/nginx/vpnkit.d /etc/nginx/stream.d

# --- фаервол: 80 (ACME, 301), 443, на entry ещё порт XHTTP
ports="80, 443"; [ $entry = 1 ] && ports="$ports, $VK_XHTTP_PORT"
cat <<EOF | put /etc/nftables.d/40-vpnkit-nginx.nft 644
# vpnkit nginx-tls
table inet vpnkit_fw {
	chain input {
		tcp dport { $ports } accept
	}
}
EOF
nft -c -f /etc/nftables.conf
systemctl reload nftables

link() {  # link <имя> — включить vhost (ссылка тоже под снимком)
  local l=/etc/nginx/sites-enabled/vpnkit-$1
  [ -L "$l" ] && return 0
  snapshot "$l"; ln -s "/etc/nginx/sites-available/vpnkit-$1" "$l"
}
acme80() {  # acme80 <имя> — :80 только ACME и 301
  cat <<EOF
server {
    listen 80;
    server_name $1;
    server_tokens off;
    location /.well-known/acme-challenge/ { root /var/www/acme; }
    location / { return 301 https://\$host\$request_uri; }
}
EOF
}

# --- шаг 1: только :80, чтобы выпустить сертификаты
for n in $VK_NAMES; do
  [ -f "/etc/letsencrypt/live/$n/fullchain.pem" ] && continue
  acme80 "$n" | put "/etc/nginx/sites-available/vpnkit-$n" 644
  link "$n"
done
nginx -t -q
systemctl reload-or-restart nginx
mail=(--register-unsafely-without-email); [ -n "$VK_EMAIL" ] && mail=(--email "$VK_EMAIL")
for n in $VK_NAMES; do
  [ -f "/etc/letsencrypt/live/$n/fullchain.pem" ] && { say "сертификат $n уже есть"; continue; }
  out=$(certbot certonly --webroot -w /var/www/acme -d "$n" --key-type ecdsa --non-interactive \
        --agree-tos "${mail[@]}" --keep-until-expiring 2>&1) || die "certbot $n: $(tail -5 <<<"$out")"
  say "сертификат $n выпущен"
done
printf '#!/bin/sh\n# vpnkit: после продления — перечитать сертификаты (sing-box держит старый в памяти, P-20)\nsystemctl reload nginx\nsystemctl try-restart sing-box\n' \
  | put /etc/letsencrypt/renewal-hooks/deploy/vpnkit-reload.sh 755

# --- шаг 2: витрины и TLS
# директива «http2 on» появилась в nginx 1.25.1; в Ubuntu 24.04 стоит 1.24 — там флаг в listen (P-56)
v=$(nginx -v 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')
if [ "$(printf '%s\n1.25.1\n' "$v" | sort -V | head -1)" = 1.25.1 ]; then H2=""; H2ON="    http2 on;"; else H2=" http2"; H2ON=""; fi
year=$(date +%Y)
e() { printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/[&|\\]/\\&/g'; }   # HTML, затем для sed
for n in $VK_NAMES; do
  root=/var/www/vpnkit-$n
  mkdir -p "$root"
  sed -e "s|__LANG__|$(e "$VK_SITE_LANG")|g" -e "s|__TITLE__|$(e "$VK_SITE_TITLE")|g" \
      -e "s|__TAGLINE__|$(e "$VK_SITE_TAGLINE")|g" -e "s|__P1__|$(e "$VK_SITE_P1")|g" \
      -e "s|__P2__|$(e "$VK_SITE_P2")|g" -e "s|__YEAR__|$year|g" "$VK_RUN/src/index.html" \
    | put "$root/index.html" 644
  tls="    ssl_certificate     /etc/letsencrypt/live/$n/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$n/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    server_tokens off;
    root $root;
    index index.html;"
  {
    echo "# vpnkit nginx-tls: $n. Пишет пульт — руками не править; свои location — в /etc/nginx/vpnkit.d/"
    acme80 "$n"
    if [ $entry = 1 ]; then
      # TCP/443 разбирает stream (stream.d/vpnkit-443.conf); витрина — на петле
      cat <<EOF

server {
    listen 127.0.0.1:8444 ssl$H2;
$H2ON
    server_name $n;
$tls
    include /etc/nginx/vpnkit.d/$n-site-*.conf;
    location / { try_files \$uri \$uri/ =404; }
}
EOF
      if [ "$n" = "$VK_TUNNEL_NAME" ]; then cat <<EOF

server {
    listen $VK_XHTTP_PORT ssl$H2;
$H2ON
    server_name $n;
$tls
    include /etc/nginx/vpnkit.d/$n-xhttp-*.conf;
    location / { try_files \$uri \$uri/ =404; }
}
EOF
      fi
    else
      cat <<EOF

server {
    listen 443 ssl$H2;
$H2ON
    server_name $n;
$tls
    include /etc/nginx/vpnkit.d/$n-site-*.conf;
    location / { try_files \$uri \$uri/ =404; }
}
EOF
    fi
  } | put "/etc/nginx/sites-available/vpnkit-$n" 644
  link "$n"
done

if [ $entry = 1 ]; then
  # весь TCP/443 — через stream: сейчас всё на витрину; MTProxy потом заберёт себе default
  # блок stream кладём в modules-enabled: nginx.conf подключает его на верхнем уровне, после
  # 50-mod-stream.conf, и сам nginx.conf не трогаем. Если в нём уже свой stream с stream.d — хватит его.
  if ! grep -qE '^\s*stream\s*\{' /etc/nginx/nginx.conf; then
    printf '# vpnkit nginx-tls: блок stream (модуль подключает 50-mod-stream.conf)\nstream {\n    include /etc/nginx/stream.d/*.conf;\n}\n' \
      | put /etc/nginx/modules-enabled/99-vpnkit-stream.conf 644
  fi
fi
# куда идёт TCP/443 (витрина или telemt), решает генератор по факту установленных модулей:
# иначе повторная установка nginx-tls молча отрезала бы MTProxy
put /usr/local/sbin/vpnkit-gen.py 755 < "$VK_RUN/src/vpnkit-gen.py"
mkdir -p /etc/vpnkit; chmod 700 /etc/vpnkit
printf '# vpnkit nginx-tls. Пишет пульт.\nSTREAM=%s\n' "$entry" | put /etc/vpnkit/nginx.env 600
snapshot /etc/nginx/stream.d/vpnkit-443.conf
/usr/local/sbin/vpnkit-gen.py build >/dev/null
nginx -t -q
systemctl reload-or-restart nginx
say "nginx: $(nginx -v 2>&1 | cut -d/ -f2), имена: $VK_NAMES$([ $entry = 1 ] && echo ", XHTTP-порт $VK_XHTTP_PORT для $VK_TUNNEL_NAME")"
