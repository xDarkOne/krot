#!/usr/bin/env bash
# Самопроверка redact-check.sh: на подложенных секретах он обязан сработать,
# на заглушках и номерах версий — промолчать. Запускается хуком перед коммитом.
# Появилось после того, как 29.09.2026 проверка молча пропускала UUID и short_id.
set -u
here=$(cd "$(dirname "$0")" && pwd)
d=$(mktemp -d); trap 'rm -rf "$d"' EXIT
key=$(head -c32 /dev/urandom | base64)
cat > "$d/t.txt" <<EOF
PrivateKey = $key
uuid 3f2a9c1e-1b2c-4d5e-8f90-a1b2c3d4e5f6
https://ru.example.test/u/AbCdEfGhIjKlMnOp
password=hunter2hunter2
hash \$2a\$10\$0xSY7GXxQKerTOBqxZmGleDpt1VwGdjxE7ttoBpsoPQvk8wcQBAAm
"short_id": "1c3e69f9"
-wrap-key 3f9a1c0e5b7d2a4c6e8f0a1b2c3d4e5f
bot 1234567890:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsawZ
PrivateKey = <REDACTED>
pass: "2.0.0-rc.1"
EOF
got=$(bash "$here/redact-check.sh" "$d" | cut -d: -f2 | sort -un | tr '\n' ' ')
want="1 2 3 4 5 6 7 8 "
if [ "$got" != "$want" ]; then
  echo "redact-check-selftest: ПРОВАЛ — сработали строки [$got], ждали [$want]" >&2
  exit 1
fi
echo "redact-check-selftest: ок (8 видов секретов ловятся, заглушки и версии — нет)"
