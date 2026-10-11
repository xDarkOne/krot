# vpnkit agent lib — подключается перед каждым шагом модуля (bash).
# Переменные от пульта: VK_MODULE, VK_RUN (каталог прогона), VK_* — параметры модуля.
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
export DEBIAN_FRONTEND=noninteractive
umask 022

: "${VK_MODULE:?}" "${VK_RUN:?}"
mkdir -p "$VK_RUN/files"
chmod 700 "$VK_RUN"

say()  { echo "  • $*"; }
die()  { echo "  ✗ $*" >&2; exit 1; }

# snapshot <путь…> — запомнить состояние файла ДО изменения (один раз за прогон).
# Несуществующий файл помечается: откат его удалит.
snapshot() {
  local p
  for p in "$@"; do
    grep -qxF "$p" "$VK_RUN/snapshot.list" 2>/dev/null && continue
    if [ -e "$p" ]; then
      mkdir -p "$VK_RUN/files$(dirname "$p")"
      cp -a "$p" "$VK_RUN/files$p"
      echo "$p" >> "$VK_RUN/snapshot.list"
    else
      echo "$p" >> "$VK_RUN/snapshot.absent"
      echo "$p" >> "$VK_RUN/snapshot.list"
    fi
  done
}

# put <путь> <права> — записать stdin в файл атомарно (со снимком). Не меняет, если содержимое то же.
put() {
  local p=$1 mode=$2 tmp
  tmp=$(mktemp "$(dirname "$p")/.vk.XXXXXX" 2>/dev/null || { mkdir -p "$(dirname "$p")"; mktemp "$(dirname "$p")/.vk.XXXXXX"; })
  cat > "$tmp"
  if [ -f "$p" ] && cmp -s "$tmp" "$p"; then rm -f "$tmp"; return 0; fi
  snapshot "$p"
  chmod "$mode" "$tmp"
  mv "$tmp" "$p"
  echo "$p" >> "$VK_RUN/changed.list"
}

# changed <префикс пути> — менял ли этот прогон файлы под этим путём (по changed.list от put).
# Перезапуск служб — только если поменялось их: повторная установка и update не должны
# рвать клиентов (стенд 03.10: dnsmasq и sing-box перезапускались без изменений).
changed() { grep -q "^$1" "$VK_RUN/changed.list" 2>/dev/null; }

# restore_snapshot — вернуть все файлы из снимка (используется rollback).
restore_snapshot() {
  local p
  [ -f "$VK_RUN/snapshot.list" ] || return 0
  while IFS= read -r p; do
    if grep -qxF "$p" "$VK_RUN/snapshot.absent" 2>/dev/null; then
      rm -rf -- "$p"
    elif [ -e "$VK_RUN/files$p" ]; then
      mkdir -p "$(dirname "$p")"
      cp -a "$VK_RUN/files$p" "$p"
    fi
  done < "$VK_RUN/snapshot.list"
}

apt_install() {
  local missing=() p
  for p in "$@"; do dpkg -s "$p" >/dev/null 2>&1 || missing+=("$p"); done
  [ ${#missing[@]} -eq 0 ] && return 0
  say "ставлю пакеты: ${missing[*]}"
  apt-get update -q >/dev/null
  NEEDRESTART_SUPPRESS=1 apt-get install -y -q --no-install-recommends "${missing[@]}" >/dev/null 2>&1 \
    || die "apt-get install ${missing[*]} не удался"
  printf '%s\n' "${missing[@]}" >> "$VK_RUN/packages.installed"
}
