# nginx-tls — сайт-витрина, Let's Encrypt и разбор порта 443 по имени

**Роли:** entry, exit, solo.  
**Зависит от:** base-host.  
**Код:** [agent/nginx-tls/](../../agent/nginx-tls/)

Модуль делает сервер похожим на обычный сайт: выпускает сертификаты Let's Encrypt для указанных имён, разворачивает статическую витрину на каждом имени и открывает порты 80/443. На entry весь TCP/443 разбирается по SNI через `stream`, а на exit витрина отвечает напрямую на 443. На этом фундаменте держатся VLESS, Hysteria2, подписка и маска Telegram-прокси.

## Как устроено

### Устанавливаемые пакеты
- `nginx`
- `libnginx-mod-stream`
- `certbot`

### Службы
- `nginx.service` — включена и запущена.
- `certbot.timer` — для автопродления сертификатов.
- `nftables` — перезагружается после записи правил.

### Файлы
- `/etc/nginx/sites-available/vpnkit-<имя>` — конфигурация vhost для каждого имени из `names`.
- `/etc/nginx/sites-enabled/vpnkit-<имя>` — символическая ссылка, включающая vhost.
- `/var/www/vpnkit-<имя>/index.html` — статическая витрина, сгенерированная из шаблона.
- `/var/www/acme` — webroot для Let's Encrypt.
- `/etc/nginx/vpnkit.d/` — каталог для дополнительных `location` от других модулей.
- `/etc/nginx/stream.d/vpnkit-443.conf` — конфигурация stream, создаётся `vpnkit-gen.py`.
- `/etc/nginx/modules-enabled/99-vpnkit-stream.conf` — добавляет блок `stream`, если его нет в `nginx.conf`.
- `/etc/letsencrypt/renewal-hooks/deploy/vpnkit-reload.sh` — перезагружает `nginx` и `try-restart sing-box` после продления.
- `/etc/nftables.d/40-vpnkit-nginx.nft` — правила nftables.
- `/etc/vpnkit/nginx.env` — флаг `STREAM=1|0` для генератора.
- `/usr/local/sbin/vpnkit-gen.py` — генератор stream-конфигурации.

### Порты
- `80/tcp` — ACME-челленджи и редирект на HTTPS.
- `443/tcp`:
  - entry — разбирается по SNI через `stream`.
  - exit — отвечает витриной напрямую.
- `xhttp_port` (по умолчанию `4443`) — TLS-порт для XHTTP, только на entry и solo, только для имени `tunnel_name`.
- `127.0.0.1:8444` — петлевая витрина на entry.

### Правила nftables
- entry/solo: `tcp dport { 80, 443, xhttp_port } accept`
- exit: `tcp dport { 80, 443 } accept`

### Сертификаты
- Выпускаются `certbot certonly --webroot -w /var/www/acme -d <имя> --key-type ecdsa`.
- Если сертификат уже есть — повторный выпуск не выполняется.
- При откате сертификаты не удаляются, чтобы не упереться в лимиты Let's Encrypt.

### Поведение по ролям
- **entry** и **solo**:
  - весь TCP/443 идёт через `stream`.
  - витрины слушают `127.0.0.1:8444`.
  - для имени `tunnel_name` создаётся TLS-порт `xhttp_port`.
- **exit**:
  - витрины слушают `443` напрямую.
  - XHTTP-порт не создаётся.

## Параметры

Переменные `VK_*` готовит пульт. Те, что имеет смысл менять, задаются в inventory как `modules.<модуль>.<имя>` (имя — в нижнем регистре, без `VK_`).

| Переменная | Умолчание | Что значит |
|---|---|---|
| `names` | — (обязателен) | Список имён узла (A-записи без проксирования). Первое имя — основное. |
| `tunnel_name` | первое из `names` | Имя, для которого создаётся XHTTP-порт на entry/solo. |
| `email` | пусто | Почта для Let's Encrypt. Необязательно. |
| `xhttp_port` | `4443` | TLS-порт для XHTTP на entry/solo. |

## Проверка

`verify.sh` проверяет:
- `nginx -t` проходит.
- `nginx` запущен.
- для каждого имени:
  - сертификат не истекает раньше 20 дней;
  - `https://<имя>/` через 443 отвечает `200`;
  - `http://<имя>/` уводит на `https://<имя>/` с кодом `301`.
- если роль не exit:
  - `https://<tunnel_name>:<xhttp_port>/` отвечает `200`.
- в nftables открыты порты `80` и `443`.

Полезные ручные команды:
```bash
nginx -t
systemctl is-active nginx
certbot certificates
openssl x509 -checkend $((20*86400)) -noout -in /etc/letsencrypt/live/<имя>/fullchain.pem
curl -s -o /dev/null -w '%{http_code}\n' --resolve "<имя>:443:127.0.0.1" "https://<имя>/"
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' --resolve "<имя>:80:127.0.0.1" "http://<имя>/"
nft list chain inet vpnkit_fw input
```

## Ловушки

- [P-20](../60-pitfalls.md) — после продления сертификатов `sing-box` держит старый в памяти, поэтому deploy-hook делает `try-restart sing-box`.
- [P-21](../60-pitfalls.md)
- [P-23](../60-pitfalls.md)
- [P-47](../60-pitfalls.md) — в preflight при проверке занятых портов `grep` без совпадений под `pipefail` валит шаг.
- [P-56](../60-pitfalls.md) — форма `http2` зависит от версии nginx: в 1.25.1+ используется `http2 on;`, в более старых — флаг `http2` в `listen`.

## Откат

`rollback.sh`:
- восстанавливает снимки файлов.
- перезагружает `nftables`.
- если `nginx` был установлен этим прогоном — гасит службу `nginx` (пакет оставляет).
- иначе, если `nginx -t` проходит — делает `reload-or-restart nginx`.
- сертификаты не удаляются.
