// Крот — окно пульта. Данные с узлов попадают в страницу только как текст (textContent):
// имена клиентов и тексты проверок приходят по сети.
import {
    GetState, PickInventory, Unlock, Status, Clients,
    AddClient, ShowClient, DelClient, OpenClientsFolder,
    Modules, Install, Answer, Backups, PullBackup, RestoreClients,
} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {$, el, toast, errText} from './util.js';
import {startWizard} from './wizard.js';
import {initAppUpdate, updateNow} from './appupdate.js';
import {openAlerts} from './alerts.js';
import {checkUpdates} from './updates.js';
import {openVersions} from './versions.js';

const statusCache = {};   // узел → последний Status
let state = {nodes: []};
let current = null;

EventsOn('log', (s) => toast(s));

const ROLE = {entry: 'вход', exit: 'выход', solo: 'один сервер'};
const KIND = {
    awg: {title: 'AmneziaWG', what: 'Приложение AmneziaVPN, весь трафик телефона или компьютера'},
    sub: {title: 'Подписка', what: 'Happ, v2rayNG, Hiddify: hysteria2, XHTTP, PQ одной ссылкой'},
    tg: {title: 'Telegram', what: 'Прокси только для Telegram, без приложений'},
};

// ---- список узлов ---------------------------------------------------------

function dotClass(st) {
    if (!st) return 'dot';
    if (st.busy) return 'dot busy';
    if (!st.reachable || st.error) return 'dot bad';
    if (st.watch && st.failed && st.failed.length) return 'dot warn';
    return 'dot ok';
}

function renderNodes() {
    const ul = $('nodes');
    ul.replaceChildren(...state.nodes.map((n) => {
        const st = statusCache[n.name];
        return el('li', {class: current === n.name ? 'active' : '', onclick: () => selectNode(n.name)},
            el('span', {class: dotClass(st)}),
            el('span', {class: 'nm'}, n.name),
            el('span', {class: 'sub'}, `${ROLE[n.role] || n.role} · ${n.host}`));
    }));
}

async function refreshStatus(name) {
    statusCache[name] = {...(statusCache[name] || {}), busy: true};
    renderNodes();
    if (current === name) renderNode();
    try {
        statusCache[name] = await Status(name);
    } catch (e) {
        statusCache[name] = {reachable: false, error: errText(e)};
    }
    await loadModules(name);
    renderNodes();
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
    const pull = el('button', {class: 'ghost small', disabled: st.busy ? '' : null}, st.busy ? 'Забираю…' : 'Забрать свежую копию');
    pull.addEventListener('click', async () => {
        st.busy = true; backupCache[n.name] = st; renderNode();
        try {
            const r = await PullBackup(n.name);
            st.report = r.missing && r.missing.length
                ? {bad: true, text: `✗ в копии нет ключевых файлов: ${r.missing.join(', ')}`}
                : {bad: false, text: `✓ ${r.file.name}: расшифрована ключом пульта, ${r.files} файлов, все модули на месте`};
        } catch (e) {
            st.report = {bad: true, text: errText(e)};
        }
        st.busy = false;
        await loadBackups(n.name);
    });
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
        el('h3', {class: 'bar'}, 'Резервные копии', pull),
        st.report ? el('div', {class: st.report.bad ? 'checks bad' : 'checks ok'}, st.report.text) : null,
        rows.length ? el('ul', {class: 'mods'}, rows) : el('div', {class: 'muted small'}, 'На этом компьютере копий узла пока нет. Сервер делает их сам каждую ночь — заберите свежую.'),
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
    renderNodes();
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
    renderNodes();
    renderNode();
    const n = state.nodes.find((x) => x.name === name);
    if (n && (n.role === 'entry' || n.role === 'solo') && !clientCache[name]) loadClients(name);
}

function renderNode() {
    const n = state.nodes.find((x) => x.name === current);
    if (!n) return;
    const st = statusCache[n.name];
    const main = $('main');
    const head = el('div', {class: 'node-head'},
        el('h1', {}, n.name), el('span', {class: 'role'}, ROLE[n.role] || n.role),
        el('span', {class: 'host'}, n.host), el('span', {class: 'grow'}),
        el('button', {class: 'ghost', disabled: st && st.busy ? '' : null, onclick: () => refreshStatus(n.name)},
            st && st.busy ? 'Проверяю…' : 'Проверить'));
    const blocks = [head, statusCard(st)];
    blocks.push(modulesCard(n.name));
    blocks.push(backupsCard(n));
    if (n.role === 'entry' || n.role === 'solo') blocks.push(clientsCard(n.name));
    else if (n.role === 'exit') blocks.push(el('div', {class: 'card muted'},
        'Клиенты подключаются к входу; выход только выпускает их трафик в интернет.'));
    main.replaceChildren(...blocks);
}

function statusCard(st) {
    if (!st || (st.busy && !st.at)) return el('div', {class: 'card muted'}, 'Связываюсь с узлом…');
    if (!st.reachable || st.error && !st.os) {
        return el('div', {class: 'card'}, el('h3', {}, 'Нет связи'), el('div', {class: 'err'}, st.error || 'узел не ответил'));
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
    return el('div', {class: 'card'},
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
    const cols = ['awg', 'sub', 'tg'].map((kind) => clientColumn(node, kind, c[kind] || [], (c.errors || {})[kind]));
    return el('div', {class: 'card'}, head, el('div', {class: 'cols'}, cols));
}

function clientColumn(node, kind, names, error) {
    const input = el('input', {placeholder: 'имя: латиница, цифры, - _', maxlength: '32'});
    const btn = el('button', {}, 'Добавить');
    const add = async () => {
        const name = input.value.trim();
        if (!/^[A-Za-z0-9._-]{1,32}$/.test(name)) { toast('Имя: латиница, цифры, точка, дефис, подчёркивание; до 32 знаков', true); return; }
        btn.disabled = true; btn.textContent = 'Создаю…';
        try {
            const card = await AddClient(node, kind, name);
            delete clientCache[node];
            loadClients(node);
            showCard(card, true);
        } catch (e) {
            toast(errText(e), true);
            btn.disabled = false; btn.textContent = 'Добавить';
        }
    };
    btn.addEventListener('click', add);
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') add(); });
    const body = error
        ? el('div', {class: 'muted'}, /модул/.test(error) ? 'Модуль не установлен на узле' : error)
        : [el('ul', {}, names.length ? names.map((n) => el('li', {onclick: () => openCard(node, kind, n)}, n, el('span', {class: 'muted'}, '›')))
                                     : el('li', {class: 'muted'}, 'пока никого')),
           el('div', {class: 'add'}, input, btn)];
    return el('div', {class: 'col'}, el('h3', {}, KIND[kind].title, el('span', {class: 'muted'}, names.length || '')),
        el('div', {class: 'what'}, KIND[kind].what), body);
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
            el('div', {class: 'warnline'}, 'Ссылка и QR — пропуск. Передавайте только самому человеку.'),
            el('div', {class: 'row', style: 'margin-top:18px'}, del))));
    $('modal').hidden = false;
}

function closeModal() { $('modal').hidden = true; $('modal-body').replaceChildren(); }
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
    renderNodes();
    startWizard(async () => {   // мастер создал и открыл новый inventory
        Object.keys(statusCache).forEach((k) => delete statusCache[k]);
        Object.keys(clientCache).forEach((k) => delete clientCache[k]);
        await loadState();
    });
});

window.krotUnlock = Unlock;   // для будущего окна пароля хранилища
loadState();
initAppUpdate();
