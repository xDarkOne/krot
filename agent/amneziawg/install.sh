# amneziawg: модуль ядра (DKMS) и утилиты AmneziaWG из git по пину.
# Параметры: VK_TAG (модуль), VK_TOOLS_TAG (утилиты). Описание — docs/20-server-modules/amneziawg.md
: "${VK_TAG:=v3.1.20260906}" "${VK_TOOLS_TAG:=v3.1.20260812}"
want=${VK_TAG#v}
kr=$(uname -r)

# ядра, под которые модуль нужен: работающее и все поставленные с заголовками.
# Новое ядро без модуля — это каскад, который не поднимется после перезагрузки (P-62).
kernels() {
  echo "$kr"
  for d in /lib/modules/*/build; do
    k=${d%/build}; k=${k##*/}
    [ "$k" != "$kr" ] && [ -e "$d/include" ] && echo "$k"
  done
  true
}
dkms_ok() { grep -q installed <<<"$(dkms status -m amneziawg -v "$want" -k "$1" 2>/dev/null)"; }

cur=$(modinfo -F version amneziawg 2>/dev/null || true)
if [ "$cur" = "$want" ] && command -v awg >/dev/null && grep -q -- "$VK_TOOLS_TAG" <<<"$(awg --version 2>/dev/null)"; then
  missing=""
  for k in $(kernels); do dkms_ok "$k" || missing="$missing $k"; done
  if [ -z "$missing" ]; then
    say "AmneziaWG $want и утилиты $VK_TOOLS_TAG уже стоят (ядра: $(kernels | tr '\n' ' '))"
    exit 0
  fi
  say "AmneziaWG $want стоит, но нет модуля для ядер:$missing — пересобираю"
fi

apt_install build-essential dkms git libmnl-dev pkg-config "linux-headers-$kr"

b=$VK_RUN/build
rm -rf "$b"; mkdir -p "$b"
say "исходники: модуль $VK_TAG, утилиты $VK_TOOLS_TAG"
git -c advice.detachedHead=false clone -q --depth 1 -b "$VK_TAG" https://github.com/amnezia-vpn/amneziawg-linux-kernel-module "$b/kmod"
git -c advice.detachedHead=false clone -q --depth 1 -b "$VK_TOOLS_TAG" https://github.com/amnezia-vpn/amneziawg-tools "$b/tools"

# Ubuntu перенесла в 7.0.0-38 новую сигнатуру setup_udp_tunnel_sock(struct sock *),
# а compat.h модуля судит по номеру ядра (< 7.1.5) и подставляет старую — сборка падает.
# Патч смотрит в заголовки. Если в новом теге это уже исправлено, патч не ляжет — собираем как есть.
p=$VK_RUN/src/ubuntu-udp-tunnel-sk.patch
if [ -f "$p" ] && git -C "$b/kmod" apply --check "$p" 2>/dev/null; then
  git -C "$b/kmod" apply "$p"
  say "патч совместимости с ядрами Ubuntu наложен"
else
  say "патч совместимости не нужен или не ложится на $VK_TAG — собираю как есть"
fi
# версия в modinfo = тег: в теге v3.1.20260906 version.h остался «3.1.20260812»
sed -i "s/^#define WIREGUARD_VERSION .*/#define WIREGUARD_VERSION \"$want\"/" "$b/kmod/src/version.h"

# --- модуль через DKMS. Версия DKMS = тег (не «1.0.0», которое ничего не говорит — P-12)
if grep -q . <<<"$(dkms status -m amneziawg -v "$want" 2>/dev/null)"; then
  say "DKMS amneziawg/$want уже зарегистрирован — пересобираю"
  dkms remove -m amneziawg -v "$want" --all >/dev/null 2>&1 || true
fi
sed -i "s/^PACKAGE_VERSION=.*/PACKAGE_VERSION=\"$want\"/" "$b/kmod/src/dkms.conf"
snapshot "/usr/src/amneziawg-$want"
make -s -C "$b/kmod/src" dkms-install WIREGUARD_VERSION="$want" >/dev/null 2>"$VK_RUN/dkms-install.log"
echo "dkms amneziawg/$want" >> "$VK_RUN/created.list"
dkms add -m amneziawg -v "$want" >/dev/null 2>>"$VK_RUN/dkms-install.log"
warn=""
for k in $(kernels); do
  say "сборка модуля под $k (DKMS, 1–5 минут)…"
  dkms build -m amneziawg -v "$want" -k "$k" > "$VK_RUN/dkms-build-$k.log" 2>&1 || dkms_failed=1
  if [ -z "${dkms_failed:-}" ]; then
    # Старый файл модуля под другим именем (на 24.04 прежняя сборка — .ko, новая — .ko.zst)
    # остаётся рядом, и modprobe берёт его. Все варианты — в снимок (откат вернёт старое и
    # уберёт новое, даже если dkms remove не справится), затем убираем.
    d=/lib/modules/$k/updates/dkms
    for f in "$d/amneziawg.ko" "$d/amneziawg.ko.zst" "$d/amneziawg.ko.xz" "$d/amneziawg.ko.gz"; do
      snapshot "$f"; rm -f "$f"
    done
    if dkms install -m amneziawg -v "$want" -k "$k" --force >/dev/null 2>>"$VK_RUN/dkms-install.log"; then
      depmod -a "$k" 2>/dev/null || true
      continue
    fi
    # не встал — вернуть этому ядру прежний файл модуля, чтобы оно не осталось совсем без AWG
    for f in "$d/amneziawg.ko" "$d/amneziawg.ko.zst" "$d/amneziawg.ko.xz" "$d/amneziawg.ko.gz"; do
      [ -e "$VK_RUN/files$f" ] && cp -a "$VK_RUN/files$f" "$f"
    done
    depmod -a "$k" 2>/dev/null || true
  fi
  unset dkms_failed
  tail -20 "$VK_RUN/dkms-build-$k.log" >&2
  find "/var/lib/dkms/amneziawg/$want" -name make.log -exec tail -20 {} \; >&2 2>/dev/null || true
  [ "$k" = "$kr" ] && die "DKMS не собрал модуль под работающее ядро $k"
  warn="$warn $k"
done
[ -z "$warn" ] || say "ВНИМАНИЕ: модуль не собрался под ядра:$warn — не перезагружайтесь в них, пока это не исправлено"

# Прежние версии модуля в DKMS (например «1.0.0», P-12) с AUTOINSTALL=yes пытаются
# собраться под каждое новое ядро, падают и роняют установку ядра (P-62). Не удаляем их
# (откат этого шага вернул бы узел вовсе без модуля), а выключаем им автосборку — файл в снимке.
for v in $(dkms status -m amneziawg 2>/dev/null | sed -n 's#^amneziawg/\([^,]*\),.*#\1#p' | sort -u); do
  c=/usr/src/amneziawg-$v/dkms.conf
  [ "$v" = "$want" ] || [ ! -f "$c" ] && continue
  grep -q '^AUTOINSTALL=yes' "$c" || continue
  snapshot "$c"
  sed -i 's/^AUTOINSTALL=yes/AUTOINSTALL=no/' "$c"
  say "прежней версии DKMS amneziawg/$v выключена автосборка под новые ядра"
done

# Ядро, чья установка упала на сборке модуля, осталось недонастроенным (iF/iU, без initrd).
# Теперь модуль собирается — достраиваем, иначе следующая загрузка уйдёт в ядро без initrd.
stuck=$(dpkg -l 2>/dev/null | awk '$1 ~ /^i[FU]$/ && $2 ~ /^linux-/ {printf "%s ", $2}' || true)
if [ -n "$stuck" ]; then
  say "недонастроенные пакеты ядра: $stuck— dpkg --configure -a"
  DEBIAN_FRONTEND=noninteractive dpkg --configure -a > "$VK_RUN/dpkg-configure.log" 2>&1 \
    || { tail -20 "$VK_RUN/dpkg-configure.log" >&2; die "dpkg --configure -a не прошёл"; }
  say "пакеты ядра достроены"
fi
modprobe amneziawg
say "модуль ядра: $(modinfo -F version amneziawg)"

# --- утилиты: сначала во временный каталог, чтобы знать точный список файлов (для отката)
make -s -C "$b/tools/src" >/dev/null 2>"$VK_RUN/tools-build.log" || { tail -20 "$VK_RUN/tools-build.log" >&2; die "утилиты не собрались"; }
make -s -C "$b/tools/src" install DESTDIR="$b/stage" PREFIX=/usr WITH_WGQUICK=yes WITH_SYSTEMDUNITS=yes WITH_BASHCOMPLETION=yes >/dev/null
( cd "$b/stage" && find . -type f -o -type l ) | sed 's#^\.##' | while IFS= read -r f; do
  snapshot "$f"
  mkdir -p "$(dirname "$f")"
  cp -a "$b/stage$f" "$f"
  echo "$f" >> "$VK_RUN/changed.list"
done
systemctl daemon-reload
say "утилиты: $(awg --version | head -1)"
rm -rf "$b/kmod/.git" "$b/tools/.git"
