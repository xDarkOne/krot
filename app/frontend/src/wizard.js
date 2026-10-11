// Мастер первой установки: схема → сервер(ы) → домен → состав → установка → ключ пульта.
// Каждый шаг проверяет ввод сразу; дальше — только после зелёной проверки.
import {WReach, WCheck, WBootstrap, WDNS, WCreate, WPlan, WDeploy, KeyPath, WCloudflare, WCatalog, WPreview} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {$, el, toast, errText} from './util.js';

const W = {
    step: 0,
    topology: 'cascade',
    entry: {name: 'ru', host: '', rootPassword: '', role: 'entry', ok: false, keyed: false, via: '', viaName: ''},
    exit: {name: 'eu', host: '', rootPassword: '', role: 'exit', ok: false, keyed: false, via: '', viaName: ''},
    domain: '', hasDomain: true, dns: {}, created: false,
    choices: {},                                    // модуль → включён (только то, что меняли)
    pre: {tunnel: 'es', sub: 'ru', exit: 'en'},     // поддомены по умолчанию
};
let onDone = null;
let logLines = [];

EventsOn('wizard-log', (l) => { logLines.push(l); const p = $('wlog'); if (p) { p.textContent = logLines.join('\n'); p.scrollTop = p.scrollHeight; } });
EventsOn('deploy-log', (e) => {
    logLines.push(e.line);
    const p = $('wlog');
    if (p) { p.textContent = logLines.join('\n'); p.scrollTop = p.scrollHeight; }
    const bar = $('wbar');
    if (bar) { bar.style.width = `${Math.round(100 * (e.step - 1) / e.total)}%`; }
    const cap = $('wcap');
    if (cap) cap.textContent = `Шаг ${e.step} из ${e.total}: ${e.title} → ${e.node}`;
});

const solo = () => W.topology === 'solo';

export function startWizard(done, test = '') {
    onDone = done;
    W.step = 0;
    render();
    // проверка интерфейса без мыши: wizard-entry:<IP> — сразу шаг входа и проверка адреса
    if (test.startsWith('wizard-entry:')) {
        W.step = 1;
        W.entry.host = test.slice(13);
        render();
        const b = $('wcheck');
        if (b) b.click();
    }
    // wizard-domain:<домен>:<IP входа>:<IP выхода>:<поддомен входа>:<поддомен выхода> — шаг домена и проверка DNS
    if (test.startsWith('wizard-domain:')) {
        const [dom, ein, xin, ep, xp] = test.slice(14).split(':');
        Object.assign(W, {topology: 'cascade', domain: dom, hasDomain: true, pre: {tunnel: ep, sub: ep, exit: xp}});
        W.entry.host = ein; W.exit.host = xin;
        W.step = steps().findIndex((s) => s.id === 'domain');
        render();
        const b = $('wdns');
        if (b) b.click();
    }
    // wizard-catalog:<cascade|solo> — шаг состава (с доменом example.org)
    if (test.startsWith('wizard-catalog:')) {
        Object.assign(W, {topology: test.slice(15), domain: 'example.org', hasDomain: true});
        W.step = steps().findIndex((s) => s.id === 'catalog');
        render();
    }
}

// шаги зависят от схемы: в каскаде два сервера, в одиночной — один
function steps() {
    const servers = solo()
        ? [{id: 'solo', title: 'Сервер', body: serverStep('entry')}]
        : [{id: 'entry', title: 'Вход (Россия)', body: serverStep('entry')},
            {id: 'exit', title: 'Выход (за границей)', body: serverStep('exit')}];
    return [{id: 'scheme', title: 'Схема', body: schemeStep}, ...servers,
        {id: 'domain', title: 'Домен', body: domainStep},
        {id: 'catalog', title: 'Состав', body: catalogStep},
        {id: 'deploy', title: 'Установка', body: deployStep},
        {id: 'done', title: 'Готово', body: doneStep}];
}

function render() {
    const main = $('main');
    const st = steps();
    const crumbs = el('ol', {class: 'wsteps'}, st.map((s, i) =>
        el('li', {class: i === W.step ? 'cur' : (i < W.step ? 'done' : '')}, s.title)));
    const title = W.step === 0 ? 'Новая установка' : (solo() ? 'Новый VPN на одном сервере' : 'Новый каскад');
    main.replaceChildren(el('div', {class: 'wizard'}, el('h1', {}, title), crumbs, st[W.step].body()));
}

const next = () => { W.step++; logLines = []; render(); };
const back = () => { W.step--; logLines = []; render(); };

// setup — ответы мастера в виде WSetup (бэкенд пишет из него inventory)
function setup() {
    const d = W.hasDomain ? W.domain : '';
    return {
        topology: W.topology, entry: {...W.entry, role: solo() ? 'solo' : 'entry'}, exit: W.exit, domain: d,
        tunnelName: d ? `${W.pre.tunnel}.${d}` : '', subName: d ? `${W.pre.sub}.${d}` : '',
        exitName: d && !solo() ? `${W.pre.exit}.${d}` : '', choices: W.choices, dir: '',
    };
}

function schemeStep() {
    const pick = (t) => () => {
        if (W.topology !== t) { W.topology = t; W.choices = {}; W.dns = {}; }
        next();
    };
    const opt = (t, head, pros, cons, needs) => el('div', {class: 'card scheme' + (W.topology === t ? ' chosen' : '')},
        el('h3', {}, head),
        el('ul', {class: 'need'}, pros.map((x) => el('li', {class: 'pro'}, x)), cons.map((x) => el('li', {class: 'con'}, x))),
        el('p', {class: 'muted small'}, needs),
        el('div', {class: 'row'}, el('button', {onclick: pick(t)}, 'Выбрать')));
    return el('div', {},
        el('p', {}, 'Выберите, как будет устроен ваш VPN. Состав (какие способы подключения ставить) выберете дальше, у каждого будет пояснение.'),
        el('div', {class: 'cols'},
            opt('cascade', 'Каскад: вход в России → выход за границей',
                ['Устройства видят только российский адрес: переживает белые списки мобильных операторов и блокировки зарубежных адресов.',
                    'Российские сайты (Госуслуги, банки) открываются с российского адреса.'],
                ['Два сервера — дороже; около 40 минут на установку.'],
                'Нужно: VPS в России, который открывается у российских операторов, и VPS за границей.'),
            opt('solo', 'Один зарубежный сервер',
                ['Проще и дешевле: один сервер, около 25 минут.', 'Для дома и Wi-Fi обычно достаточно.'],
                ['Устройства подключаются к зарубежному адресу — такие режут первыми; при белых списках не работает.',
                    'Российские сайты, закрытые для заграницы, открываются только через правила подписки на устройстве.'],
                'Нужно: один VPS за границей, который открывается из России без VPN.')),
        el('div', {class: 'card'},
            el('h3', {}, 'Ещё понадобится'),
            el('ul', {class: 'need'},
                el('li', {}, 'VPS с KVM (не OpenVZ/LXC), Ubuntu 24.04/26.04 или Debian 12/13, от 1 ГБ памяти, публичный IPv4.'),
                el('li', {}, 'Пароль root от каждого сервера — его прислал хостер. После перевода на ключ пароль больше не нужен и нигде не хранится.'),
                el('li', {}, 'По желанию домен: без него будет AmneziaWG, но не подписка, VLESS, Hysteria2 и прокси Telegram.')),
            el('p', {class: 'muted'}, 'Перед установкой отключите VPN на этом компьютере: мастер проверяет, что сервер виден из России.')));
}

function verdictBlock(list) {
    const icon = {ok: '✓', warn: '!', fail: '✗'};
    return el('ul', {class: 'verdict'}, list.map((f) => el('li', {class: f.level}, `${icon[f.level] || '•'} ${f.what}`)));
}

function serverStep(key) {
    return () => {
        const s = W[key];
        const isEntry = key === 'entry';   // вход каскада или единственный сервер: к нему подключаются устройства
        const role = solo() ? 'solo' : key;
        if (solo() && s.name === 'ru') s.name = 'vpn';
        const name = el('input', {value: s.name, maxlength: '16'});
        const host = el('input', {value: s.host, placeholder: 'например 185.0.0.10'});
        const pass = el('input', {type: 'password', placeholder: 'пароль root от хостера', autocomplete: 'off'});
        const out = el('div', {});
        const check = el('button', {id: 'wcheck'}, 'Проверить');
        const toKey = el('button', {disabled: s.ok && !s.keyed ? null : ''}, 'Перевести на ключ');
        const go = el('button', {disabled: s.keyed ? null : ''}, 'Дальше');
        const pre = el('pre', {class: 'install-log', id: 'wlog'}, logLines.join('\n'));

        check.addEventListener('click', async () => {
            s.name = name.value.trim(); s.host = host.value.trim(); s.rootPassword = pass.value;
            s.ok = false; s.via = ''; s.viaName = '';
            check.disabled = true; check.textContent = 'Проверяю…';
            out.replaceChildren(el('div', {class: 'muted'}, 'Доступен ли сервер с этого компьютера…'));
            try {
                const r = await WReach(s.host);
                const lines = [];
                if (r.viaOwn) lines.push(el('div', {class: 'err'}, `Этот компьютер выходит в интернет через ${r.viaOwn} — выключите VPN и проверьте снова.`));
                if (r.ok) lines.push(el('div', {class: 'checks ok'}, `✓ Доступен отсюда: ${r.banner}`));
                else if (isEntry) {
                    lines.push(el('div', {class: 'checks bad'}, `✗ ${r.problem}`),
                        el('div', {}, solo()
                            ? 'Сервер обязан открываться из России без VPN — иначе ваши устройства к нему не подключатся. Возьмите другой сервер или другой IP у хостера.'
                            : 'Вход обязан открываться из России без VPN — иначе каскад бессмыслен. Возьмите другой сервер или другой IP у хостера.'));
                    out.replaceChildren(...lines);
                    return;
                } else {
                    lines.push(el('div', {class: 'checks'}, `Отсюда не виден (${r.problem}). Для выхода это допустимо: подключаюсь через вход.`));
                    s.via = W.entry.host; s.viaName = W.entry.name;
                }
                out.replaceChildren(...lines, el('div', {class: 'muted'}, 'Вхожу по паролю и проверяю сервер…'));
                const c = await WCheck({name: s.name, host: s.host, role, rootPassword: s.rootPassword, via: s.via, viaName: s.viaName});
                out.lastChild.remove();
                if (c.error) { out.append(el('div', {class: 'err'}, c.error)); return; }
                out.append(verdictBlock(c.findings || []));
                if (c.worst === 'fail') { out.append(el('div', {class: 'checks bad'}, 'Сервер не подходит — причины выше.')); return; }
                s.ok = true;
                toKey.disabled = false;
                out.append(el('div', {class: 'checks ok'}, c.worst === 'warn' ? 'Подходит, посмотрите предупреждения.' : 'Подходит.'));
            } catch (e) {
                out.append(el('div', {class: 'err'}, errText(e)));
            } finally {
                check.disabled = false; check.textContent = 'Проверить';
            }
        });
        toKey.addEventListener('click', async () => {
            toKey.disabled = true; toKey.textContent = 'Перевожу…';
            logLines = [];
            try {
                await WBootstrap({name: s.name, host: s.host, role, rootPassword: s.rootPassword || pass.value, via: s.via, viaName: s.viaName});
                s.keyed = true; s.rootPassword = ''; pass.value = '';
                toast(`${s.name}: вход по ключу, пароль отключён`);
                go.disabled = false;
                toKey.textContent = 'Готово';
            } catch (e) {
                toast(errText(e), true);
                toKey.disabled = false; toKey.textContent = 'Перевести на ключ';
            }
        });
        go.addEventListener('click', next);

        const head = solo() ? 'Сервер за границей' : (isEntry ? 'Сервер в России — вход' : 'Сервер за границей — выход');
        const note = solo()
            ? 'К нему подключаются ваши устройства, и через него трафик выходит в интернет.'
            : (isEntry
                ? 'К нему подключаются ваши устройства. Российские сайты он открывает сам, зарубежные отправляет на выход.'
                : 'Через него зарубежный трафик выходит в интернет. Из России напрямую он может быть не виден — это нормально.');
        return el('div', {class: 'card'},
            el('h3', {}, head),
            el('p', {class: 'muted'}, note),
            el('div', {class: 'form'},
                el('label', {}, 'Имя', name, el('span', {class: 'muted small'}, 'латиница, для себя: ru, eu, vpn')),
                el('label', {}, 'IP-адрес', host),
                el('label', {}, 'Пароль root', pass)),
            el('div', {class: 'row'}, check, toKey),
            out, pre,
            el('div', {class: 'row'}, el('button', {class: 'ghost', onclick: back}, 'Назад'), go));
    };
}

function domainStep() {
    const d = el('input', {value: W.domain, placeholder: 'example.org'});
    const has = el('input', {type: 'checkbox'}); has.checked = W.hasDomain;
    const table = el('div', {});
    const go = el('button', {}, 'Дальше');
    // поддомены можно переназвать (например, если es/ru/en в домене уже заняты)
    const pre = {
        tunnel: el('input', {value: W.pre.tunnel, maxlength: '30'}),
        sub: el('input', {value: W.pre.sub, maxlength: '30'}),
        exit: el('input', {value: W.pre.exit, maxlength: '30'}),
    };
    const names = () => {
        const x = d.value.trim().toLowerCase();
        const p = (k) => pre[k].value.trim().toLowerCase();
        if (!x) return [];
        const out = [[`${p('tunnel')}.${x}`, W.entry.host, 'туннели: hysteria2, XHTTP, PQ'],
            [`${p('sub')}.${x}`, W.entry.host, 'подписки и маска прокси Telegram']];
        if (!solo()) out.push([`${p('exit')}.${x}`, W.exit.host, 'витрина выхода']);
        return out;
    };
    for (const [k, inp] of Object.entries(pre)) {
        inp.addEventListener('change', () => { W.pre[k] = inp.value.trim().toLowerCase(); W.dns = {}; draw(); });
    }
    const allOk = () => !has.checked || (names().length && names().every(([n]) => W.dns[n] && W.dns[n].ok));
    const draw = () => {
        go.disabled = allOk() ? null : '';
        if (!has.checked) { table.replaceChildren(el('p', {class: 'muted'}, 'Без домена: только AmneziaWG. Подписку, VLESS, Hysteria2 и прокси Telegram можно добавить позже, когда появится домен.')); return; }
        // одно имя под две роли (туннели и подписки) — одна запись
        const merged = [];
        for (const [n, ip, what] of names()) {
            const m = merged.find((x) => x[0] === n);
            if (m) m[2] += '; ' + what; else merged.push([n, ip, what]);
        }
        const rows = merged.map(([n, ip, what]) => {
            const r = W.dns[n];
            return el('tr', {},
                el('td', {}, el('code', {}, n)), el('td', {}, 'A'), el('td', {}, el('code', {}, ip)),
                el('td', {class: 'muted small'}, what),
                el('td', {class: r ? (r.ok ? 'checks ok' : 'checks bad') : 'muted'}, r ? (r.ok ? '✓' : '✗ ' + r.problem) : '—'));
        });
        table.replaceChildren(
            el('p', {}, `Создайте у регистратора или в Cloudflare A-записи (${merged.length}). В Cloudflare — без проксирования (облако серое).`),
            el('table', {class: 'dns'}, el('tr', {}, el('th', {}, 'Имя'), el('th', {}, 'Тип'), el('th', {}, 'Значение'), el('th', {}, 'Зачем'), el('th', {}, 'Проверка')), rows),
            el('div', {class: 'row'}, el('button', {class: 'ghost', id: 'wdns', onclick: checkDNS}, 'Проверить записи')),
            cfBlock(merged));
    };
    // Cloudflare: создать недостающие записи по токену (только создаёт, чужое не трогает)
    const cfToken = el('input', {type: 'password', autocomplete: 'off', placeholder: 'токен API Cloudflare', style: 'flex:1'});
    const cfOut = el('div', {});
    const cfBlock = (merged) => {
        const btn = el('button', {class: 'ghost'}, 'Создать через Cloudflare');
        btn.addEventListener('click', async () => {
            btn.disabled = true; btn.textContent = 'Создаю…';
            const want = {};
            for (const [n, ip] of merged) want[n] = ip;
            try {
                const recs = await WCloudflare(cfToken.value, d.value.trim().toLowerCase(), want);
                const word = {created: '✓ создана', exists: '✓ уже есть', conflict: '✗ конфликт'};
                cfOut.replaceChildren(el('ul', {class: 'verdict'}, recs.map((r) =>
                    el('li', {class: r.status === 'conflict' ? 'fail' : 'ok'}, `${word[r.status]}: ${r.name}${r.problem ? ' — ' + r.problem : ''}`))));
                cfToken.value = '';
                setTimeout(checkDNS, 3000);   // записи в Cloudflare видны почти сразу
            } catch (e) {
                cfOut.replaceChildren(el('div', {class: 'err'}, errText(e)));
            }
            btn.disabled = false; btn.textContent = 'Создать через Cloudflare';
        });
        return el('details', {class: 'cf'},
            el('summary', {}, 'Домен в Cloudflare? Создать записи автоматически'),
            el('p', {class: 'muted small'}, 'Cloudflare → My Profile → API Tokens → Create Token → шаблон «Edit zone DNS» → Zone Resources: только этот домен. Токен используется один раз и не сохраняется. Существующие записи не меняются.'),
            el('div', {class: 'row'}, cfToken, btn), cfOut);
    };
    const checkDNS = async () => {
        W.domain = d.value.trim().toLowerCase();
        for (const [n, ip] of names()) {
            try { W.dns[n] = await WDNS(n, ip); } catch (e) { W.dns[n] = {ok: false, problem: errText(e)}; }
            draw();
        }
    };
    d.addEventListener('change', () => { W.domain = d.value.trim().toLowerCase(); W.dns = {}; draw(); });
    has.addEventListener('change', () => { W.hasDomain = has.checked; draw(); });
    go.addEventListener('click', () => { W.domain = has.checked ? d.value.trim().toLowerCase() : ''; next(); });
    draw();
    return el('div', {class: 'card'},
        el('h3', {}, 'Домен'),
        el('p', {class: 'muted'}, 'Домен нужен, чтобы сервер выглядел обычным сайтом: на нём держатся подписка, VLESS, Hysteria2 и маска прокси Telegram. Подойдёт любой недорогой.'),
        el('label', {class: 'check'}, has, 'У меня есть домен'),
        el('div', {class: 'form'}, el('label', {}, 'Домен', d),
            el('label', {}, 'Поддомен туннелей', pre.tunnel),
            el('label', {}, 'Поддомен подписок', pre.sub),
            solo() ? null : el('label', {}, 'Поддомен выхода', pre.exit)),
        table,
        el('div', {class: 'row'}, el('button', {class: 'ghost', onclick: back}, 'Назад'), go));
}

// Состав: каталог модулей схемы по разделам. Галочки — выбор пользователя; что получится на самом
// деле (зависимости, нехватка домена) показывает предпросмотр плана с бэкенда.
function catalogStep() {
    const body = el('div', {}, el('p', {class: 'muted'}, 'Загружаю каталог…'));
    const problems = el('div', {});
    const go = el('button', {}, 'Дальше');
    const state = {};   // модуль → {box, note}
    const clientNode = () => W.entry.name;

    const refresh = async () => {
        try {
            const pv = await WPreview(setup());
            for (const [name, st] of Object.entries(state)) {
                // состояние на узле клиентов, а если модуль ставится только на выход — на нём
                const steps = (pv.steps || []).filter((s) => s.module === name);
                const s = steps.find((x) => x.node === clientNode()) || steps[0];
                if (!s) continue;
                st.box.checked = !s.skip;
                st.note.textContent = s.skip ? `не ставится: ${s.reason}` : (['по умолчанию', 'выбран', 'обязателен'].includes(s.reason) ? '' : s.reason);
                // красным — только если пользователь хотел модуль, а поставить его нельзя
                const c = st.item.catalog;
                const wanted = !c.auto && (W.choices[name] ?? (c.default || c.required));
                st.note.className = 'small ' + (s.skip && wanted ? 'err' : 'muted');
            }
            const pr = pv.problems || [];
            problems.replaceChildren(...pr.map((p) => el('div', {class: 'checks bad'}, '✗ ' + p)));
            go.disabled = pr.length ? '' : null;
        } catch (e) {
            problems.replaceChildren(el('div', {class: 'err'}, errText(e)));
            go.disabled = '';
        }
    };

    WCatalog(W.topology).then((cat) => {
        const parts = [];
        for (const g of cat.groups) {
            const items = (cat.modules || []).filter((m) => m.catalog.group === g.id);
            if (!items.length) continue;
            parts.push(el('div', {class: 'card'},
                el('h3', {}, g.title, el('span', {class: 'muted small'}, ' — ' + g.note)),
                el('div', {class: 'catalog'}, items.map((m) => {
                    const c = m.catalog;
                    const box = el('input', {type: 'checkbox'});
                    box.checked = W.choices[m.name] ?? c.default ?? false;
                    if (c.required || c.auto) box.disabled = true;
                    box.addEventListener('change', () => { W.choices[m.name] = box.checked; refresh(); });
                    const note = el('div', {class: 'small muted'});
                    state[m.name] = {box, note, item: m};
                    const tags = [];
                    if (c.required) tags.push('обязательно');
                    if (c.auto) tags.push('ставится само, когда нужно');
                    if (c.domain) tags.push('нужен домен');
                    if (c.cost) tags.push(c.cost);
                    return el('label', {class: 'cmod'}, box,
                        el('div', {},
                            el('div', {}, el('b', {}, c.title), ' — ', c.what),
                            el('div', {class: 'small'}, c.why),
                            tags.length ? el('div', {class: 'small muted'}, tags.join(' · ')) : null,
                            note));
                }))));
        }
        body.replaceChildren(...parts);
        refresh();
    }).catch((e) => body.replaceChildren(el('div', {class: 'err'}, errText(e))));

    go.addEventListener('click', next);
    return el('div', {},
        el('p', {}, 'Отметьте, что ставить. Всё можно добавить и позже — на экране сервера.'),
        body, problems,
        el('div', {class: 'row'}, el('button', {class: 'ghost', onclick: back}, 'Назад'), go));
}

function deployStep() {
    const out = el('div', {});
    const start = el('button', {}, 'Установить');
    const pre = el('pre', {class: 'install-log', id: 'wlog'}, logLines.join('\n'));
    const bar = el('div', {class: 'pbar'}, el('div', {id: 'wbar', style: 'width:0%'}));
    const cap = el('div', {id: 'wcap', class: 'muted'}, '');
    const prepare = async () => {
        try {
            if (!W.created) {
                await WCreate(setup());
                W.created = true;
            }
            const plan = (await WPlan()).filter((s) => !(s.skip && (s.reason === 'не нужен' || s.reason === 'не выбран')));
            const todo = plan.filter((s) => !s.done && !s.skip);
            out.replaceChildren(
                el('p', {}, `Будет поставлено шагов: ${todo.length}. Каждый: снимок → откат по таймеру, если связь пропадёт → установка → проверка. При ошибке шаг откатывается, а «Продолжить» начнёт с него же.`),
                el('ul', {class: 'verdict'}, plan.map((s) => el('li', {class: s.done ? 'ok' : (s.skip ? 'muted' : '')},
                    `${s.done ? '✓' : (s.skip ? '–' : '→')} ${s.title} · ${s.node}${s.skip ? ' — ' + s.reason : ''}`))));
            start.disabled = todo.length ? null : '';
        } catch (e) {
            out.replaceChildren(el('div', {class: 'err'}, errText(e)));
        }
    };
    start.addEventListener('click', async () => {
        start.disabled = true; start.textContent = 'Устанавливаю…';
        logLines = [];
        try {
            await WDeploy();
            $('wbar').style.width = '100%';
            toast(solo() ? 'Сервер готов' : 'Каскад установлен');
            next();
        } catch (e) {
            toast(errText(e), true);
            start.disabled = false; start.textContent = 'Продолжить';
        }
    });
    prepare();
    return el('div', {class: 'card'}, el('h3', {}, 'Установка'), out, cap, bar, pre,
        el('div', {class: 'row'}, start));
}

function doneStep() {
    const path = el('code', {}, '…');
    KeyPath().then((p) => { path.textContent = p; });
    return el('div', {class: 'card'},
        el('h3', {}, solo() ? 'Сервер готов' : 'Каскад готов'),
        el('p', {}, 'Сохраните копию ключа пульта — без него не расшифровать ни настройки, ни резервные копии серверов. Удобно положить в менеджер паролей.'),
        el('p', {}, 'Ключ: ', path),
        el('p', {}, `Дальше: на экране ${solo() ? 'сервера' : 'входа'} добавьте клиентов — у каждого будет QR и ссылка.`),
        el('div', {class: 'row'}, el('button', {onclick: () => onDone && onDone()}, 'К узлам')));
}
