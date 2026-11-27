// Крот — окно пульта. Данные с узлов попадают в страницу только как текст (textContent):
// имена клиентов и тексты проверок приходят по сети.
import {
    GetState, PickInventory, Unlock, Status, Clients,
    AddClient, ShowClient, DelClient, OpenClientsFolder, PublishSubAmnezia,
    Modules, Install, Answer, Backups, CreateBackup, PullBackup, RestoreClients,
} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {$, closeModal, el, toast, errText} from './util.js';
import {startWizard} from './wizard.js';
import {initAppUpdate, updateNow} from './appupdate.js';
import {openAlerts} from './alerts.js';
import {checkUpdates} from './updates.js';
import {openVersions} from './versions.js';
import {openDashboard} from './dashboard.js';

const statusCache = {};   // узел → последний Status
let state = {nodes: []};
let current = null;

EventsOn('log', (s) => toast(s));

const ROLE = {entry: 'вход', exit: 'выход', solo: 'один сервер'};
const CLIENT_TYPES = {combined: 'Подписка + профиль AmneziaVPN', awg: 'Профиль AmneziaVPN', sub: 'Подписка', tg: 'Прокси Telegram'};

// ---- список узлов ---------------------------------------------------------

function dotClass(st) {
    if (!st) return 'dot';
    if (st.busy) return 'dot busy';
    if (!st.reachable || st.error) return 'dot bad';
    if (st.watch && st.failed && st.failed.length) return 'dot warn';
    return 'dot ok';
}

function renderInventory() {
    const list = $('nodes');
    const nodes = state.nodes;
    $('refresh-all').disabled = !nodes.length;
    if (!nodes.length) {
        list.replaceChildren();
        return;
    }
    const indicators = nodes.map((n) => dotClass(statusCache[n.name]));
    const health = ['dot bad', 'dot warn', 'dot busy', 'dot'].find((level) => indicators.includes(level)) || 'dot ok';
    const name = nodes.length === 1 ? 'Один сервер'
        : nodes.some((n) => n.role === 'entry') && nodes.some((n) => n.role === 'exit') ? 'Каскад' : 'Серверы';
    list.replaceChildren(el('li', {},
        el('button', {class: 'inventory-record', 'aria-current': current ? 'page' : null, onclick: () => {
            selectNode(current || nodes[0].name);
            $('main').scrollTop = 0;
        }}, el('span', {class: health, 'aria-hidden': 'true'}),
        el('span', {class: 'inventory-name'}, name),
        el('span', {class: 'inventory-count'}, `Серверов: ${nodes.length}`))));
    for (const dot of document.querySelectorAll('.server-switcher [data-node-status]')) {
        dot.className = dotClass(statusCache[dot.dataset.nodeStatus]);
    }
}

async function refreshStatus(name) {
    statusCache[name] = {...(statusCache[name] || {}), busy: true};
    renderInventory();
    if (current === name) renderNode();
    try {
        statusCache[name] = await Status(name);
    } catch (e) {
        statusCache[name] = {reachable: false, error: errText(e)};
    }
    await loadModules(name);
    renderInventory();
    if (current === name) renderNode();
}

// ---- модули ---------------------------------------------------------------

const moduleCache = {};   // узел → [ModuleView]
const installLog = {};    // узел → строки хода установки
let installing = {};      // узел → модуль
const openDone = {};      // узел → раскрыт ли список установленных модулей

async function loadModules(name) {
    const st = statusCache[name];
    if (!st || !st.reachable) return;
    try {
        moduleCache[name] = await Modules(name, (st.modules || []).map((m) => m.name));
    } catch (e) {
        moduleCache[name] = {error: errText(e)};
    }
}

EventsOn('install-log', (ev) => {
    (installLog[ev.node] = installLog[ev.node] || []).push(ev.line);
    const pre = document.getElementById('install-log');
    if (pre) { pre.textContent = installLog[ev.node].join('\n'); pre.scrollTop = pre.scrollHeight; }
});

async function runInstall(node, mod) {
    installing[node] = mod.name;
    installLog[node] = [];
    renderNode();
    // ход установки — в окне поверх страницы: видно сразу, а не внизу длинного списка
    const result = el('div', {class: 'muted', style: 'margin-top:10px'}, 'Идёт установка. Окно можно не трогать: при обрыве связи узел сам откатит изменения.');
    const close = el('button', {class: 'ghost', disabled: '', onclick: closeModal}, 'Закрыть');
    $('modal-body').replaceChildren(
        el('h2', {}, `${mod.catalog.title} → ${node}`),
        el('div', {class: 'muted'}, mod.what),
        el('pre', {class: 'install-log', id: 'install-log'}, ''),
        result, el('div', {class: 'row'}, close));
    $('modal').hidden = false;
    try {
        await Install(node, mod.name);
        result.className = 'checks ok';
        result.textContent = '✓ Установлен и проверен.';
        delete clientCache[node];
    } catch (e) {
        result.className = 'checks bad';
        result.textContent = '✗ ' + errText(e);
    }
    close.disabled = false;
    delete installing[node];
    await refreshStatus(node);
    const n = state.nodes.find((x) => x.name === node);
    if (n && (n.role === 'entry' || n.role === 'solo')) loadClients(node);
}

function modulesCard(node) {
    const ms = moduleCache[node];
    const head = el('h3', {class: 'bar'}, 'Модули установщика');
    if (!ms) return el('div', {class: 'card'}, head, el('div', {class: 'muted'}, 'Узнаю, что стоит…'));
    if (ms.error) return el('div', {class: 'card'}, head, el('div', {class: 'err'}, ms.error));
    const busy = installing[node];
    const rows = ms.map((m) => {
        let action;
        if (busy === m.name) action = el('span', {class: 'chip'}, 'ставлю…');
        else if (m.missing && m.missing.length && !m.installed) action = el('span', {class: 'muted small'}, 'сначала: ' + m.missing.join(', '));
        else {
            const b = el('button', {class: m.installed ? 'ghost small' : 'small', disabled: busy ? '' : null},
                m.installed ? 'Переустановить' : 'Установить');
            let armed = false;
            b.addEventListener('click', () => {
                if (!armed) {   // второе нажатие — подтверждение: модуль меняет сервер
                    armed = true; b.textContent = m.installed ? 'Точно переустановить?' : 'Точно установить?';
                    setTimeout(() => { armed = false; b.textContent = m.installed ? 'Переустановить' : 'Установить'; }, 4000);
                    return;
                }
                runInstall(node, m);
            });
            action = b;
        }
        return el('li', {class: 'mod'},
            el('span', {class: m.installed ? 'dot ok' : 'dot'}),
            el('div', {}, el('div', {class: 'nm'}, m.catalog.title), el('div', {class: 'muted small'}, m.catalog.what),
                m.installed ? null : el('div', {class: 'small'}, m.catalog.why),
                !m.installed && !m.choice.on ? el('div', {class: 'muted small'}, 'в плане: не ставится — ' + m.choice.why) : null),
            action);
    });
    // установленные — свёрнуты: длинный зелёный список ничего не говорит и вытесняет остальное
    const todo = rows.filter((_, i) => !ms[i].installed);
    const done = rows.filter((_, i) => ms[i].installed);
    const blocks = [head];
    if (todo.length) blocks.push(el('ul', {class: 'mods'}, todo));
    else blocks.push(el('div', {class: 'checks ok'}, `✓ Все модули, которые установщик ставит на этот узел, стоят (${done.length}).`));
    if (done.length) {
        const det = el('details', {class: 'mods-done'}, el('summary', {}, `Установлены (${done.length})`), el('ul', {class: 'mods'}, done));
        if (openDone[node]) det.open = true;
        det.addEventListener('toggle', () => { openDone[node] = det.open; });
        blocks.push(det);
    }
    blocks.push(el('div', {class: 'muted small', style: 'margin-top:8px'},
        'Каждый модуль: снимок файлов → откат по таймеру, если связь с узлом пропадёт → установка → проверка → откат при ошибке.'));
    return el('div', {class: 'card'}, blocks);
}

// ---- резервные копии ------------------------------------------------------

const backupCache = {};   // узел → {list, report, busy}

async function loadBackups(node) {
    const b = backupCache[node] = backupCache[node] || {};
    try { b.list = await Backups(node) || []; } catch (e) { b.error = errText(e); }
    if (current === node) renderNode();
}

function backupsCard(n) {
    const b = backupCache[n.name];
    if (!b) { loadBackups(n.name); }
    const st = b || {};
    const saveBackup = async (action, request) => {
        if (st.busy) return;
        st.busy = action;
        delete st.report;
        backupCache[n.name] = st;
        renderNode();
        try {
            const r = await request(n.name);
            st.report = r.missing && r.missing.length
                ? {bad: true, text: `✗ в копии нет ключевых файлов: ${r.missing.join(', ')}`}
                : {bad: false, text: `✓ ${r.file.name}: расшифрована ключом пульта, ${r.files} файлов, все модули на месте`};
        } catch (e) {
            st.report = {bad: true, text: errText(e)};
        } finally {
            st.busy = null;
            await loadBackups(n.name);
        }
    };
    const disabled = st.busy ? '' : null;
    const pull = el('button', {class: 'ghost small', disabled, onclick: () => saveBackup('pull', PullBackup)},
        st.busy === 'pull' ? 'Забираю…' : 'Забрать свежую копию');
    const create = el('button', {class: 'small', disabled, onclick: () => saveBackup('create', CreateBackup)},
        st.busy === 'create' ? 'Создаю и скачиваю…' : 'Создать копию сейчас');
    const rows = (st.list || []).slice(0, 5).map((f) => {
        const kids = [el('span', {}, `${f.at} · ${(f.size / 1024).toFixed(0)} КБ`)];
        if (n.role === 'entry' || n.role === 'solo') {
            const rb = el('button', {class: 'ghost small'}, 'Восстановить клиентов');
            let armed = false;
            rb.addEventListener('click', async () => {
                if (!armed) {
                    armed = true; rb.textContent = 'Точно? Реестр клиентов заменится этой копией';
                    setTimeout(() => { armed = false; rb.textContent = 'Восстановить клиентов'; }, 5000);
                    return;
                }
                rb.disabled = true; rb.textContent = 'Восстанавливаю…';
                try {
                    await RestoreClients(n.name, f.path);
                    toast('Клиенты восстановлены: их ссылки снова работают');
                    delete clientCache[n.name];
                    loadClients(n.name);
                } catch (e) { toast(errText(e), true); }
                rb.disabled = false; rb.textContent = 'Восстановить клиентов';
            });
            kids.push(rb);
        }
        return el('li', {class: 'brow'}, ...kids);
    });
    return el('div', {class: 'card'},
        el('h3', {class: 'bar'}, 'Резервные копии', el('div', {class: 'server-info-actions'}, pull, create)),
        st.report ? el('div', {class: st.report.bad ? 'checks bad' : 'checks ok'}, st.report.text) : null,
        rows.length ? el('ul', {class: 'mods'}, rows) : el('div', {class: 'muted small'}, 'На этом компьютере копий узла пока нет.'),
        el('div', {class: 'muted small', style: 'margin-top:8px'}, 'Копии зашифрованы ключом этого компьютера: сервер их делает, но прочитать не может. Восстановление возвращает клиентов подписки и Telegram из копии, а AWG — из хранилища пульта, без перевыдачи.'));
}

// ---- пароль ---------------------------------------------------------------

EventsOn('ask-password', (q) => {
    const input = el('input', {type: 'password', autocomplete: 'off', style: 'width:100%'});
    const done = (ok) => { Answer(q.id, ok ? input.value : '', ok); closeModal(); };
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') done(true); });
    $('modal-body').replaceChildren(
        el('h2', {}, 'Нужен пароль'),
        el('p', {}, q.question),
        input,
        el('div', {class: 'muted small', style: 'margin-top:6px'}, 'Пароль хранится только в памяти, пока открыто приложение.'),
        el('div', {class: 'row'}, el('button', {onclick: () => done(true)}, 'Продолжить'),
            el('button', {class: 'ghost', onclick: () => done(false)}, 'Отмена')));
    $('modal').hidden = false;
    input.focus();
});

async function loadState(s) {
    state = s || await GetState();
    state.nodes = state.nodes || [];   // без файла узлов Go отдаёт null
    $('inv').textContent = state.inventory || 'файл узлов не открыт';
    const w = $('welcome');
    if (w) w.hidden = !!state.inventory;
    renderInventory();
    if (state.nodes && state.nodes.length) {
        if (!current || !state.nodes.find((n) => n.name === current)) selectNode(state.nodes[0].name);
        state.nodes.forEach((n) => refreshStatus(n.name));
        checkUpdates(() => state.nodes.forEach((n) => refreshStatus(n.name)));
    }
    if (state.open && !opened) {   // KROT_APP_OPEN=узел/вид/имя | install:узел/модуль | wizard[-entry:IP] (проверка без мыши)
        opened = true;
        if (state.open === 'versions') {
            openVersions();
        } else if (state.open === 'update') {
            updateNow();
        } else if (state.open === 'alerts') {
            openAlerts();
        } else if (state.open.startsWith('wizard')) {
            current = null;
            startWizard(async () => { await loadState(); }, state.open);
        } else if (state.open.startsWith('install:')) {
            const [node, mod] = state.open.slice(8).split('/');
            selectNode(node);
            const wait = setInterval(() => {   // ждём списка модулей узла
                const ms = moduleCache[node];
                if (!Array.isArray(ms)) return;
                clearInterval(wait);
                const m = ms.find((x) => x.name === mod);
                if (m) runInstall(node, m); else toast(`модуля ${mod} нет в списке узла`, true);
            }, 500);
        } else {
            const [node, kind, name] = state.open.split('/');
            selectNode(node);
            openCard(node, kind, name);
        }
    }
}
let opened = false;

// ---- узел -----------------------------------------------------------------

const clientCache = {};

function selectNode(name) {
    current = name;
    renderInventory();
    renderNode();
    const n = state.nodes.find((x) => x.name === name);
    if (n && (n.role === 'entry' || n.role === 'solo') && !clientCache[name]) loadClients(name);
}

function renderNode() {
    const n = state.nodes.find((x) => x.name === current);
    if (!n) return;
    const st = statusCache[n.name];
    const main = $('main');
    const focused = main.querySelector('.server-option:focus')?.dataset.node;
    const buttons = state.nodes.map((node) => el('button', {
        class: 'server-option', 'data-node': node.name, 'aria-pressed': String(node.name === n.name),
        onclick: () => selectNode(node.name),
    },
    el('span', {class: dotClass(statusCache[node.name]), 'data-node-status': node.name, 'aria-hidden': 'true'}),
    el('strong', {}, node.name), el('span', {class: 'server-role'}, ROLE[node.role] || node.role),
    el('span', {class: 'server-host'}, node.host)));
    const info = el('section', {class: 'card server-info', 'aria-labelledby': 'server-info-title'},
        el('div', {class: 'server-info-head'}, el('h1', {id: 'server-info-title'}, 'Серверы'),
            el('div', {class: 'server-info-actions'},
                el('button', {class: 'ghost', onclick: () => openDashboard(state.nodes)}, 'Статистика'),
                el('button', {class: 'ghost', disabled: st && st.busy ? '' : null, title: `Проверить ${n.name}`,
                    onclick: () => refreshStatus(n.name)}, st && st.busy ? 'Проверяю…' : 'Проверить'))),
        el('div', {class: 'server-switcher', role: 'group', 'aria-label': 'Выбор сервера'}, buttons),
        statusContent(st));
    const blocks = [info];
    blocks.push(modulesCard(n.name));
    blocks.push(backupsCard(n));
    if (n.role === 'entry' || n.role === 'solo') blocks.push(clientsCard(n.name));
    else if (n.role === 'exit') blocks.push(el('div', {class: 'card muted'},
        'Клиенты подключаются к входу; выход только выпускает их трафик в интернет.'));
    main.replaceChildren(...blocks);
    if (focused) buttons.find((button) => button.dataset.node === focused)?.focus({preventScroll: true});
}

function statusContent(st) {
    if (!st || (st.busy && !st.at)) return el('div', {class: 'muted'}, 'Связываюсь с узлом…');
    if (!st.reachable || st.error && !st.os) {
        return el('div', {}, el('h3', {}, 'Нет связи'), el('div', {class: 'err'}, st.error || 'узел не ответил'));
    }
    const facts = el('dl', {class: 'facts'},
        el('dt', {}, 'Система'), el('dd', {}, `${st.os}, ядро ${st.kernel}`),
        el('dt', {}, 'Работает'), el('dd', {}, st.uptime),
        el('dt', {}, 'Диск'), el('dd', {}, st.disk),
        el('dt', {}, 'Память'), el('dd', {}, st.mem));
    const mods = (st.modules || []);
    const modBlock = mods.length
        ? el('div', {class: 'chips'}, mods.map((m) => el('span', {class: 'chip', title: m.installed ? 'поставлен ' + m.installed : ''}, m.name)))
        : el('div', {class: 'muted'}, 'Модулей установщика нет (узел принят под управление или ещё не настроен)');
    let checks;
    if (!st.watch) checks = el('div', {class: 'muted'}, 'Мониторинг не стоит — проверок нет');
    else if (!st.failed || !st.failed.length) checks = el('div', {class: 'checks ok'}, `✓ Проверок ${st.checks}, все прошли`);
    else checks = el('div', {class: 'checks bad'}, `✗ Проверок ${st.checks}, не прошло ${st.failed.length}:`,
        el('ul', {class: 'fails'}, st.failed.map((f) => el('li', {}, f))));
    return el('div', {},
        el('h3', {}, 'Состояние'), facts,
        el('h3', {style: 'margin-top:14px'}, 'Модули'), modBlock,
        el('h3', {style: 'margin-top:14px'}, 'Проверки'), checks);
}

// ---- клиенты --------------------------------------------------------------

async function loadClients(name) {
    clientCache[name] = {loading: true};
    if (current === name) renderNode();
    try {
        clientCache[name] = await Clients(name);
    } catch (e) {
        clientCache[name] = {error: errText(e)};
    }
    if (current === name) renderNode();
}

function clientsCard(node) {
    const c = clientCache[node];
    const head = el('h3', {class: 'bar'}, 'Клиенты',
        el('button', {class: 'ghost small', onclick: () => OpenClientsFolder(node).catch((e) => toast(errText(e), true))}, 'Папка с файлами'));
    if (!c || c.loading) return el('div', {class: 'card'}, head, el('div', {class: 'muted'}, 'Загружаю список…'));
    if (c.error) return el('div', {class: 'card'}, head, el('div', {class: 'err'}, c.error));
    const errors = c.errors || {};
    const awg = new Set(c.awg || []);
    const subs = new Set(c.sub || []);
    const published = new Set(c.published || []);
    const names = [...new Set([...subs, ...awg])].sort((a, b) => a.localeCompare(b));
    const kinds = ['sub', 'awg'].filter((kind) => !errors[kind]);
    if (c.combined && c.delivery && kinds.length === 2) kinds.unshift('combined');
    const feeds = el('div', {class: 'access-formats'},
        el('div', {}, el('strong', {}, 'Подписка'), el('span', {}, 'VLESS XHTTP · VLESS PQ · Hysteria2')),
        el('div', {}, el('strong', {}, 'AmneziaWG'), el('span', {}, 'В единой подписке для INCY на iOS и Android')));
    const table = el('div', {class: 'access-list'},
        names.length ? el('div', {class: 'access-row access-columns'},
            el('span', {}, 'Устройство'), el('span', {}, 'Подписка'), el('span', {}, 'AmneziaVPN')) : null,
        names.length ? names.map((name) => {
            const hasSub = subs.has(name), hasAWG = awg.has(name), isPublished = published.has(name);
            let status = 'Профиль не создан';
            if (isPublished) status = 'Ссылка опубликована';
            else if (hasAWG && hasSub && c.delivery === true) status = 'Готов к публикации';
            else if (hasSub && c.delivery === false) status = 'Модуль подписок выключен';
            else if (hasSub && c.delivery == null) status = 'Статус в карточке подписки';
            else if (hasAWG) status = 'Импорт по QR или из файла';
            return el('div', {class: 'access-row'},
                el('strong', {class: 'access-name'}, name),
                el('div', {class: 'access-cell'}, el('span', {class: 'access-mobile-label'}, 'Подписка'),
                    hasSub ? clientOpenButton(node, 'sub', name, 'Открыть подписку') : el('span', {class: 'muted'}, 'Нет подписки')),
                el('div', {class: 'access-cell'}, el('span', {class: 'access-mobile-label'}, 'AmneziaVPN'),
                    el('span', {class: isPublished ? 'access-state published' : 'access-state'}, status),
                    hasAWG ? clientOpenButton(node, 'awg', name, 'Открыть профиль') : null,
                    !hasAWG && hasSub && !errors.awg && /^[A-Za-z0-9_-]{1,32}$/.test(name)
                        ? el('button', {class: 'ghost small', onclick: (e) => addClient(node, 'awg', name, e.currentTarget)}, 'Создать профиль') : null));
        }) : el('p', {class: 'muted access-empty'}, 'Устройств пока нет.'));
    return el('div', {class: 'card access-card'}, head, feeds, table,
        errors.sub ? el('p', {class: 'muted'}, errors.sub) : null,
        errors.awg ? el('p', {class: 'muted'}, errors.awg) : null,
        kinds.length ? clientForm(node, kinds) : null,
        el('details', {class: 'access-tg'},
            el('summary', {}, 'Прокси Telegram', (c.tg || []).length ? ` · ${c.tg.length}` : ''),
            errors.tg ? el('p', {class: 'muted'}, errors.tg) : [
                el('div', {class: 'access-proxies'}, (c.tg || []).map((name) => clientOpenButton(node, 'tg', name, name))),
                clientForm(node, ['tg'])]));
}

function clientOpenButton(node, kind, name, label) {
    return el('button', {class: 'ghost small', onclick: () => openCard(node, kind, name)}, label);
}

function clientForm(node, kinds) {
    const input = el('input', {placeholder: 'Имя устройства', 'aria-label': 'Имя устройства', maxlength: '32', required: ''});
    const select = el('select', {'aria-label': 'Тип подключения'}, kinds.map((kind) => el('option', {value: kind}, CLIENT_TYPES[kind])));
    const button = el('button', {type: 'submit'}, 'Создать');
    return el('form', {class: 'access-form', onsubmit: (e) => {
        e.preventDefault();
        addClient(node, select.value, input.value.trim(), button);
    }}, input, select, button);
}

async function addClient(node, kind, name, button) {
    const pattern = kind === 'combined' ? /^[A-Za-z0-9-][A-Za-z0-9_-]{0,31}$/
        : kind === 'awg' ? /^[A-Za-z0-9_-]{1,32}$/ : /^[A-Za-z0-9.-][A-Za-z0-9._-]{0,31}$/;
    if (!pattern.test(name)) {
        const message = kind === 'combined' ? 'Имя: латиница, цифры, дефис и подчёркивание, до 32 знаков. Первый символ не может быть подчёркиванием.'
            : kind === 'awg' ? 'Имя профиля: латиница, цифры, дефис и подчёркивание, до 32 знаков.'
                : 'Имя: латиница, цифры, точка, дефис и подчёркивание, до 32 знаков. Первый символ не может быть подчёркиванием.';
        toast(message, true);
        return;
    }
    const label = button.textContent;
    button.disabled = true;
    button.textContent = 'Создаю…';
    try {
        const card = await AddClient(node, kind, name);
        loadClients(node);
        showCard(card, true);
    } catch (e) {
        toast(errText(e), true);
        button.disabled = false;
        button.textContent = label;
    }
}

async function openCard(node, kind, name) {
    try {
        showCard(await ShowClient(node, kind, name), false);
    } catch (e) {
        toast(errText(e), true);
    }
}

function showCard(card, fresh) {
    const copy = el('button', {class: 'ghost', onclick: async () => {
        await navigator.clipboard.writeText(card.qr);
        toast('Ссылка скопирована');
    }}, 'Копировать ссылку');
    const copyAll = el('button', {class: 'ghost', onclick: async () => {
        await navigator.clipboard.writeText(card.body);
        toast('Текст скопирован');
    }}, 'Копировать всё');
    const amnezia = card.kind === 'sub' ? el('button', {class: 'ghost'},
        card.amnezia_url ? 'Убрать AmneziaWG из подписки' : 'Добавить AmneziaWG в подписку') : null;
    if (amnezia) {
        amnezia.addEventListener('click', async () => {
            const publish = !card.amnezia_url;
            amnezia.disabled = true;
            try {
                showCard(await PublishSubAmnezia(card.node, card.name, publish), false);
                loadClients(card.node);
            } catch (e) {
                toast(errText(e), true);
                amnezia.disabled = false;
            }
        });
    }
    const copyAmnezia = card.amnezia_url ? el('button', {class: 'ghost', onclick: async () => {
        await navigator.clipboard.writeText(card.amnezia_url);
        toast('Ссылка AmneziaVPN скопирована');
    }}, 'Копировать ссылку AmneziaVPN') : null;
    const copyCompat = card.compat_url ? el('button', {class: 'ghost', onclick: async () => {
        await navigator.clipboard.writeText(card.compat_url);
        toast('Ссылка без AWG скопирована');
    }}, 'Копировать подписку без AWG') : null;
    const del = el('button', {class: 'danger'}, 'Удалить клиента');
    let armed = false;
    del.addEventListener('click', async () => {
        if (!armed) {   // второе нажатие — подтверждение
            armed = true; del.classList.add('sure'); del.textContent = `Точно удалить ${card.name}?`;
            setTimeout(() => { armed = false; del.classList.remove('sure'); del.textContent = 'Удалить клиента'; }, 4000);
            return;
        }
        del.disabled = true; del.textContent = 'Удаляю…';
        try {
            await DelClient(card.node, card.kind, card.name);
            toast(`${card.name} удалён: доступ закрыт`);
            closeModal();
            delete clientCache[card.node];
            loadClients(card.node);
        } catch (e) {
            toast(errText(e), true);
            del.disabled = false; del.textContent = 'Удалить клиента';
        }
    });
    $('modal-body').replaceChildren(el('div', {class: 'client'},
        el('img', {src: card.qrImage, alt: 'QR'}),
        el('div', {},
            el('h2', {}, card.name), el('div', {class: 'muted'}, card.title),
            fresh ? el('div', {class: 'checks ok', style: 'margin-top:6px'}, '✓ Клиент создан') : null,
            el('p', {}, card.hint),
            el('pre', {}, card.body),
            el('div', {class: 'row'}, copy, copyAll, el('button', {class: 'ghost', onclick: () => OpenClientsFolder(card.node)}, 'Папка с файлами')),
            card.kind === 'sub' ? el('div', {},
                el('div', {class: 'row'}, amnezia, copyCompat, copyAmnezia),
                el('div', {class: 'muted'}, card.compat_url
                    ? 'Удаление AWG отзывает единую ссылку и файл AmneziaVPN. Подписка без AWG продолжит работать.'
                    : 'Добавляется одноимённый профиль AmneziaWG. Один профиль на устройство.')) : null,
            el('div', {class: 'warnline'}, 'Ссылка и QR — пропуск. Передавайте только самому человеку.'),
            el('div', {class: 'row', style: 'margin-top:18px'}, del))));
    $('modal').hidden = false;
}

$('modal-close').addEventListener('click', closeModal);
$('modal').addEventListener('click', (e) => { if (e.target === $('modal')) closeModal(); });
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeModal(); });

// ---- верх -----------------------------------------------------------------

$('pick').addEventListener('click', async () => {
    try {
        Object.keys(statusCache).forEach((k) => delete statusCache[k]);
        Object.keys(clientCache).forEach((k) => delete clientCache[k]);
        current = null;
        await loadState(await PickInventory());
    } catch (e) {
        toast(errText(e), true);
    }
});
$('refresh-all').addEventListener('click', () => state.nodes.forEach((n) => refreshStatus(n.name)));
$('alerts').addEventListener('click', () => openAlerts());
$('versions').addEventListener('click', () => openVersions());
$('new').addEventListener('click', () => {
    current = null;
    renderInventory();
    startWizard(async () => {   // мастер создал и открыл новый inventory
        Object.keys(statusCache).forEach((k) => delete statusCache[k]);
        Object.keys(clientCache).forEach((k) => delete clientCache[k]);
        await loadState();
    });
});

window.krotUnlock = Unlock;   // для будущего окна пароля хранилища
loadState();
initAppUpdate();
