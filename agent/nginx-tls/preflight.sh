# nginx-tls: до изменений — имена смотрят на этот узел, порты не заняты чужими.
: "${VK_NAMES:?}" "${VK_HOST:?}" "${VK_ROLE:?}" "${VK_XHTTP_PORT:=4443}"
bad=0
for n in $VK_NAMES; do
  a=$(getent ahostsv4 "$n" | awk '{print $1}' | sort -u | tr '\n' ' ')
  case " $a" in
    *" $VK_HOST "*) say "$n → $VK_HOST" ;;
    *) echo "  ✗ $n указывает на «${a:-ничего}», а не на $VK_HOST: сертификат не выпустится" >&2; bad=1 ;;
  esac
done
ports="80 443"; [ "$VK_ROLE" = exit ] || ports="$ports $VK_XHTTP_PORT"
for p in $ports; do
  l=$(ss -Hltnp "sport = :$p")   # P-47: без совпадений grep под pipefail валит шаг
  who=$(grep -o 'users:(("[^"]*"' <<<"$l" | cut -d'"' -f2 | sort -u | tr '\n' ' ' || true)
  case "$who" in ""|"nginx ") ;; *) echo "  ✗ TCP $p занят: $who" >&2; bad=1 ;; esac
done
if [ -f /etc/nginx/nginx.conf ] && grep -qE '^\s*stream\s*\{' /etc/nginx/nginx.conf \
   && ! grep -q 'stream.d' /etc/nginx/nginx.conf; then
  echo "  ✗ в nginx.conf свой блок stream без include stream.d — разбор 443 по SNI некуда положить" >&2; bad=1
fi
[ "$bad" = 0 ] || die "условия для nginx-tls не выполнены (ничего не менялось)"
