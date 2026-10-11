# gemini: таймер жив, dnsmasq отвечает, конфиг подмены (если уже выбран провайдер) проходит проверку.
systemctl is-active -q ai-dns-switch.timer || die "таймер ai-dns-switch не запущен"
grep -q . <<<"$(dig +short +time=3 +tries=1 @127.0.0.1 ya.ru)" || die "dnsmasq не отвечает на 127.0.0.1"
if [ -f /etc/dnsmasq.d/ai-smart.conf ]; then
  dnsmasq --test >/dev/null 2>&1 || die "dnsmasq --test не принимает ai-smart.conf"
  p=$(sed -n 's/^# Провайдер: \([^ ]*\).*/\1/p' /etc/dnsmasq.d/ai-smart.conf)
  a=$(dig +short +time=3 +tries=1 @127.0.0.1 gemini.google.com | grep -E '^[0-9.]+$' | head -1 || true)
  say "проверка gemini: провайдер ${p:-?}, gemini.google.com → ${a:-нет ответа}"
else
  say "проверка gemini: провайдер ещё не выбран (все отказали) — Gemini по обычному DNS, таймер повторит"
fi
