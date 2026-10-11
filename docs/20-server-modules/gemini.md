# gemini — доступ к Gemini и связанным AI-доменам через Smart-DNS

**Роли:** entry (только каскад). **Зависит от:** split-routing, xray. **Код:** [agent/gemini/](../../agent/gemini/)

Модуль нужен, когда запросы к Gemini и другим AI-доменам блокируются из-за страны, которую видит провайдер по адресу выхода. Он подменяет DNS-ответы для этих доменов адресами живого Smart-DNS-прокси, чтобы клиенты и xray получали рабочий путь.

## Как устроено

Модуль ставится поверх split-routing и xray:

- проверяет наличие `/etc/dnsmasq.d/10-vpnkit-split.conf`;
- ставит пакеты `curl`, `python3`, `bind9-dnsutils`;
- кладёт скрипт `/usr/local/bin/ai-dns-switch.sh`;
- создаёт и включает `ai-dns-switch.service` и `ai-dns-switch.timer`;
- делает снимки `/etc/dnsmasq.d/ai-smart.conf` и `/var/lib/ai-dns-switch.state` для отката.

Таймер запускает скрипт через 3 минуты после загрузки и затем раз в `interval_min` минут. Скрипт:

- держится за текущего провайдера, пока тот жив;
- перебирает провайдеров Smart-DNS из встроенного списка в скрипте;
- проверяет провайдера: DNS-ответ, HTTP 200, валидный сертификат, страну, которую видит прокси;
- берёт список AI-доменов из внешнего hosts-файла, указанного в скрипте, кеш — `/var/lib/ai-dns-hosts.cache`;
- при недоступности внешнего списка использует встроенный запасной список;
- отбрасывает адреса самого провайдера и мёртвые адреса пула;
- записывает `/etc/dnsmasq.d/ai-smart.conf` строками `address=` или `server=`;
- перед применением выполняет `dnsmasq --test`;
- применяет конфиг только через `restart dnsmasq` ([P-15](../60-pitfalls.md));
- после перезапуска проверяет, что dnsmasq активен и отвечает;
- при сбое возвращает прежний конфиг и поднимает dnsmasq обратно.

Файлы и службы:

- `/usr/local/bin/ai-dns-switch.sh`
- `/etc/systemd/system/ai-dns-switch.service`
- `/etc/systemd/system/ai-dns-switch.timer`
- `/etc/dnsmasq.d/ai-smart.conf`
- `/var/lib/ai-dns-switch.state`
- `/var/log/ai-dns-switch.log`
- `/var/lib/ai-dns-hosts.cache`
- `/var/lib/ai-dns-goog.json`

Порты, правила nftables и IP-правила модуль не добавляет.

В каскаде в роли `entry` подменённые адреса видят клиенты DNS входа и xray. В роли `solo` те же правила работают локально: xray берёт DNS `127.0.0.1`, а подмена идёт через dnsmasq того же сервера.

## Параметры

Переменные `VK_*` готовит пульт. Те, что имеет смысл менять, задаются в inventory как `modules.<модуль>.<имя>` (имя — в нижнем регистре, без `VK_`).

| Переменная | Умолчание | Что значит |
|---|---:|---|
| `interval_min` | `10` | Период, с которым таймер запускает проверку Smart-DNS-провайдеров. |

## Проверка

`verify.sh` проверяет:

- запущен `ai-dns-switch.timer`;
- dnsmasq отвечает на `127.0.0.1`;
- если `/etc/dnsmasq.d/ai-smart.conf` уже создан, конфиг проходит `dnsmasq --test`;
- выводит текущего провайдера и ответ DNS для проверочного домена Gemini.

Полезные команды:

```sh
systemctl is-active ai-dns-switch.timer
tail -n 20 /var/log/ai-dns-switch.log
cat /var/lib/ai-dns-switch.state
dnsmasq --test
dig +short @127.0.0.1 example.org
```

Для проверки подмены используйте проверочный домен Gemini из health-проверки манифеста:

```sh
dig +short @127.0.0.1 <проверочный-домен-Gemini> | head -3
```

Если провайдер ещё не выбран, ответ будет обычным DNS-ответом, а таймер повторит проверку через `interval_min`.

## Ловушки

- [P-15](../60-pitfalls.md) — dnsmasq применяет конфиг только через `restart`, а не через reload.
- [P-16](../60-pitfalls.md) — для xray нужно исключение `127.0.0.1:53 → direct`.
- [P-18](../60-pitfalls.md) — для xray нужен включённый sniffing, потому что клиенты могут присылать уже готовый IP.

## Откат

`rollback.sh`:

- останавливает и отключает `ai-dns-switch.timer`;
- восстанавливает снимки `/etc/dnsmasq.d/ai-smart.conf` и `/var/lib/ai-dns-switch.state`;
- выполняет `systemctl daemon-reload`;
- перезапускает dnsmasq.
