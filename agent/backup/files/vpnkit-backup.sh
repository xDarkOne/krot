#!/bin/bash
# vpnkit-backup — зашифрованная резервная копия состояния узла.
#
# Шифрование — открытыми ключами из /etc/vpnkit/backup.recipients (ключ пульта
# на ПК с пультом). Узел копию СОЗДАЁТ, но прочитать её не может.
# Настройки — /etc/vpnkit/backup.conf:
#   NODE=ru                     имя узла в именах файлов
#   PATHS="..."                 что копировать (несуществующее пропускается)
#   EXCLUDES="..."              что не копировать (шаблоны tar)
#   SQLITE="..."                базы SQLite: копируются через backup API (целостно)
#   KEEP=7                      сколько своих копий держать
#   PEER=10.100.0.1             соседний узел по SSH (туннель): туда кладётся копия,
#                               оттуда забирается копия соседа (пусто — не обмениваться)
#   PEER_KEEP=7
# Каталоги: /var/backups/vpnkit (свои), /var/backups/vpnkit-peer (копии соседа).
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077
CONF=/etc/vpnkit/backup.conf
RCPT=/etc/vpnkit/backup.recipients
OUT=/var/backups/vpnkit
PEER_DIR=/var/backups/vpnkit-peer
log() { logger -t vpnkit-backup "$*"; echo "$*"; }
fail() { log "ОШИБКА: $*"; exit 1; }

# shellcheck source=/dev/null
. "$CONF"
: "${NODE:?}" "${PATHS:?}" "${KEEP:=7}" "${PEER:=}" "${PEER_KEEP:=7}" "${EXCLUDES:=}" "${SQLITE:=}"
[ -s "$RCPT" ] || fail "нет $RCPT — некому шифровать"
mkdir -p "$OUT" "$PEER_DIR"; chmod 700 "$OUT" "$PEER_DIR"
rm -f "$OUT"/*.part "$PEER_DIR"/*.part

ts=$(date +%Y%m%d-%H%M%S)
name="$NODE-$ts"
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT

# 1. целостные копии баз SQLite (живые базы с WAL нельзя просто tar-ить)
mkdir -p "$tmp/sqlite"
for db in $SQLITE; do
  [ -f "$db" ] || continue
  python3 - "$db" "$tmp/sqlite/$(echo "$db" | tr / _)" <<'EOF'
import sqlite3, sys
src = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
dst = sqlite3.connect(sys.argv[2])
src.backup(dst); dst.close(); src.close()
EOF
done

# 2. список путей и исключений (шаблоны НЕ раскрывать как маски файлов)
set -f
list=()
for p in $PATHS; do [ -e "$p" ] && list+=("$p"); done
ex=()
for e in $EXCLUDES; do ex+=("--exclude=$e"); done
set +f

# 3. архив → шифрование; открытый текст на диск не пишется
set +o pipefail
tar czf - --warning=no-file-changed "${ex[@]}" "${list[@]}" -C "$tmp" sqlite 2>"$tmp/tar.err" | age -R "$RCPT" -o "$OUT/$name.tar.gz.age.part"
rc=("${PIPESTATUS[@]}")
set -o pipefail
{ grep -vE 'Removing leading|socket ignored' "$tmp/tar.err" || true; } | head -5 | while read -r l; do log "tar: $l"; done
# tar: 0 — хорошо, 1 — файл менялся во время чтения (архив цел), больше — ошибка
[ "${rc[0]}" -le 1 ] || fail "tar завершился с кодом ${rc[0]}"
[ "${rc[1]}" -eq 0 ] || fail "age завершился с кодом ${rc[1]}"
mv "$OUT/$name.tar.gz.age.part" "$OUT/$name.tar.gz.age"
sha256sum "$OUT/$name.tar.gz.age" | awk '{print $1}' > "$OUT/$name.sha256"
size=$(du -h "$OUT/$name.tar.gz.age" | cut -f1)

# 4. ротация своих копий
ls -1t "$OUT/$NODE"-*.tar.gz.age 2>/dev/null | tail -n +$((KEEP+1)) | while read -r f; do rm -f "$f" "${f%.tar.gz.age}.sha256"; done

# 5. обмен с соседом: своя копия — к нему, его последняя — к себе
peer_msg=""
if [ -n "$PEER" ]; then
  SSHP=(ssh -o BatchMode=yes -o ConnectTimeout=10 "root@$PEER")
  "${SSHP[@]}" "umask 077; mkdir -p $PEER_DIR && cat > $PEER_DIR/$name.tar.gz.age.part && mv $PEER_DIR/$name.tar.gz.age.part $PEER_DIR/$name.tar.gz.age" < "$OUT/$name.tar.gz.age" \
    || fail "не удалось положить копию соседу $PEER"
  rsum=$("${SSHP[@]}" "sha256sum $PEER_DIR/$name.tar.gz.age" | awk '{print $1}')
  [ "$rsum" = "$(cat "$OUT/$name.sha256")" ] || fail "копия у соседа не совпала по sha256"
  "${SSHP[@]}" "ls -1t $PEER_DIR/$NODE-*.tar.gz.age | tail -n +$((PEER_KEEP+1)) | xargs -r rm -f"
  peer_msg="копия у соседа $PEER сверена по sha256"
  latest=$("${SSHP[@]}" "ls -1t /var/backups/vpnkit/*.tar.gz.age 2>/dev/null | head -1" || true)
  if [ -n "$latest" ] && [ ! -f "$PEER_DIR/$(basename "$latest")" ]; then
    b=$(basename "$latest")
    "${SSHP[@]}" "cat $latest" > "$PEER_DIR/$b.part"
    want=$("${SSHP[@]}" "cat ${latest%.tar.gz.age}.sha256")
    [ "$(sha256sum "$PEER_DIR/$b.part" | awk '{print $1}')" = "$want" ] || fail "копия соседа $b пришла повреждённой"
    mv "$PEER_DIR/$b.part" "$PEER_DIR/$b"
    peer_msg="$peer_msg; забрана копия соседа $b"
  fi
  ls -1t "$PEER_DIR"/*.tar.gz.age 2>/dev/null | grep -v "/$NODE-" | tail -n +$((PEER_KEEP+1)) | xargs -r rm -f
fi
log "готово: $name.tar.gz.age ($size, ${#list[@]} путей); $peer_msg"
