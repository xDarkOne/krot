# hysteria2: основной UDP-вход подписки (sing-box), трафик сдаётся в xray — маршрутизация общая.
# Параметры: VK_SINGBOX_VERSION, VK_SINGBOX_SHA256_AMD64/ARM64 (пин), VK_TUNNEL_NAME, VK_HY2_PORT.
# Описание — docs/20-server-modules/hysteria2.md
: "${VK_SINGBOX_VERSION:?}" "${VK_SINGBOX_SHA256_AMD64:?}" "${VK_SINGBOX_SHA256_ARM64:?}" "${VK_TUNNEL_NAME:?}" "${VK_HY2_PORT:=443}"
[ -s /etc/vpnkit/xray.env ] || die "нет модуля xray — hysteria2 сдаёт трафик в него"
[ -f "/etc/letsencrypt/live/$VK_TUNNEL_NAME/fullchain.pem" ] || die "нет сертификата $VK_TUNNEL_NAME — сначала nginx-tls"
l=$(ss -Hlunp "sport = :$VK_HY2_PORT")
if [ -n "$l" ] && ! grep -q '"sing-box"' <<<"$l"; then die "UDP $VK_HY2_PORT занят: $l"; fi

arch=$(dpkg --print-architecture)
case "$arch" in
  amd64) sum=$VK_SINGBOX_SHA256_AMD64 ;; arm64) sum=$VK_SINGBOX_SHA256_ARM64 ;;
  *) die "архитектура $arch не поддерживается" ;;
esac
have=$(dpkg-query -W -f '${Version}' sing-box 2>/dev/null || true)
if [ "$have" != "$VK_SINGBOX_VERSION" ]; then
  d=$VK_RUN/sing-box.deb
  curl -fsSL -m 300 -o "$d" "https://github.com/SagerNet/sing-box/releases/download/v$VK_SINGBOX_VERSION/sing-box_${VK_SINGBOX_VERSION}_linux_$arch.deb" \
    || die "не скачать sing-box $VK_SINGBOX_VERSION с GitHub"
  [ "$(sha256sum "$d" | cut -d' ' -f1)" = "$sum" ] || die "sha256 sing-box не совпал с пином — не ставлю"
  [ -n "$have" ] || echo sing-box >> "$VK_RUN/created.list"
  # конфиг — conffile пакета: при обновлении оставляем свой (его пишет генератор)
  dpkg -i --force-confold "$d" >/dev/null || die "dpkg -i sing-box не удался"
  mkdir -p /var/lib/vpnkit/pkgcache && cp "$d" "/var/lib/vpnkit/pkgcache/sing-box_${VK_SINGBOX_VERSION}_$arch.deb"
  say "sing-box $VK_SINGBOX_VERSION поставлен (sha256 сверен)"
else
  say "sing-box $VK_SINGBOX_VERSION уже стоит"
fi

# штатный ExecStart читает весь каталог (-C /etc/sing-box): любой второй json там ломает службу
cat <<'EOF' | put /etc/systemd/system/sing-box.service.d/10-vpnkit-single-config.conf 644
# vpnkit hysteria2: только config.json, не весь каталог
[Service]
ExecStart=
ExecStart=/usr/bin/sing-box -D /var/lib/sing-box -c /etc/sing-box/config.json run
EOF
printf '# vpnkit hysteria2. Пишет пульт.\nTUNNEL_NAME=%s\nHY2_PORT=%s\n' "$VK_TUNNEL_NAME" "$VK_HY2_PORT" | put /etc/vpnkit/hy2.env 600
cat <<EOF | put /etc/nftables.d/46-vpnkit-hy2.nft 644
# vpnkit hysteria2
table inet vpnkit_fw {
	chain input {
		udp dport $VK_HY2_PORT accept
	}
}
EOF
nft -c -f /etc/nftables.conf
systemctl reload nftables
snapshot /etc/sing-box/config.json
systemctl daemon-reload
systemctl enable -q sing-box
# конфиг генератор перезапускает сам, когда он изменился; здесь — новый пакет или юнит
out=$(/usr/local/sbin/vpnkit-gen.py build)
if { [ "$have" != "$VK_SINGBOX_VERSION" ] || changed /etc/systemd/system/sing-box.service.d/ || ! systemctl is-active -q sing-box; } \
   && ! grep -q 'sing-box перезапущен' <<<"$out"; then
  systemctl restart sing-box
fi
say "hysteria2: udp/$VK_HY2_PORT, сертификат $VK_TUNNEL_NAME, выход — через xray"
