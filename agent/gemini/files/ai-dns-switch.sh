#!/bin/bash
# ai-dns-switch: выбирает живой Smart-DNS для AI-доменов и прописывает его в dnsmasq.
#
# Держится за текущего провайдера пока тот работает, переключается только при отказе.
# Проверка настоящая: резолвим домен через провайдера и стучимся в выданный прокси
# с правильным SNI, требуя HTTP 200 и валидный сертификат Google.
set -u

# Пути переопределяются переменными окружения — это позволяет прогнать скрипт
# на боевой машине вхолостую, ничего не трогая: DRY_RUN=1 отключает reload dnsmasq.
CONF=${AIDNS_CONF:-/etc/dnsmasq.d/ai-smart.conf}
STATE=${AIDNS_STATE:-/var/lib/ai-dns-switch.state}
LOG=${AIDNS_LOG:-/var/log/ai-dns-switch.log}
DRY_RUN=${DRY_RUN:-0}
PROBE=gemini.google.com

# Провайдеры в порядке предпочтения: "имя IP[,IP2]"
# xbox-dns первым: 26.09 его прокси (87.228.47.x) Google видел как Францию,
# а прокси comss 89.150.59.128 — как Россию (см. geo() ниже).
PROVIDERS=(
    "xboxdns  176.99.11.77,80.78.247.254"
    "comss    83.220.169.155,212.109.195.93"
    "geohide  195.133.25.16,176.99.11.77"
    "malw     95.216.204.218,80.253.249.40"
)

# Список доменов берётся из hosts-файла malw.link (секция Google) — он
# поддерживается и обновляется, так что новые домены Gemini подхватятся сами.
# Кешируется локально; если скачать не вышло, используется встроенный список.
HOSTS_URL=https://raw.githubusercontent.com/ImMALWARE/dns.malw.link/master/hosts
CACHE=${AIDNS_CACHE:-/var/lib/ai-dns-hosts.cache}

# Явно НЕ проксируем, хотя malw.link их перечисляет:
#   o.pki.goog  — OCSP Google, от него зависит проверка сертификатов;
#   fitbit*     — не имеет отношения к Gemini.
# Секции Claude/OpenAI сознательно не берём: там api.anthropic.com и api.openai.com,
# заворачивать рабочие API на сторонний прокси опаснее, чем оставить как есть.
EXCLUDE='^o\.pki\.goog$|fitbit'

# Запасной список на случай, если malw.link недоступен и кеша нет.
FALLBACK_DOMAINS="gemini.google.com aistudio.google.com generativelanguage.googleapis.com
proactivebackend-pa.googleapis.com alkalimakersuite-pa.clients6.google.com
webchannel-alkalimakersuite-pa.clients6.google.com aisandbox-pa.googleapis.com
assistant-s3-pa.googleapis.com robinfrontend-pa.googleapis.com
aitestkitchen.withgoogle.com notebooklm.google.com labs.google jules.google.com
stitch.withgoogle.com"

# Найден вручную в window.WIZ_global_data на странице Gemini, у malw.link его нет.
EXTRA_DOMAINS="geminiweb-pa.clients6.google.com"

log() { echo "$(date '+%F %T') $*" >> "$LOG"; }

# Ротация лога, чтобы не рос бесконечно.
[ -f "$LOG" ] && [ "$(stat -c %s "$LOG" 2>/dev/null || echo 0)" -gt 524288 ] && \
    { tail -n 300 "$LOG" > "$LOG.tmp" && mv "$LOG.tmp" "$LOG"; }

# Настоящие адреса Google — чтобы отличить проксирование от прямого ответа.
google_ips=$(dig +short @8.8.8.8 "$PROBE" A 2>/dev/null | grep -E '^[0-9]' | tr '\n' ' ')

# Официальный список диапазонов Google. Нужен потому, что настоящий адрес
# Google проходит проверку is_alive не хуже прокси (200 + валидный сертификат).
# Без него домены, которые провайдер НЕ подменяет, закреплялись за прямыми
# адресами Google: те меняются от запроса к запросу, конфиг «менялся» каждые
# 10 минут, и dnsmasq перезапускался у всех клиентов (19 раз за 26.09).
GOOG=${AIDNS_GOOG:-/var/lib/ai-dns-goog.json}
if [ ! -s "$GOOG" ] || [ -n "$(find "$GOOG" -mtime +7 2>/dev/null)" ]; then
    g=$(timeout 20 curl -sf https://www.gstatic.com/ipranges/goog.json 2>/dev/null)
    case "$g" in *ipv4Prefix*) printf '%s' "$g" > "$GOOG" ;; esac
fi

# Из переданных адресов печатает те, что НЕ принадлежат Google. Если списка
# диапазонов нет, отсеивает хотя бы адреса, которые 8.8.8.8 отдал для PROBE.
not_google() {
    [ $# -gt 0 ] || return 0
    if [ -s "$GOOG" ] && command -v python3 >/dev/null 2>&1; then
        python3 - "$GOOG" "$@" <<'PYEOF'
import ipaddress, json, sys
nets = [ipaddress.ip_network(p["ipv4Prefix"])
        for p in json.load(open(sys.argv[1]))["prefixes"] if "ipv4Prefix" in p]
for a in sys.argv[2:]:
    try:
        ip = ipaddress.ip_address(a)
    except ValueError:
        continue
    if not any(ip in n for n in nets):
        print(a)
PYEOF
    else
        for a in "$@"; do case " $google_ips " in *" $a "*) ;; *) echo "$a" ;; esac; done
    fi
}

# Какую страну Google видит через прокси: код из настроек панели Google на
# странице Gemini (gbar CONFIG: ..., "ru", "en", ...). Живой прокси ещё не
# значит рабочий: 26.09 прокси comss отвечал 200 с валидным сертификатом, а
# Google через него видел Россию, и Gemini писал «не поддерживается в вашей
# стране». Пустой ответ — страница не пришла целиком, это тоже отказ.
# 07.10.2026: метка лежит на ~670-м КБ страницы, а прокси comss отдаёт ~20 КБ/с —
# за прежние 14 с она не приходила, и живой прокси (nl) отбраковывался.
# Теперь ждём до 45 с, но grep -m1 обрывает загрузку, как только метка нашлась.
geo() {
    timeout 50 curl -s -m 45 --resolve "$PROBE:443:$1" -H 'Accept-Language: en-US' \
        "https://$PROBE/app?hl=en" 2>/dev/null |
        grep -m1 -oE 'gbar_=\{CONFIG:\[\[\[0,"www\.gstatic\.com","[^"]*","[a-z]+"' |
        grep -oE '"[a-z]+"$' | tr -d '"' | head -1
}

# Проверка одного DNS-сервера. 0 = проксирует и прокси живой.
probe() {
    local srv=$1 ips ip res last=""
    # Перебираем весь пул, а не только первый адрес: у comss первым регулярно
    # отдаётся мёртвый адрес, и провайдер целиком считался нерабочим, хотя
    # рядом в пуле живые. Из-за этого 11.09–12.09 конфиг не обновлялся, и
    # клиентам доставалась пара «живой + мёртвый».
    ips=$(timeout 6 dig +short @"$srv" "$PROBE" A 2>/dev/null | grep -E '^[0-9]' | head -5)
    [ -z "$ips" ] && { echo "нет ответа от DNS"; return 1; }
    for ip in $ips; do
        case " $google_ips " in
            *" $ip "*) last="не проксирует (отдал настоящий Google $ip)"; continue ;;
        esac
        # Прокси обязан отдать 200 и валидный сертификат Google: TLS=0 отсекает MITM и поломки.
        # HEAD, а не GET: тело страницы ~700 КБ, через медленный прокси оно не
        # успевало прийти за 12 с, и живой прокси считался мёртвым (07.10.2026).
        res=$(timeout 12 curl -s -I -o /dev/null \
              -w '%{http_code} %{ssl_verify_result}' \
              --resolve "$PROBE:443:$ip" "https://$PROBE/" 2>/dev/null)
        case "$res" in
            "200 0")
                c=$(geo "$ip")
                case "$c" in
                    ""|ru) last="прокси $ip Google видит как ${c:-?} — не годится" ;;
                    *)     echo "$ip ($c)"; return 0 ;;
                esac ;;
            *)       last="прокси $ip не отвечает (code/tls: ${res:-таймаут})" ;;
        esac
    done
    echo "$last"
    return 1
}

current=$(cat "$STATE" 2>/dev/null || echo "")

# Сначала пробуем удержать текущего — переключение только при реальном отказе.
order=()
[ -n "$current" ] && for p in "${PROVIDERS[@]}"; do
    [ "${p%% *}" = "$current" ] && order+=("$p")
done
for p in "${PROVIDERS[@]}"; do
    [ "${p%% *}" = "$current" ] || order+=("$p")
done

chosen_name=""; chosen_ips=""; fails=""
for entry in "${order[@]}"; do
    name=${entry%% *}
    ips=$(echo "$entry" | awk '{print $2}')
    for srv in ${ips//,/ }; do
        out=$(probe "$srv")
        if [ $? -eq 0 ]; then
            chosen_name=$name
            chosen_ips=$ips
            [ "$name" != "$current" ] && log "выбран $name ($srv -> прокси $out)"
            break 2
        fi
        [ "$name" = "$current" ] && log "текущий $name отказал: $srv — $out"
        fails="$fails; $name $srv: $out"
    done
done

if [ -z "$chosen_name" ]; then
    log "НИ ОДИН провайдер не работает — конфиг оставлен без изменений ($current)${fails}"
    exit 1
fi

# Обновляем кеш списка доменов. Скачанное принимаем только если оно осмысленно:
# непустое и содержит gemini.google.com — иначе останемся на прежнем кеше.
fresh=$(timeout 20 curl -sfL "$HOSTS_URL" 2>/dev/null)
# case вместо `echo | grep -q`: grep закрывает пайп на первом совпадении,
# echo получает EPIPE и пишет «write error: Broken pipe» в журнал.
if [ -n "$fresh" ] && case "$fresh" in *gemini.google.com*) true ;; *) false ;; esac; then
    echo "$fresh" > "$CACHE"
fi

# Из hosts-файла берём только секцию Google и только строки на прокси (не 0.0.0.0).
domain_list=""
if [ -s "$CACHE" ]; then
    domain_list=$(awk '/^# /{sec=substr($0,3)} /^[0-9]/ && $1!="0.0.0.0" && sec=="Google"{print $2}' "$CACHE"                   | grep -vE "$EXCLUDE" | sort -u)
fi
if [ -z "$domain_list" ]; then
    domain_list=$FALLBACK_DOMAINS
    log "список malw.link недоступен — использую встроенный"
fi
domain_list=$(printf '%s
%s
' "$domain_list" "$EXTRA_DOMAINS" | tr ' ' '
' | grep -v '^$' | sort -u)

# Провайдер отдаёт пул из нескольких адресов, и среди них регулярно попадаются
# мёртвые (91.207.75.110 висел таймаутом сутками). dnsmasq форвардит ответ как
# есть, поэтому клиент с шансом 1/N упирается в дохлый адрес и ждёт.
# Поэтому резолвим домены сами, отсеиваем нерабочие адреса и отдаём клиентам
# только живые через address=. Проверка TCP-connect: дёшево и достаточно.
declare -A DOM_IPS
alive_cache=""
dead_cache=""

# Проверять TCP-connect бесполезно: у дохлых адресов пула порт 443 открыт,
# висит уже TLS-рукопожатие. Требуем ответа с валидным сертификатом Google.
is_alive() {
    local ip=$1 res
    case " $alive_cache " in *" $ip "*) return 0 ;; esac
    case " $dead_cache "  in *" $ip "*) return 1 ;; esac
    res=$(timeout 12 curl -s -I -o /dev/null -w '%{http_code} %{ssl_verify_result}'           --resolve "$PROBE:443:$ip" "https://$PROBE/" 2>/dev/null)
    if [ "$res" = "200 0" ]; then
        # тот же прокси, но через него Google видит Россию — нам он бесполезен
        case "$(geo "$ip")" in
            ""|ru) ;;
            *) alive_cache="$alive_cache $ip"; return 0 ;;
        esac
    fi
    dead_cache="$dead_cache $ip"; return 1
}

primary_dns=${chosen_ips%%,*}
for d in $domain_list; do
    # Держимся за уже закреплённые адреса, пока они живы: провайдер каждый раз
    # отдаёт случайную часть пула, и без этого конфиг «менялся» на ровном месте.
    old=$(grep "^address=/$d/" "$CONF" 2>/dev/null | cut -d/ -f3 | tr '\n' ' ')
    if [ -n "$old" ]; then
        keep=""
        # shellcheck disable=SC2086
        for ip in $(not_google $old); do is_alive "$ip" && keep="$keep $ip"; done
        if [ "$(echo $keep)" = "$(echo $old)" ]; then
            DOM_IPS[$d]="${keep# }"
            continue
        fi
    fi
    ips=$(timeout 6 dig +short @"$primary_dns" "$d" A 2>/dev/null | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$')
    good=""
    # Адреса самого Google сюда не попадают: домен, который провайдер не
    # подменяет, уходит ниже в обычную переадресацию, как и задумано.
    # shellcheck disable=SC2086
    for ip in $(not_google $ips); do is_alive "$ip" && good="$good $ip"; done
    [ -n "$good" ] && DOM_IPS[$d]="${good# }"
done

primary=${chosen_ips%%,*}
secondary=${chosen_ips#*,}
tmp=$(mktemp)
{
    echo "# Генерируется ai-dns-switch.sh — правки затрутся."
    echo "# Провайдер: $chosen_name ($chosen_ips), выбран $(date '+%F %T')"
    for d in $domain_list; do
        if [ -n "${DOM_IPS[$d]:-}" ]; then
            # Только проверенные адреса — клиент не попадёт на мёртвый.
            for ip in ${DOM_IPS[$d]}; do echo "address=/$d/$ip"; done
        else
            # Живых не нашлось: оставляем обычный форвард, пусть работает как раньше.
            echo "server=/$d/$primary"
            [ "$secondary" != "$chosen_ips" ] && echo "server=/$d/$secondary"
        fi
    done
} > "$tmp"

# Никогда не подсовываем dnsmasq непроверенный конфиг: иначе DNS ляжет у всех клиентов.
# Сравниваем по существу — только строки server=, без строки с датой в шапке.
# Так обновление списка доменов применится, а лишних restart не будет.
if [ -f "$CONF" ] &&    diff -q <(grep -E '^(server|address)=' "$CONF" | sort) <(grep -E '^(server|address)=' "$tmp" | sort) >/dev/null 2>&1; then
    rm -f "$tmp"
    exit 0
fi

# Порядок важен: кладём файл, проверяем конфиг ЦЕЛИКОМ вместе с ним, и только
# при успехе перезагружаем. При провале сразу возвращаем как было — dnsmasq
# обслуживает всех клиентов, оставить его с битым конфигом нельзя.
backup=""
if [ -f "$CONF" ]; then
    backup=$(mktemp)
    cp -a "$CONF" "$backup"
fi
install -m 644 "$tmp" "$CONF"

if ! dnsmasq --test >/dev/null 2>&1; then
    if [ -n "$backup" ]; then cp -a "$backup" "$CONF"; rm -f "$backup"; else rm -f "$CONF"; fi
    log "ОШИБКА: конфиг для $chosen_name не прошёл dnsmasq --test, откатился"
    rm -f "$tmp"; exit 1
fi

if [ "$DRY_RUN" = "1" ]; then
    log "DRY_RUN: конфиг для $chosen_name записан, dnsmasq не трогали"
else
    # ВАЖНО: только restart. По SIGHUP (reload) dnsmasq перечитывает /etc/hosts
    # и чистит кеш, но НЕ перечитывает конфиг — правила server= не применятся.
    systemctl restart dnsmasq 2>/dev/null
    sleep 1
    # dnsmasq обслуживает DNS всем клиентам: убеждаемся, что он поднялся и отвечает.
    if systemctl is-active --quiet dnsmasq &&        timeout 5 dig +short @127.0.0.1 example.com A >/dev/null 2>&1; then
        if [ "$chosen_name" = "$current" ]; then
            log "обновлены адреса у $chosen_name, dnsmasq перезапущен"
        else
            log "переключено на $chosen_name, dnsmasq перезапущен"
        fi
    else
        if [ -n "$backup" ]; then cp -a "$backup" "$CONF"; else rm -f "$CONF"; fi
        systemctl restart dnsmasq 2>/dev/null
        log "ОШИБКА: dnsmasq не ожил после restart — конфиг откачен, сервис поднят обратно"
        rm -f "$backup" "$tmp"; exit 1
    fi
fi
echo "$chosen_name" > "$STATE"
[ -n "$backup" ] && rm -f "$backup"
rm -f "$tmp"
exit 0
