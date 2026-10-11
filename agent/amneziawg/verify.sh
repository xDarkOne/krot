# amneziawg: проверка — не только «файлы на месте», но и настоящий интерфейс.
: "${VK_TAG:=v3.1.20260906}" "${VK_TOOLS_TAG:=v3.1.20260812}"
want=${VK_TAG#v}
has() { grep -q -- "$2" <<<"$1"; }
v=$(modinfo -F version amneziawg 2>/dev/null) || die "modinfo не видит amneziawg"
[ "$v" = "$want" ] || die "версия модуля $v, ждали $want"
st=$(dkms status -m amneziawg -v "$want" -k "$(uname -r)" 2>/dev/null)
has "$st" installed || die "DKMS: модуль не установлен для $(uname -r): $st"
# поставленные, но ещё не загруженные ядра: без модуля перезагрузка оставит узел без AWG (P-62)
for d in /lib/modules/*/build; do
  k=${d%/build}; k=${k##*/}
  [ "$k" = "$(uname -r)" ] || [ ! -e "$d/include" ] && continue
  has "$(dkms status -m amneziawg -v "$want" -k "$k" 2>/dev/null)" installed \
    || say "ВНИМАНИЕ: под ядро $k модуль amneziawg не собран — перезагрузка в него оставит узел без AWG"
done
tv=$(awg --version 2>&1) || die "нет утилиты awg"
has "$tv" "$VK_TOOLS_TAG" || die "утилиты: $tv"
command -v awg-quick >/dev/null || die "нет awg-quick"
[ -f /usr/lib/systemd/system/awg-quick@.service ] || [ -f /lib/systemd/system/awg-quick@.service ] || die "нет awg-quick@.service"
# живой тест: создать интерфейс, задать параметры 3.1, удалить
ip link add vk-awgtest type amneziawg || die "ядро не создаёт интерфейс amneziawg"
k=$(awg genkey)
awg set vk-awgtest private-key <(echo "$k") jc 4 jmin 32 jmax 96 s1 10 s2 20 h1 100-200 h2 300-400 h3 500-600 h4 700-800 2>"$VK_RUN/awgtest.err" \
  || { ip link del vk-awgtest; die "awg set не принял параметры: $(cat "$VK_RUN/awgtest.err")"; }
ip link del vk-awgtest
say "проверка amneziawg: модуль $v, утилиты $VK_TOOLS_TAG, тестовый интерфейс создан и удалён"
