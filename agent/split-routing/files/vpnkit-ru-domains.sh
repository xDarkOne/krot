#!/bin/bash
# Список российских доменов в чужих зонах из v2fly/domain-list-community (category-ru) для dnsmasq.
# Зачем поверх зон .ru/.su/.рф: множество российских сервисов живёт на зарубежных доменах
# (ozon.com, yandex.com, mail.ru на .com-поддоменах), и угадывать их руками — проигранная гонка.
# Файл /etc/dnsmasq.d/20-vpnkit-v2fly.conf создаётся автоматически; при сбое источника остаётся прежний.
set -u
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
BASE="https://raw.githubusercontent.com/v2fly/domain-list-community/master/data"
OUT=/etc/dnsmasq.d/20-vpnkit-v2fly.conf
RESOLVER=$(awk -F= '$1=="RU_RESOLVER"{print $2}' /etc/vpnkit/split.conf 2>/dev/null)
RESOLVER=${RESOLVER:-77.88.8.8}
TMP=$(mktemp); TMPD=$(mktemp)
trap 'rm -f "$TMP" "$TMPD" "$OUT.new"' EXIT
log() { logger -t vpnkit-ru-domains "$1"; echo "$1"; }

fetch() {  # разворачиваем include рекурсивно, не глубже 3 уровней
  local file="$1" depth="${2:-0}" line
  [ "$depth" -gt 3 ] && return
  curl -fsSL --max-time 25 "$BASE/$file" 2>/dev/null | while IFS= read -r line; do
    line="${line%%#*}"; line="$(echo "$line" | xargs)"
    [ -z "$line" ] && continue
    case "$line" in
      include:*) fetch "${line#include:}" $((depth + 1)) ;;
      full:*) echo "${line#full:}" ;;
      domain:*) echo "${line#domain:}" ;;
      keyword:*|regexp:*) : ;;  # dnsmasq такого не умеет
      *) echo "${line%% *}" ;;  # отбрасываем @атрибуты
    esac
  done
}

fetch category-ru > "$TMPD"
grep -oE '^[a-z0-9.-]+\.[a-z]{2,}$' "$TMPD" | grep -vE '\.(ru|su)$' | sort -u > "$TMP"
n=$(wc -l < "$TMP")
if [ "$n" -lt 100 ]; then
  log "получено всего $n доменов — источник недоступен или изменился, файл не трогаю"
  exit 1
fi
{
  echo "# Российские домены из v2fly/domain-list-community (category-ru). Создаётся автоматически."
  echo "# Обновлено: $(date '+%F %T'), доменов: $n"
  while IFS= read -r d; do
    echo "nftset=/$d/4#inet#vpnkit_split#ru_domains"
    echo "server=/$d/$RESOLVER"
  done < "$TMP"
} > "$OUT.new"
if [ -f "$OUT" ] && cmp -s "$OUT" "$OUT.new"; then log "список не изменился: $n доменов"; exit 0; fi
old=""; [ -f "$OUT" ] && { old=$(mktemp); cp -a "$OUT" "$old"; }
mv -f "$OUT.new" "$OUT"
if grep -q "syntax check OK" <<<"$(dnsmasq --test 2>&1)"; then
  systemctl restart dnsmasq && log "список обновлён: $n доменов"
else
  if [ -n "$old" ]; then mv -f "$old" "$OUT"; else rm -f "$OUT"; fi
  systemctl restart dnsmasq
  log "dnsmasq отверг файл — вернул прежний"
  exit 1
fi
[ -n "$old" ] && rm -f "$old"
exit 0
