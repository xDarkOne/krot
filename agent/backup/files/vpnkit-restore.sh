#!/bin/bash
# vpnkit-restore.sh — восстановление узла krot из зашифрованного бэкапа (.tar.gz.age)
# на чистый Ubuntu/Debian VPS (у любого хостера) с автоадаптацией IP и сетевого интерфейса.
#
# Использование (на новом сервере от root):
#   bash vpnkit-restore.sh <архив.tar.gz.age> <age.key> [--dry-run] [--switch-dns]
#
# По умолчанию DNS в Cloudflare НЕ переключается автоматически до проверки сервисов:
# в конце скрипт покажет статус всех служб и команду для переключения DNS
# (или переключит сразу с ключом --switch-dns).
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

DRY=0
SWITCH_DNS=0
ARGS=()
for a in "$@"; do
  case "$a" in
    --dry-run) DRY=1 ;;
    --switch-dns) SWITCH_DNS=1 ;;
    -h|--help)
      sed -n '2,10p' "$0"; exit 0 ;;
    *) ARGS+=("$a") ;;
  esac
done

if [ "${#ARGS[@]}" -lt 2 ]; then
  echo "Использование: $0 <архив.tar.gz.age> <age.key> [--dry-run] [--switch-dns]" >&2
  exit 2
fi
ARCHIVE="${ARGS[0]}"
KEYFILE="${ARGS[1]}"
[ -f "$ARCHIVE" ] || { echo "ОШИБКА: архив не найден: $ARCHIVE" >&2; exit 1; }
[ -f "$KEYFILE" ] || { echo "ОШИБКА: ключ не найден: $KEYFILE" >&2; exit 1; }
if [ "$DRY" = 0 ] && [ "$(id -u)" != 0 ]; then
  echo "ОШИБКА: запускай от root (или с --dry-run для проверки)" >&2; exit 1
fi

log() { echo "==> $*"; }
warn() { echo "ВНИМАНИЕ: $*" >&2; }

if ! command -v age >/dev/null 2>&1; then
  [ "$DRY" = 0 ] || { echo "ОШИБКА: утилита age не установлена" >&2; exit 1; }
  log "Ставлю age..."
  apt-get update -qq && apt-get install -y -qq age
fi

NEW_IFACE=$(ip -4 route show default 2>/dev/null | awk '{for(i=1;i<NF;i++) if($i=="dev"){print $(i+1); exit}}')
NEW_IFACE=${NEW_IFACE:-eth0}
NEW_IP_CIDR=$(ip -4 -o addr show dev "$NEW_IFACE" scope global 2>/dev/null | awk '{print $4; exit}')
NEW_IP=${NEW_IP_CIDR%/*}
NEW_PLEN=${NEW_IP_CIDR#*/}; [ "$NEW_PLEN" = "$NEW_IP_CIDR" ] && NEW_PLEN=24
NEW_GW=$(ip -4 route show default 2>/dev/null | awk '{for(i=1;i<NF;i++) if($i=="via"){print $(i+1); exit}}')
NEW_IP6=$(ip -6 -o addr show dev "$NEW_IFACE" scope global 2>/dev/null | awk '{sub("/.*","",$4); print $4; exit}')
NEW_IP6=${NEW_IP6:--}

STAGE=$(mktemp -d)
chmod 700 "$STAGE"
trap 'rm -rf "$STAGE"' EXIT

log "Расшифровываю и проверяю архив $(basename "$ARCHIVE")..."
age -d -i "$KEYFILE" "$ARCHIVE" | tar xzf - -C "$STAGE"

NODE=""
ROLE=""
if [ -f "$STAGE/etc/vpnkit/relocate.conf" ]; then
  NODE=$(awk -F= '$1=="NODE"{print $2}' "$STAGE/etc/vpnkit/relocate.conf")
  ROLE=$(awk -F= '$1=="ROLE"{print $2}' "$STAGE/etc/vpnkit/relocate.conf")
fi
if [ -z "$NODE" ] && [ -f "$STAGE/etc/vpnkit/watch.conf" ]; then
  NODE=$(awk -F= '$1=="NODE"{print $2}' "$STAGE/etc/vpnkit/watch.conf")
  ROLE=$(awk -F= '$1=="ROLE"{print $2}' "$STAGE/etc/vpnkit/watch.conf")
fi
if [ -z "$NODE" ] && [ -f "$STAGE/meta/node.txt" ]; then
  NODE=$(awk -F= '$1=="NODE"{print $2}' "$STAGE/meta/node.txt")
fi
[ -n "$NODE" ] || { echo "ОШИБКА: не удалось определить имя узла в архиве" >&2; exit 1; }

OLD_IP=$(awk '$1=="address"{sub("/.*","",$2); print $2; exit}' "$STAGE/etc/network/interfaces" 2>/dev/null || true)
OLD_GW=$(awk '$1=="gateway"{print $2; exit}' "$STAGE/etc/network/interfaces" 2>/dev/null || true)
OLD_IFACE=$(awk '$1=="iface" && $2!="lo" && $3=="inet"{print $2; exit}' "$STAGE/etc/network/interfaces" 2>/dev/null || echo eth0)
OLD_IFACE=${OLD_IFACE:-eth0}

log "Архив узла: NODE=$NODE (ROLE=${ROLE:-?})"
log "Старый сервер: ${OLD_IP:-?} (gw ${OLD_GW:-?}, iface $OLD_IFACE)"
log "Новый сервер:  ${NEW_IP:-?}/$NEW_PLEN (gw ${NEW_GW:-?}, iface $NEW_IFACE, IPv6 $NEW_IP6)"

if [ -d "$STAGE/sqlite" ]; then
  for db in "$STAGE/sqlite"/*; do
    [ -f "$db" ] || continue
    if command -v sqlite3 >/dev/null 2>&1; then
      res=$(sqlite3 "$db" "PRAGMA integrity_check;" 2>&1 || true)
      log "SQLite $(basename "$db"): $res"
      [ "$res" = "ok" ] || { echo "ОШИБКА: повреждена БД $db" >&2; exit 1; }
    else
      log "SQLite $(basename "$db"): $(du -h "$db" | cut -f1)"
    fi
  done
fi

if [ -f "$STAGE/meta/versions.txt" ]; then
  log "Версии из бэкапа:"
  sed 's/^/   /' "$STAGE/meta/versions.txt"
fi

if [ "$DRY" = 1 ]; then
  log "DRY-RUN завершён успешно: архив расшифрован, структура и БД целы."
  exit 0
fi

[ -n "$NEW_IP" ] && [ -n "$NEW_GW" ] || { echo "ОШИБКА: не удалось определить IP/шлюз нового сервера на $NEW_IFACE" >&2; exit 1; }

ARCH=$(dpkg --print-architecture)
case "$ARCH" in
  amd64) XRAY_ASSET="Xray-linux-64.zip"; SB_RE='linux[_-]amd64\.deb$' ;;
  arm64) XRAY_ASSET="Xray-linux-arm64-v8a.zip"; SB_RE='linux[_-]arm64\.deb$' ;;
  *) echo "ОШИБКА: архитектура $ARCH не поддерживается" >&2; exit 1 ;;
esac

log "Устанавливаю базовые пакеты ОС..."
apt-get update -qq
BASE_PKGS=(
  age nftables nginx libnginx-mod-stream certbot python3-certbot fail2ban
  curl jq dnsutils sqlite3 busybox-static wireguard-tools
  dkms build-essential "linux-headers-$(uname -r)"
  chrony python3 python3-venv python3-pip qrencode openssl iproute2 ca-certificates
)
[ -d "$STAGE/etc/dnsmasq.d" ] && BASE_PKGS+=(dnsmasq)
[ -f "$STAGE/etc/danted.conf" ] && BASE_PKGS+=(dante-server)
apt-get install -y -qq "${BASE_PKGS[@]}"

# Системные пользователи, если соответствующие сервисы были в бэкапе
if [ -d "$STAGE/etc/mtproxyl-panel" ]; then
  id -u mtproxyl-panel >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin -d /var/lib/mtproxyl-panel mtproxyl-panel
fi
if [ -d "$STAGE/opt/mtproxyl-tgbot" ]; then
  id -u mtproxyl-tgbot >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin -d /opt/mtproxyl-tgbot mtproxyl-tgbot
fi
if [ -d "$STAGE/etc/telemt" ]; then
  id -u telemt >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin -d /var/lib/telemt telemt
fi

# Установка Xray (+ geoip/geosite) с проверкой SHA-256
if [ -d "$STAGE/usr/local/etc/xray" ] && [ ! -x /usr/local/bin/xray ]; then
  log "Скачиваю и проверяю Xray-core ($XRAY_ASSET)..."
  XTMP=$(mktemp -d)
  rel=$(curl -fsSL https://api.github.com/repos/XTLS/Xray-core/releases/latest)
  url=$(jq -r --arg n "$XRAY_ASSET" '.assets[] | select(.name==$n) | .browser_download_url' <<<"$rel")
  dgst=$(jq -r --arg n "$XRAY_ASSET" '.assets[] | select(.name==$n) | .digest // empty' <<<"$rel" | sed 's/^sha256://')
  curl -fsSL "$url" -o "$XTMP/xray.zip"
  if [ -n "$dgst" ]; then
    got=$(sha256sum "$XTMP/xray.zip" | awk '{print $1}')
    [ "$got" = "$dgst" ] || { echo "ОШИБКА SHA-256 для $XRAY_ASSET" >&2; exit 1; }
  fi
  python3 -c 'import zipfile,sys; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])' "$XTMP/xray.zip" "$XTMP/out"
  install -m 755 "$XTMP/out/xray" /usr/local/bin/xray
  mkdir -p /usr/local/share/xray
  install -m 644 "$XTMP/out/geoip.dat" /usr/local/share/xray/geoip.dat
  install -m 644 "$XTMP/out/geosite.dat" /usr/local/share/xray/geosite.dat
  rm -rf "$XTMP"
fi

# Установка sing-box (.deb) с проверкой SHA-256
if [ -d "$STAGE/etc/sing-box" ] && ! command -v sing-box >/dev/null 2>&1; then
  log "Скачиваю и ставлю sing-box (.deb)..."
  STMP=$(mktemp -d)
  rel=$(curl -fsSL https://api.github.com/repos/SagerNet/sing-box/releases/latest)
  url=$(jq -r --arg re "$SB_RE" '.assets[] | select(.name | test($re)) | .browser_download_url' <<<"$rel" | head -1)
  dgst=$(jq -r --arg re "$SB_RE" '.assets[] | select(.name | test($re)) | .digest // empty' <<<"$rel" | head -1 | sed 's/^sha256://')
  curl -fsSL "$url" -o "$STMP/sing-box.deb"
  if [ -n "$dgst" ]; then
    got=$(sha256sum "$STMP/sing-box.deb" | awk '{print $1}')
    [ "$got" = "$dgst" ] || { echo "ОШИБКА SHA-256 для sing-box.deb" >&2; exit 1; }
  fi
  dpkg -i "$STMP/sing-box.deb" || apt-get install -f -y -qq
  rm -rf "$STMP"
fi

USE_NETPLAN=0
if ls /etc/netplan/*.yaml >/dev/null 2>&1 && systemctl is-active --quiet systemd-networkd 2>/dev/null; then
  USE_NETPLAN=1
fi
[ -f /etc/network/interfaces ] && cp -a /etc/network/interfaces /root/interfaces.new-vps.orig

log "Распаковываю конфиги и файлы узла..."
for top in etc usr opt var root; do
  [ -d "$STAGE/$top" ] || continue
  if [ "$top" = root ] && [ -f /root/.ssh/authorized_keys ] && [ -f "$STAGE/root/.ssh/authorized_keys" ]; then
    cat /root/.ssh/authorized_keys "$STAGE/root/.ssh/authorized_keys" | sort -u > "$STAGE/root/.ssh/authorized_keys.merged"
    mv "$STAGE/root/.ssh/authorized_keys.merged" "$STAGE/root/.ssh/authorized_keys"
    chmod 600 "$STAGE/root/.ssh/authorized_keys"
  fi
  cp -a "$STAGE/$top/." "/$top/"
done

if [ -d "$STAGE/sqlite" ]; then
  log "Восстанавливаю базы SQLite..."
  for db in "$STAGE/sqlite"/*; do
    [ -f "$db" ] || continue
    bn=$(basename "$db")
    target=$(echo "$bn" | sed 's/^_/\//; s/_/\//g')
    case "$bn" in
      _var_lib_vpn-bot_stats.db) target=/var/lib/vpn-bot/stats.db ;;
      _var_lib_vpnkit_panel.db) target=/var/lib/vpnkit/panel.db ;;
      _etc_csqtt_csqtt.db) target=/etc/csqtt/csqtt.db ;;
    esac
    mkdir -p "$(dirname "$target")"
    rm -f "${target}-wal" "${target}-shm"
    cp -a "$db" "$target"
    chk=$(sqlite3 "$target" "PRAGMA integrity_check;")
    [ "$chk" = ok ] || { echo "ОШИБКА проверки $target: $chk" >&2; exit 1; }
    log "  восстановлена: $target"
  done
fi

AWG_SRC=$(ls -d /usr/src/amneziawg-* 2>/dev/null | sort -V | tail -1 || true)
if [ -n "$AWG_SRC" ] && ! modprobe amneziawg 2>/dev/null; then
  AWG_VER=${AWG_SRC##*/amneziawg-}
  log "Собираю модуль ядра amneziawg/$AWG_VER через DKMS..."
  st=$(dkms status "amneziawg/$AWG_VER" 2>/dev/null || true)
  if ! grep -q installed <<<"$st"; then
    dkms add -m amneziawg -v "$AWG_VER" 2>/dev/null || true
    dkms build -m amneziawg -v "$AWG_VER"
    dkms install -m amneziawg -v "$AWG_VER"
  fi
  modprobe amneziawg
fi

if [ -f /opt/mtproxyl-tgbot/requirements.txt ] && [ ! -x /opt/mtproxyl-tgbot/venv/bin/python ]; then
  log "Создаю venv для mtproxyl-tgbot..."
  python3 -m venv /opt/mtproxyl-tgbot/venv
  /opt/mtproxyl-tgbot/venv/bin/pip install -q -r /opt/mtproxyl-tgbot/requirements.txt
fi
[ -d /opt/mtproxyl-tgbot ] && chown -R mtproxyl-tgbot:mtproxyl-tgbot /opt/mtproxyl-tgbot 2>/dev/null || true
[ -d /var/lib/mtproxyl-panel ] && chown -R mtproxyl-panel:mtproxyl-panel /var/lib/mtproxyl-panel /etc/mtproxyl-panel 2>/dev/null || true
[ -x /opt/mtproxyl/mtproxyl.sh ] && ln -sf /opt/mtproxyl/mtproxyl.sh /usr/local/bin/mtproxyl
[ -f /usr/local/etc/xray/config.json ] && { chown root:nogroup /usr/local/etc/xray/config.json 2>/dev/null || true; chmod 640 /usr/local/etc/xray/config.json 2>/dev/null || true; }

if [ "$NEW_IFACE" != "$OLD_IFACE" ]; then
  log "Адаптирую имя сетевого интерфейса: $OLD_IFACE -> $NEW_IFACE"
  for f in /etc/nftables.conf /etc/nftables.d/*.nft /etc/network/interfaces /etc/network/interfaces.d/ipv6-link \
           /usr/local/sbin/zapret2-scope.sh /usr/local/sbin/vpnkit-watch.sh \
           /usr/local/sbin/vpnkit-autoreip /opt/mtproxyl/settings.conf; do
    [ -f "$f" ] || continue
    sed -i "s/\b${OLD_IFACE}\b/${NEW_IFACE}/g" "$f"
  done
fi

if [ -f /etc/network/interfaces ] && [ "$USE_NETPLAN" = 0 ]; then
  sed -i -E "s|(address[[:space:]]+[0-9.]+)/[0-9]+|\1/${NEW_PLEN}|" /etc/network/interfaces
fi

sysctl --system >/dev/null 2>&1 || true

# Перенастройка IP через relocate (DNS переключается только с --switch-dns)
CF_TMP=""
if [ "$SWITCH_DNS" = 0 ] && [ -f /etc/vpnkit/cloudflare.conf ]; then
  CF_TMP=/etc/vpnkit/cloudflare.conf.restore-hold
  mv /etc/vpnkit/cloudflare.conf "$CF_TMP"
fi
if [ -x /usr/local/sbin/relocate ] && [ -n "${OLD_IP:-}" ] && [ "$OLD_IP" != "$NEW_IP" ]; then
  log "Переписываю IP: $OLD_IP -> $NEW_IP (шлюз $NEW_GW)..."
  NEW_SELF="$NEW_IP" NEW_GW="$NEW_GW" NEW_SELF6="$NEW_IP6" /usr/local/sbin/relocate -y || warn "relocate вернул ненулевой код"
fi
[ -n "$CF_TMP" ] && [ -f "$CF_TMP" ] && mv "$CF_TMP" /etc/vpnkit/cloudflare.conf

log "Проверяю конфигурации и запускаю службы..."
systemctl daemon-reload
[ -f /etc/nftables.conf ] && nft -c -f /etc/nftables.conf && nft -f /etc/nftables.conf
[ -d /etc/nginx ] && nginx -t && systemctl enable --now nginx
[ -f /usr/local/etc/xray/config.json ] && /usr/local/bin/xray run -test -config /usr/local/etc/xray/config.json && systemctl enable --now xray
[ -f /etc/sing-box/config.json ] && sing-box check -c /etc/sing-box/config.json && systemctl enable --now sing-box

for u in nftables fail2ban awg-quick@awg0 awg-quick@awg1 awg-quick@awg2 dnsmasq danted \
         vpnkit-cascade-xray vpnkit-cascade-tun vpnkit-cascade-watch \
         telemt mtproxyl-telemt mtproxyl-zapret2 mtproxyl-panel mtproxyl-tgbot \
         vpn-bot vpn-panel csqtt aivpn-server traffic-guard \
         vpnkit-watch.timer vpnkit-backup.timer vpnkit-gen.timer; do
  out=$(systemctl list-unit-files "$u*" --no-legend 2>/dev/null || true)
  if [ -n "$out" ]; then
    systemctl enable --now "$u" 2>/dev/null || true
  fi
done

echo
log "=== ИТОГОВАЯ ПРОВЕРКА ==="
systemctl --failed --no-pager || true
[ -x /usr/local/sbin/vpnkit-watch.sh ] && /usr/local/sbin/vpnkit-watch.sh || true
echo
if [ "$SWITCH_DNS" = 0 ] && [ -n "${OLD_IP:-}" ] && [ "$OLD_IP" != "$NEW_IP" ]; then
  echo "DNS ещё НЕ переключён. После проверки выполни:"
  echo "  vpnkit-dns move $OLD_IP $NEW_IP --apply"
fi
echo "ВАЖНО: удали приватный ключ с сервера после восстановления:"
echo "  shred -u '$KEYFILE'"
