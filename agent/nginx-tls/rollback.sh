# nginx-tls: откат прогона. Сертификаты не удаляются (повторный выпуск упирается в лимиты Let's Encrypt).
restore_snapshot
systemctl reload nftables 2>/dev/null || true
if grep -qxF nginx "$VK_RUN/created.list" 2>/dev/null; then
  systemctl disable -q --now nginx 2>/dev/null || true   # пакет оставляем, службу гасим
elif nginx -t -q 2>/dev/null; then
  systemctl reload-or-restart nginx 2>/dev/null || true
fi
say "откат nginx-tls выполнен"
