#!/usr/bin/env bash
# Проверка репозитория на секреты перед коммитом.
# Код выхода 0 — чисто, 1 — найдено подозрительное (вывод: файл:строка:совпадение).
# Запуск: tools/redact-check.sh [каталог]   (по умолчанию — корень репо)
set -u
root="${1:-$(cd "$(dirname "$0")/.." && pwd)}"
cd "$root" || exit 2

# Что считаем секретом. Каждая строка — расширенное регулярное выражение.
patterns=(
  # ключи WireGuard/AmneziaWG, PSK, HeaderProtectionKey: base64 длиной 44 с '='
  '[A-Za-z0-9+/]{42}[AEIMQUYcgkosw048]='
  # UUID клиентов VLESS
  '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}'
  # токен Telegram-бота
  '[0-9]{8,10}:[A-Za-z0-9_-]{35}'
  # секреты MTProto (32 hex) и похожие
  '(^|[^0-9a-fA-F])[0-9a-fA-F]{32}([^0-9a-fA-F]|$)'
  # ссылки подписок с токеном
  '/u/[A-Za-z0-9_-]{12,}'
  # явные присваивания секретов, если значение не замаскировано
  '(PrivateKey|PresharedKey|HeaderProtectionKey|private_key|preshared_key|awg_header_protection_key|privateKey)[[:space:]]*[=:][[:space:]]*["'"'"']?[A-Za-z0-9+/]{20,}'
  '(-wrap-key|wrap_key)[[:space:]=]+[A-Za-z0-9+/=_-]{16,}'
  '(password|passwd|pass|token|secret|AuthKey)[[:space:]]*[=:][[:space:]]*["'"'"']?[A-Za-z0-9+/_.-]{8,}'
  'RT_PASS=[^[:space:]…]'
  # bcrypt-хеши и поля Reality
  '\$2[aby]\$[0-9]{2}\$[./A-Za-z0-9]{53}'
  '"(short_id|shortId|public_key|publicKey|password_hash)"[[:space:]]*:[[:space:]]*"[^<"][^"]*"'
  # приватные ключи в PEM
  'BEGIN (OPENSSH |EC |RSA )?PRIVATE KEY'
)

# Разрешённые заглушки и заведомо безопасные образцы
allow='(^|:|")[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?"?$|"(str|int|bool|float|NoneType)"$|<REDACTED>|<[A-Z_]+>|example|EXAMPLE|0{32}|x{8,}|\$\{?[A-Z_]+\}?'

found=0
files=$(git ls-files -co --exclude-standard 2>/dev/null || find . -type f -not -path './.git/*')
# wails_tools.nsh — шаблон установщика Wails: GUID WebView2 в реестре и ссылка на gist;
# package-lock.json / package.json.md5 — хеши целостности публичных пакетов npm. Не секреты.
files=$(printf '%s\n' $files | grep -vE '^(tools/redact-check\.sh|tools/redact-check-selftest\.sh|go\.sum|app/build/windows/installer/wails_tools\.nsh|app/frontend/package-lock\.json|app/frontend/package\.json\.md5)$|\.(png|jpg|ico)$')
pats=$(mktemp); trap 'rm -f "$pats"' EXIT
printf '%s\n' "${patterns[@]}" > "$pats"
# один проход grep по всем файлам сразу (раньше — файлы × шаблоны, тысячи запусков)
hits=$(printf '%s\n' $files | xargs -d '\n' grep -HnEo -f "$pats" -- 2>/dev/null | grep -vE -- "$allow" || true)
if [ -n "$hits" ]; then echo "$hits"; found=1; fi

if [ "$found" -eq 0 ]; then echo "redact-check: чисто"; fi
exit "$found"
