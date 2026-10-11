# cascade-awg0: проверка. Сервер — интерфейс и порт; клиент — рукопожатие, пинг и выход наружу через exit.
: "${VK_SIDE:?}" "${VK_PORT:?}" "${VK_PEER_TUN:?}"
systemctl is-active -q awg-quick@awg0 || die "awg-quick@awg0 не запущен"
ip link show awg0 >/dev/null 2>&1 || die "нет интерфейса awg0"
if [ "$VK_SIDE" = server ]; then
  [ "$(awg show awg0 listen-port)" = "$VK_PORT" ] || die "awg0 слушает не $VK_PORT"
  fw=$(nft list chain inet vpnkit_fw input)
  grep -q "udp dport $VK_PORT accept" <<<"$fw" || die "порт $VK_PORT не открыт в фаерволе"
  say "проверка сервера каскада: awg0 слушает udp/$VK_PORT"
else
  : "${VK_EXIT_IP:?}"
  ok=0
  for _ in $(seq 1 15); do
    hs=$(awg show awg0 latest-handshakes | awk '{print $2}')
    [ -n "$hs" ] && [ "$hs" -gt 0 ] && { ok=1; break; }
    sleep 2
  done
  [ $ok = 1 ] || die "нет рукопожатия с exit за 30 с (порт закрыт у хостера? параметры не совпали?)"
  ping -c3 -W2 -q "$VK_PEER_TUN" >/dev/null || die "рукопожатие есть, но $VK_PEER_TUN не пингуется"
  ip=$(curl -s -m10 --interface awg0 https://api.ipify.org) || true
  [ "$ip" = "$VK_EXIT_IP" ] || die "выход через awg0 не на exit (получено: ${ip:-нет ответа}, ждали $VK_EXIT_IP)"
  say "проверка клиента каскада: рукопожатие есть, ping $VK_PEER_TUN, выход через awg0 = $ip"
fi
