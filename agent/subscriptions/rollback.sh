# subscriptions: откат прогона. Каталог с подписками остаётся, но больше не раздаётся.
restore_snapshot
nginx -t -q 2>/dev/null && systemctl reload nginx 2>/dev/null || true
say "откат subscriptions выполнен"
