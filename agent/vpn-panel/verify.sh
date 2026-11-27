systemctl is-active -q vpnkit-panel.service || die "панель не запущена"
systemctl is-active -q vpnkit-panel-reminders.timer || die "таймер напоминаний не запущен"
for _ in $(seq 1 5); do
  if curl -fsS --max-time 3 http://127.0.0.1:19194/health >/dev/null; then break; fi
  sleep 1
done
curl -fsS --max-time 3 http://127.0.0.1:19194/health >/dev/null || die "API панели недоступен"
site="https://$VK_SUB_NAME/panel/"
opt=(--max-time 5 --resolve "$VK_SUB_NAME:443:127.0.0.1")
curl -fsS "${opt[@]}" "$site" >/dev/null || die "страница панели не отдаётся"
status=$(curl -sS "${opt[@]}" -X POST -o /dev/null -w '%{http_code}' "${site}api/")
[ "$status" = 403 ] || die "API панели должен отклонять запрос без подписи Telegram"
