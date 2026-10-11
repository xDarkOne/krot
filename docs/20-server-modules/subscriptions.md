# subscriptions — персональные подписки VPN-входов

**Роли:** entry, solo. **Зависит от:** xray, nginx-tls. **Код:** [agent/subscriptions/](../../agent/subscriptions/)

Модуль раздаёт клиентам одну персональную ссылку на все входы. Клиент получает `https://<имя>/u/<токен>`, открывает её в Happ, v2rayNG или Hiddify и получает актуальные входы; новые входы приходят в подписку без замены ссылки.

## Как устроено

Модуль не открывает свои порты, не создаёт nftables-правил и не ставит systemd-юниты. Он использует существующую витрину nginx-tls и добавляет в неё раздачу файлов подписок.

Установка:

- требует `/etc/vpnkit/xray.env` — без xray подписке нечего раздавать;
- требует витрину `/etc/nginx/sites-available/vpnkit-<sub_name>` — сначала должен быть установлен nginx-tls;
- копирует генератор подписок из `$VK_RUN/src/vpnkit-gen.py` в `/usr/local/sbin/vpnkit-gen.py` с правами `755`;
- создаёт каталог подписок `/var/www/vpnkit-sub/u/`;
- пишет настройки генератора в `/etc/vpnkit/sub.env` с правами `600`:
  - `SUB_NAME=<sub_name>`
  - `OUTDIR=/var/www/vpnkit-sub/u`
  - `ROUTING=<routing>`
  - `ROUTING_NAME=<routing_name>`
- делает снапшот `/etc/nginx/vpnkit.d/<sub_name>-site-routing.conf`, если он уже есть;
- пишет `/etc/nginx/vpnkit.d/<sub_name>-site-sub.conf` с правами `644`:

```nginx
location ^~ /u/ {
    access_log off;
    alias /var/www/vpnkit-sub/u/;
    default_type text/plain;
    add_header Cache-Control "no-store" always;
    try_files $uri =404;
}
```

- проверяет и перезапускает nginx: `nginx -t -q`, `systemctl reload nginx`;
- запускает генератор: `/usr/local/sbin/vpnkit-gen.py build`.

Ссылка клиента имеет вид:

```text
https://<sub_name>/u/<токен>
```

Чужой токен отвечает обычным `404` витрины.

В каскаде модуль занимает роль `entry`: он раздаёт подписки на входы, созданные другими модулями. На одном сервере он работает как `solo` и раздаёт доступные входы этого сервера.

## Параметры

Переменные `VK_*` готовит пульт. Те, что имеет смысл менять, задаются в inventory как `modules.<модуль>.<имя>` (имя — в нижнем регистре, без `VK_`).

| параметр inventory | умолчание | что значит |
|---|---|---|
| `sub_name` | `имя туннелей` | имя витрины nginx-tls, под которым раздаются подписки: `https://<sub_name>/u/<токен>` |
| `routing` | `1` | включать правила «российское — напрямую» для Happ и INCY на путях `/h` и `/i` |
| `routing_name` | `VPN` | имя набора правил, как он подписан в приложении |

## Проверка

`verify.sh` проверяет через `443` порт:

- берёт токены из `/etc/vpnkit/users.json`;
- для каждого токена запрашивает `https://<sub_name>/u/<токен>`;
- декодирует ответ `base64 -d`;
- требует минимум две ссылки с `://`;
- проверяет, что случайный чужой токен отвечает `404`;
- если `routing=1`, проверяет, что `/h` отвечает редиректом на `happ://routing/onadd/…`, а `/i` — на `incy://routing/onadd/…`.

Полезные команды:

```bash
test -d /var/www/vpnkit-sub/u && echo ok
ls /var/www/vpnkit-sub/u | wc -l
curl -s --resolve example.org:443:127.0.0.1 https://example.org/u/<токен> | base64 -d | grep -c '://'
```

## Ловушки

- [P-50](../60-pitfalls.md): полные ссылки с токенами не должны попадать в журнал; для `location /u/` включено `access_log off`.

## Откат

`rollback.sh` восстанавливает снапшот nginx-конфигурации, затем, если конфигурация nginx корректна, перезагружает nginx. Каталог `/var/www/vpnkit-sub/u/` с подписками остаётся на сервере, но после отката подписки больше не раздаются.
