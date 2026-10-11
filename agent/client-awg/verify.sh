# client-awg: проверка входа (без клиента: интерфейс, порт, правила, DNS на шлюзе).
: "${VK_IFACE:?}" "${VK_GW:?}" "${VK_PORT:?}" "${VK_RULE_MAIN:?}" "${VK_RULE_CASCADE:?}" "${VK_CASCADE:=1}"
I=$VK_IFACE
systemctl is-active -q "awg-quick@$I" || die "awg-quick@$I не запущен"
[ "$(awg show "$I" listen-port)" = "$VK_PORT" ] || die "$I слушает не $VK_PORT"
grep -qF "$VK_GW/" <<<"$(ip -br addr show "$I")" || die "у $I нет адреса $VK_GW"
if [ "$VK_CASCADE" = 1 ]; then
  r=$(ip rule)
  grep -qE "^$VK_RULE_MAIN:" <<<"$r" || die "нет ip rule $VK_RULE_MAIN (РФ напрямую)"
  grep -qE "^$VK_RULE_CASCADE:" <<<"$r" || die "нет ip rule $VK_RULE_CASCADE (каскад)"
  s=$(nft list chain inet vpnkit_split prerouting)
  grep -q "iifname \"$I\"" <<<"$s" || die "нет правил split для $I"
fi
f=$(nft list chain inet vpnkit_fw input)
grep -q "udp dport $VK_PORT accept" <<<"$f" || die "порт $VK_PORT не открыт"
a=$(dig +short +time=3 +tries=2 @"$VK_GW" ya.ru A | grep -E '^[0-9.]+$' | head -1)
[ -n "$a" ] || die "DNS на $VK_GW не отвечает"
say "проверка входа $I: udp/$VK_PORT, $([ "$VK_CASCADE" = 1 ] && echo "правила $VK_RULE_MAIN/$VK_RULE_CASCADE" || echo "один сервер, всё напрямую"), DNS $VK_GW отвечает"
