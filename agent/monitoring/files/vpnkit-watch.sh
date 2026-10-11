#!/bin/bash
# vpnkit-watch — проверки узла и оповещения в Telegram (только чтение, узел не меняет).
# Сообщение уходит, когда проверка не проходит FAILS_TO_ALERT раз подряд,
# и ещё одно — когда она снова проходит. Состояние: /var/lib/vpnkit/watch.state.
# Настройки: /etc/vpnkit/watch.conf и /etc/vpnkit/alert.env (TG_TOKEN, TG_CHATS; без него —
# только журнал). Что проверять, скрипт определяет по самому узлу: включённые службы,
# наличие туннеля каскада, наборов split, контейнера MTProxy, таймера копий.
set -uo pipefail
# Проверка соседа: 5 пингов, недоступен — только если не дошёл ни один (ping возвращает 1
# лишь при 100% потерь). 01.10.2026 две подряд потери двух пингов из-за моргания сети
# дали ложное оповещение, хотя каскад работал.
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077
CONF=${WATCH_CONF:-/etc/vpnkit/watch.conf}
STATE=${WATCH_STATE:-/var/lib/vpnkit/watch.state}
FAILS_TO_ALERT=2
mkdir -p /var/lib/vpnkit
NODE=$(hostname); EXIT_IP=""; PEER_TUN=""; PEER_NAME=""; PEER_PUBLIC=""; PEER_TCP=""; SERVICES_EXTRA=""
# shellcheck source=/dev/null
. "$CONF"
TG_TOKEN=""; TG_CHATS=""
# shellcheck source=/dev/null
[ -f /etc/vpnkit/alert.env ] && . /etc/vpnkit/alert.env

send() {  # $1 — текст. Сначала обычным путём, при неудаче — напрямую через внешний интерфейс
  [ -n "$TG_TOKEN" ] && [ -n "$TG_CHATS" ] || { logger -t vpnkit-watch "$1"; return 0; }
  local ok=1 c wan
  wan=$(ip -o -4 route show default | awk '{print $5}' | head -1)
  for c in $TG_CHATS; do
    url="https://api.telegram.org/bot$TG_TOKEN/sendMessage"
    if [ "$(curl -s -m 15 -o /dev/null -w '%{http_code}' "$url" --data-urlencode "chat_id=$c" --data-urlencode "text=$1")" = 200 ]; then ok=0
    elif [ -n "$wan" ] && [ "$(curl -s -m 15 --interface "$wan" -o /dev/null -w '%{http_code}' "$url" --data-urlencode "chat_id=$c" --data-urlencode "text=$1")" = 200 ]; then ok=0
    fi
  done
  return $ok
}

# --- проверки ----------------------------------------------------------------
declare -A RESULT   # ключ → пусто (ок) или текст проблемы
chk() { RESULT[$1]="$2"; }   # chk <ключ> "<проблема или пусто>"

svc() { for s in "$@"; do systemctl is-active --quiet "$s" && chk "svc:$s" "" || chk "svc:$s" "служба $s: $(systemctl is-active "$s")"; done; }

# включённые службы, которые обязаны работать (своё и стек VPN; чужое на узле не смотрим)
WATCH_RE='^(awg-quick@|wg-quick@|nftables|dnsmasq|fail2ban|xray|sing-box|nginx|docker|danted|mtproxyl|telemt|mss443)'
# экземпляры шаблонов (awg-quick@awg0) list-unit-files не показывает — их видно по ссылкам *.wants/
units=$( { systemctl list-unit-files --type=service --state=enabled --no-legend 2>/dev/null | awk '{print $1}'
           find /etc/systemd/system -path '*.wants/*.service' -printf '%f\n' 2>/dev/null; } \
         | grep -v '@\.service$' | sed 's/\.service$//' | grep -E "$WATCH_RE" | sort -u || true)
# shellcheck disable=SC2086
svc $units $SERVICES_EXTRA

disk() { local p; p=$(df --output=pcent / | tail -1 | tr -dc 0-9); [ "$p" -lt 85 ] && chk disk "" || chk disk "диск заполнен на $p%"; }

certs() {
  local f d left bad=""
  for f in /etc/letsencrypt/live/*/cert.pem; do
    [ -f "$f" ] || continue
    d=$(openssl x509 -enddate -noout -in "$f" | cut -d= -f2)
    left=$(( ($(date -d "$d" +%s) - $(date +%s)) / 86400 ))
    [ "$left" -lt 14 ] && bad="$bad $(basename "$(dirname "$f")")=${left}д"
  done
  chk certs "${bad:+сертификаты скоро истекут:$bad}"
}

backup_fresh() {
  systemctl is-enabled --quiet vpnkit-backup.timer 2>/dev/null || return 0
  local f age_h
  f=$(ls -1t /var/backups/vpnkit/*.tar.gz.age 2>/dev/null | head -1)
  if [ -z "$f" ]; then chk backup "нет ни одной резервной копии"; return; fi
  age_h=$(( ($(date +%s) - $(stat -c %Y "$f")) / 3600 ))
  [ "$age_h" -lt 30 ] && chk backup "" || chk backup "резервной копии ${age_h} ч (ждали < 30)"
}

kernel_awg() {  # P-62: ядро, в которое узел загрузится следующим, обязано иметь модуль AmneziaWG
  grep -q '^amneziawg ' /proc/modules || return 0
  local k
  k=$(for f in /boot/vmlinuz-*; do [ -e "$f" ] && echo "${f#/boot/vmlinuz-}"; done | sort -V | tail -1 || true)
  [ -n "$k" ] || return 0
  if [ -n "$(find "/lib/modules/$k" -name 'amneziawg.ko*' -print -quit 2>/dev/null)" ]; then chk kernel_awg ""
  else chk kernel_awg "в новом ядре $k нет модуля AmneziaWG — после перезагрузки туннели не поднимутся (dkms status amneziawg)"; fi
}

dpkg_broken() {  # недоустановленные пакеты: так выглядит упавшая сборка DKMS, а автообновления потом стоят
  local b
  b=$(dpkg -l 2>/dev/null | awk '$1 ~ /^i[FUH]$|^[rp]F$/ && ++n <= 5 {printf "%s ", $2}' || true)
  chk dpkg "${b:+недоустановлены пакеты: $b— dpkg --configure -a и смотреть ошибку}"
}

ru_nets() {  # набор российских сетей split (vpnkit_split; на старых узлах — split)
  local t n
  for t in vpnkit_split split; do
    nft list set inet $t ru_nets >/dev/null 2>&1 || continue
    n=$(nft -j list set inet $t ru_nets 2>/dev/null | python3 -c 'import sys,json;print(len(json.load(sys.stdin)["nftables"][1]["set"].get("elem",[])))' 2>/dev/null || echo 0)
    [ "$n" -gt 5000 ] && chk ru_nets "" || chk ru_nets "список российских сетей почти пуст ($n) — российский трафик идёт через каскад"
    return
  done
}

# каскад: entry видит в интернете адрес выхода
if [ -n "$EXIT_IP" ] && ip link show awg0 >/dev/null 2>&1; then
  ip=$(curl -s -m 10 --interface awg0 https://api.ipify.org)
  [ "$ip" = "$EXIT_IP" ] && chk cascade "" || chk cascade "каскад через awg0 не выходит на $EXIT_IP (получено: ${ip:-нет ответа})"
fi
if [ -n "$PEER_TUN" ]; then
  ping -c5 -i0.5 -W2 -q "$PEER_TUN" >/dev/null && chk peer "" || chk peer "соседний узел $PEER_NAME ($PEER_TUN) не отвечает по туннелю"
fi
# взгляд снаружи на соседа: TCP-порты, которые видят клиенты
for port in $PEER_TCP; do
  timeout 6 bash -c "exec 3<>/dev/tcp/$PEER_PUBLIC/$port" 2>/dev/null     && chk "peer_tcp:$port" "" || chk "peer_tcp:$port" "$PEER_NAME: порт $port снаружи не отвечает"
done
if systemctl is-active --quiet dnsmasq; then
  grep -q . <<<"$(dig +short +time=3 +tries=1 @127.0.0.1 ya.ru)" && chk dns "" || chk dns "dnsmasq не отвечает — у клиентов нет DNS"
fi
if [ -f /etc/vpnkit/mtproxy.env ]; then   # MTProxy установщика: telemt за nginx stream
  lp=$(sed -n 's/^LISTEN_PORT=//p' /etc/vpnkit/mtproxy.env)
  l=$(ss -Hltn "sport = :$lp")
  if [ -z "$l" ]; then chk mtproto "telemt не слушает 127.0.0.1:$lp — Telegram-прокси не работает"
  elif ! grep -q "default 127.0.0.1:$lp;" /etc/nginx/stream.d/vpnkit-443.conf 2>/dev/null; then chk mtproto "TCP/443 не ведёт в telemt"
  else chk mtproto ""; fi
fi
if command -v docker >/dev/null && grep -qx mtproxyl <<<"$(docker ps -a --format '{{.Names}}' 2>/dev/null)"; then
  grep -q '^Up' <<<"$(docker ps --filter name=mtproxyl --format '{{.Status}}')" && chk mtproto "" || chk mtproto "контейнер MTProxy (mtproxyl) не запущен"
fi
ru_nets; disk; certs; backup_fresh; kernel_awg; dpkg_broken

# --- состояние и оповещения --------------------------------------------------
declare -A FAILS ALERTED
if [ -f "$STATE" ]; then while read -r k f a; do FAILS[$k]=$f; ALERTED[$k]=$a; done < "$STATE"; fi
msgs_bad=(); msgs_good=()
for k in "${!RESULT[@]}"; do
  f=${FAILS[$k]:-0}; a=${ALERTED[$k]:-0}
  if [ -n "${RESULT[$k]}" ]; then
    f=$((f+1))
    if [ "$f" -ge "$FAILS_TO_ALERT" ] && [ "$a" = 0 ]; then msgs_bad+=("• ${RESULT[$k]}"); a=pending; fi
  else
    [ "$a" = 1 ] && msgs_good+=("• $k")
    f=0; a=0
  fi
  FAILS[$k]=$f; ALERTED[$k]=$a
done

sent_bad=0; sent_good=0
if [ ${#msgs_bad[@]} -gt 0 ]; then
  send "🔴 $NODE: проблема (держится $((FAILS_TO_ALERT*5)) мин)"$'\n'"$(printf '%s\n' "${msgs_bad[@]}")" && sent_bad=1
fi
if [ ${#msgs_good[@]} -gt 0 ]; then
  send "🟢 $NODE: восстановилось"$'\n'"$(printf '%s\n' "${msgs_good[@]}")" && sent_good=1
fi
: > "$STATE.new"
for k in "${!FAILS[@]}"; do
  a=${ALERTED[$k]}
  [ "$a" = pending ] && { [ $sent_bad = 1 ] && a=1 || a=0; }   # не доставили — попробуем в следующий раз
  echo "$k ${FAILS[$k]} $a" >> "$STATE.new"
done
mv "$STATE.new" "$STATE"
bad=0; for k in "${!RESULT[@]}"; do [ -n "${RESULT[$k]}" ] && { echo "✗ ${RESULT[$k]}"; bad=$((bad+1)); }; done
echo "проверок: ${#RESULT[@]}, не прошло: $bad"
