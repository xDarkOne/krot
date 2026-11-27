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
  command -v ping >/dev/null || die "нет утилиты ping (пакет iputils-ping), проверка связности невозможна"
  ok=0
  for _ in $(seq 1 15); do
    hs=$(awg show awg0 latest-handshakes | awk '{print $2}')
    [ -n "$hs" ] && [ "$hs" -gt 0 ] && { ok=1; break; }
    sleep 2
  done
  [ $ok = 1 ] || die "нет рукопожатия с exit за 30 с (порт закрыт у хостера? параметры не совпали?)"
  ping -I awg0 -c3 -W2 -q "$VK_PEER_TUN" >/dev/null || die "рукопожатие есть, но $VK_PEER_TUN не пингуется"
  say "проверка клиента каскада: рукопожатие есть, ping $VK_PEER_TUN через awg0 прошёл"
  if /usr/local/lib/vpnkit/cascade/watch /etc/vpnkit/cascade/watch.json probe awg0; then
    say "HTTPS через awg0 выходит с адреса exit"
  else
    say "ВНИМАНИЕ: не подтверждён публичный выход через awg0. Проверьте NAT, forwarding и доступность HTTPS с exit."
  fi
fi

if [ "$VK_SIDE" = client ]; then
  systemctl is-active -q vpnkit-cascade-watch.service || die "watchdog каскада не запущен"
fi
if [ -n "${VK_REALITY_NAME:-}" ]; then
  systemctl is-active -q vpnkit-cascade-xray.service || die "Xray резерва не запущен"
  if [ "$VK_SIDE" = server ]; then
    for _ in $(seq 1 5); do
      pid=$(systemctl show -p MainPID --value vpnkit-cascade-xray.service)
      socket=$(ss -Hltp "sport = :$VK_REALITY_PORT")
      if [ "$pid" != 0 ] && grep -qF "pid=$pid," <<<"$socket"; then
        say "Xray Reality принимает подключения на TCP/$VK_REALITY_PORT"
        exit 0
      fi
      sleep 1
    done
    die "Xray Reality не слушает TCP/$VK_REALITY_PORT"
  else
    systemctl is-active -q vpnkit-cascade-tun.service || die "sing-box резерва не запущен"
    ok=0
    for _ in $(seq 1 4); do
      if /usr/local/lib/vpnkit/cascade/watch /etc/vpnkit/cascade/watch.json probe sbtun; then
        ok=1
        break
      fi
      sleep 2
    done
    [ "$ok" = 1 ] || die "реальный HTTPS-выход через sbtun не подтверждён"
    say "резерв sbtun: HTTPS выходит с адреса exit"
  fi
fi
