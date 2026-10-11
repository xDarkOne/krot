# mtproxy — прокси для Telegram (MTProxyL)

**Роли:** entry, solo.
**Зависит от:** base-host, nginx-tls (фронт на 443); в каскаде ещё cascade-awg0 (STUN и подсети Telegram идут через выход).
**Код:** [agent/mtproxy/](../../agent/mtproxy/), пользователи — `krot clients add|show|del|list <узел> [<имя>] --tg`.

Telegram подключается к прокси сам, без VPN-приложения: человек открывает ссылку `tg://proxy…` и нажимает «Подключить прокси». Трафик выглядит как TLS к вашему сайту (FakeTLS), а не-Telegram посетитель видит обычную витрину.

Есть два вида (`mtproxy.backend`):
- **`mtproxyl`** (по умолчанию) — [MTProxyL](https://github.com/Liafanx/MTProxyL): telemt плюс WEB Proxy, zapret2, автоперезапуск при падении дата-центров, проверка доступности из России, необязательный бот;
- **`telemt`** — только официальный telemt, без WEB Proxy и бота. Для тех, кому нужен минимум.

## MTProxyL (по умолчанию)

**Установка без вопросов, по тегу** (`install.sh --branch <тег> -- …`): менеджер, движок — бинарник telemt под systemd (`mtproxyl-telemt`, Docker не нужен), режим combined, zapret2, WEB Proxy. После установки `.branch` удаляется: дальше MTProxyL обновляется сам, из main. Тег и версия движка закреплены в `internal/modexec/prepare.go`.

**Раскладка портов:**

```
:443 (nginx stream, ssl_preread по SNI)
  ├─ имя WEB Proxy  → 127.0.0.1:15444 (TLS-сервер nginx, сертификат nginx-tls) → 127.0.0.1:15080 (WEB-вход telemt)
  └─ остальное      → 127.0.0.1:15443 (telemt, PROXY protocol)
                         не-Telegram клиент → маска 127.0.0.1:8444 (витрина nginx-tls)
```

- **`WEB_FRONTEND=haproxy` — намеренно** ([P-21](../60-pitfalls.md)): роль внешнего фронта выполняет системный nginx. С этим значением MTProxyL не трогает nginx, а telemt слушает только петлю.
- **Маска — на витрину `127.0.0.1:8444`**: по умолчанию MTProxyL маскирует на свой домен, а он смотрит на этот же сервер — получилась бы петля.
- **Имя WEB Proxy** — `web.<домен>` или `*-web.<домен>` в `nginx-tls.names` (A-запись на сервер, без проксирования).
- Включаются: перезапуск при падении DC, проверка доступности из России, перебор carrier для WEB Proxy.
- **Бот MTProxyL** — по желанию: токен в хранилище (`<узел>/mtproxy/tgbot_token`) и `mtproxy.tgbot_admin` (числовой id).

**Пользователи** живут в MTProxyL. Команды `--tg` пульта переводятся в `mtproxyl secret …`; в ссылке есть и обычный вход, и WEB Proxy. При переходе со своего telemt ключи из `/etc/vpnkit/users.json` передаются установщику — ссылки клиентов не меняются.

**zapret2** (nfqws2) ставит MTProxyL. Его настройки не менять: добавление `--hostlist-exclude-domains` ломало MTProto.

## Свой telemt (`backend: telemt`)

- **Официальный релиз telemt**, sha256 архива зашит в пульт.
- `telemt.service`: пользователь `telemt`, `ProtectSystem=strict`, слушает **только петлю**: 15443 — MTProto с PROXY protocol, 9091 — API, 9090 — метрики.
- **TCP/443 держит nginx stream.** Куда он ведёт, решает `vpnkit-gen.py`: без прокси — на витрину, с ним — в telemt. Модули этот файл не пишут, иначе повторная установка nginx-tls отрезала бы прокси.
- **Пользователи** — в `/etc/vpnkit/users.json → mtproto`. Добавление и удаление применяются на лету (hot-reload `[access.users]`), без обрыва соединений. Служебный пользователь `_probe` есть всегда: без пользователей telemt подставил бы общеизвестный нулевой секрет.

## Общее для обоих видов

- **Своя таблица `inet vpnkit_mtproxy`:** MSS 1200 на внешнем :443 в обе стороны ([P-07](../60-pitfalls.md)); в каскаде STUN от telemt получает метку 0x3 и уходит через выход, поэтому telemt видит адрес выхода (`STUN-Quorum reached`).
- **Порт после старта** появляется через несколько секунд ([P-22](../60-pitfalls.md)); установка ждёт до минуты.
- **verify:** движок слушает только петлю, 443 ведёт в него, на 443 сертификат витрины, сайт отвечает 200 через маску.

## Параметры

| Параметр inventory | Умолчание |
|---|---|
| `backend` | `mtproxyl` |
| `mask_name` | первое имя nginx-tls — домен FakeTLS |
| `web_name` | имя nginx-tls с первой меткой `web` или `*-web` (только mtproxyl) |
| `tgbot_admin` | пусто — без бота |

## Проверка

```
ss -Hltn 'sport = :15443'                          # движок на петле
grep ^WEB_FRONTEND= /opt/mtproxyl/settings.conf    # → haproxy
mtproxyl secret list                                # пользователи
```

Снаружи — `tools/e2e/mtproto-test.py`: клиент без Telegram (FakeTLS с подписью секретом → obfuscated2 → `req_pq_multi`). Ответ `resPQ` может прислать только сам Telegram.

**Проверено на тестовых серверах:**
- переход со своего telemt на MTProxyL — пользователь с тем же ключом;
- e2e MTProto — `resPQ` от Telegram;
- WEB Proxy с сертификатом;
- маска отдаёт витрину; чужой и удалённый секреты получают сайт-маску;
- после перезагрузки всё поднимается само.

## Обновление
MTProxyL обновляется своими средствами из main. После обновления проверить: `WEB_FRONTEND` остался `haproxy`, nginx жив и держит 443. Новый тег по умолчанию для новых установок — пин в `prepare.go`.
