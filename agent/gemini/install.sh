# gemini: автовыбор живого Smart-DNS для доменов Gemini (ai-dns-switch).
# Подменённые адреса уходят в dnsmasq split-routing — их видят и клиенты AWG (DNS входа), и xray
# (у него первым DNS стоит 127.0.0.1, sniffing включён). Описание — docs/20-server-modules/gemini.md
: "${VK_INTERVAL_MIN:=10}"
[ -f /etc/dnsmasq.d/10-vpnkit-split.conf ] || die "нет split-routing (dnsmasq входа) — сначала split-routing"
apt_install curl python3 bind9-dnsutils
snapshot /etc/dnsmasq.d/ai-smart.conf /var/lib/ai-dns-switch.state
put /usr/local/bin/ai-dns-switch.sh 755 < "$VK_RUN/src/ai-dns-switch.sh"
cat <<'U' | put /etc/systemd/system/ai-dns-switch.service 644
[Unit]
Description=Выбор живого Smart-DNS для AI-доменов (vpnkit gemini)
After=network-online.target dnsmasq.service
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/ai-dns-switch.sh
U
cat <<U | put /etc/systemd/system/ai-dns-switch.timer 644
[Unit]
Description=Проверка Smart-DNS для AI-доменов (vpnkit gemini)

[Timer]
OnBootSec=3min
OnUnitActiveSec=${VK_INTERVAL_MIN}min
Persistent=true

[Install]
WantedBy=timers.target
U
systemctl daemon-reload
systemctl enable -q --now ai-dns-switch.timer
say "первый прогон переключалки (до пары минут: прокси проверяются по-настоящему)…"
if timeout 300 /usr/local/bin/ai-dns-switch.sh; then
  say "$(tail -1 /var/log/ai-dns-switch.log 2>/dev/null)"
else
  # не провал установки: DNS работает как раньше, переключалка повторит через интервал
  say "ни один провайдер Smart-DNS сейчас не годится — Gemini пока по обычному DNS; повтор через ${VK_INTERVAL_MIN} мин ($(tail -1 /var/log/ai-dns-switch.log 2>/dev/null))"
fi
