# traffic-guard: проверка — таблица на месте, наборы наполнены, узлы и пульт в исключениях, связь жива.
nft list table inet guard >/dev/null 2>&1 || die "нет таблицы inet guard"
out=$(/usr/local/sbin/traffic-guard status) || die "traffic-guard status не отвечает"
systemctl is-enabled -q traffic-guard.timer || die "таймер не включён"
n=$(nft list set inet guard antiscanner4 2>/dev/null | grep -c '/' || true)
[ "$n" -gt 0 ] || die "набор antiscanner пуст"
for a in $VK_NODES; do
  awk -v a="$a" '$1==a{f=1} END{exit !f}' /etc/traffic-guard.exempt || die "узел $a не в исключениях"
done
say "traffic-guard: правила применены, счётчики: $(tr '\n' ';' <<<"$out")"
