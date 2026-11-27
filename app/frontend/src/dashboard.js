import {RestartServer, ServerStatistics} from '../wailsjs/go/main/App';
import {$, el, errText} from './util.js';

const PERIODS = [[1, 'Сутки'], [7, 'Неделя'], [30, 'Месяц']];
const PROTOCOLS = [
    ['awg', 'AmneziaWG', '#5699f5'], ['xhttp', 'VLESS XHTTP', '#46c6a4'],
    ['pq', 'VLESS PQ', '#f1ad49'], ['hy2', 'Hysteria2', '#e66a9e'],
];
const number = new Intl.NumberFormat('ru', {maximumFractionDigits: 1});
const date = new Intl.DateTimeFormat('ru', {dateStyle: 'medium', timeStyle: 'short'});

function bytes(value) {
    const units = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ', 'ПБ'];
    let i = 0;
    while (value >= 1024 && i < units.length - 1) { value /= 1024; i++; }
    return `${number.format(value)} ${units[i]}`;
}

function trafficChart(snapshots, days, loading) {
    if (loading) return el('p', {class: 'muted'}, 'Загружаю трафик…');
    const readouts = snapshots.filter((s) => s.has_history);
    if (!readouts.length) {
        const failed = snapshots.some((s) => s.history_error || s.error);
        return el('p', {class: 'muted'}, failed ? 'Не удалось прочитать историю трафика.'
            : 'История появится после запуска сборщика трафика vpn-stats.');
    }
    const buckets = new Map();
    for (const stats of readouts) {
        for (const bucket of stats.traffic || []) {
            const totals = buckets.get(bucket.at) || {};
            for (const [key] of PROTOCOLS) totals[key] = (totals[key] || 0) + (bucket.bytes[key] || 0);
            buckets.set(bucket.at, totals);
        }
    }
    const points = [...buckets.entries()].sort(([a], [b]) => a - b).slice(-(days === 1 ? 24 : days));
    const total = ([, values]) => PROTOCOLS.reduce((sum, [key]) => sum + values[key], 0);
    const peak = Math.max(0, ...points.map(total));
    const dateOptions = days === 1 ? {hour: '2-digit', minute: '2-digit'} : {day: '2-digit', month: '2-digit'};
    const formatter = new Intl.DateTimeFormat('ru', {...dateOptions, timeZone: 'UTC'});
    const chart = peak ? el('div', {class: 'dashboard-plot', role: 'img', 'aria-label': 'Диаграмма трафика по протоколам'},
        points.map((point) => el('div', {class: 'dashboard-bar',
            title: `${formatter.format(point[0] * 1000)} UTC · ${bytes(total(point))}`},
            PROTOCOLS.filter(([key]) => point[1][key]).map(([key, , color]) =>
                el('span', {style: `height:${point[1][key] * 100 / peak}%;background:${color}`}))))) :
        el('p', {class: 'muted'}, 'За этот период трафика нет.');
    const labelPoints = [...new Set([0, .25, .5, .75, 1].map((part) => Math.round(part * (points.length - 1))))];
    return el('div', {},
        el('strong', {class: 'dashboard-total'}, bytes(points.reduce((sum, point) => sum + total(point), 0))),
        snapshots.some((s) => !s.has_history) ? el('p', {class: 'muted'}, 'Данные о трафике доступны не для всех входов.') : null,
        chart,
        peak ? el('div', {class: 'dashboard-axis'}, labelPoints.map((i) =>
            el('span', {}, formatter.format(points[i][0] * 1000))), el('span', {}, 'UTC')) : null,
        el('div', {class: 'dashboard-legend'}, PROTOCOLS.map(([key, label, color]) =>
            el('div', {}, el('span', {class: 'dashboard-key', style: `background:${color}`, 'aria-hidden': 'true'}),
                label, el('strong', {}, bytes(points.reduce((sum, [, values]) => sum + values[key], 0)))))));
}

export function openDashboard(nodes) {
    const view = el('div', {class: 'dashboard'});
    let days = 7;
    let loading = false;
    let request = 0;
    let snapshots = {};
    let confirmation = null;
    const restarts = new Map();

    async function load() {
        const id = ++request;
        loading = true;
        render();
        const results = await Promise.all(nodes.map(async (node) => {
            try { return [node.name, await ServerStatistics(node.name, days)]; }
            catch (error) { return [node.name, {error: errText(error)}]; }
        }));
        if (id !== request || !view.isConnected) return;
        snapshots = Object.fromEntries(results);
        loading = false;
        for (const [name, restart] of restarts) {
            if (restart.requested && snapshots[name]?.booted_at && snapshots[name].booted_at !== restart.boot) {
                restarts.set(name, {done: true});
            }
        }
        render();
    }

    async function restart(node) {
        confirmation = null;
        const boot = snapshots[node.name]?.booted_at;
        restarts.set(node.name, {sending: true});
        render();
        try {
            await RestartServer(node.name, node.host);
            restarts.set(node.name, {requested: true, boot});
            setTimeout(() => { if (view.isConnected && !loading) load(); }, 12000);
        } catch (error) {
            restarts.set(node.name, {error: errText(error)});
        }
        if (view.isConnected) render();
    }

    function serverCard(node) {
        const s = snapshots[node.name];
        const result = restarts.get(node.name) || {};
        const confirm = confirmation === node.name;
        return el('article', {class: 'dashboard-server'},
            el('div', {class: 'dashboard-head'}, el('h3', {}, node.name), el('span', {class: 'muted'}, node.host)),
            s && !s.error ? el('dl', {class: 'dashboard-facts'},
                el('dt', {}, 'Время работы'), el('dd', {}, s.uptime),
                s.booted_at ? [el('dt', {}, 'Последний запуск'), el('dd', {}, date.format(s.booted_at * 1000))] : null,
                s.interface ? [
                    el('dt', {}, `Интерфейс ${s.interface}`),
                    el('dd', {title: 'Получено и отправлено с начала работы интерфейса'}, `↓ ${bytes(s.rx)} · ↑ ${bytes(s.tx)}`),
                ] : null)
                : el('p', {class: s?.error ? 'err' : 'muted'}, s?.error || 'Загружаю данные…'),
            result.sending ? el('p', {class: 'muted'}, 'Отправляю запрос…') : null,
            result.requested ? el('p', {class: 'muted'}, 'Перезагрузка запрошена. Проверяю время запуска.') : null,
            result.done ? el('p', {class: 'checks ok'}, 'Сервер снова доступен после перезагрузки.') : null,
            result.error ? el('p', {class: 'err'}, result.error) : null,
            confirm ? el('div', {class: 'dashboard-confirm'},
                el('p', {}, `Перезагрузить ${node.name} (${node.host})? Подключения к нему временно прервутся.`),
                el('div', {class: 'row'},
                    el('button', {class: 'danger', onclick: () => restart(node)}, 'Подтвердить перезагрузку'),
                    el('button', {class: 'ghost', onclick: () => { confirmation = null; render(); }}, 'Отмена')))
                : el('button', {class: 'danger small', disabled: !s || s.error || result.sending || result.requested ? '' : null,
                    onclick: () => { confirmation = node.name; render(); }}, 'Перезагрузить'));
    }

    function render() {
        view.replaceChildren(
            el('div', {class: 'dashboard-head dashboard-title'}, el('h2', {}, 'Статистика'),
                el('button', {class: 'ghost small', disabled: loading ? '' : null, onclick: load}, 'Обновить данные')),
            el('section', {class: 'dashboard-section'}, el('h3', {}, 'Трафик VPN'),
                el('div', {class: 'dashboard-periods', role: 'group', 'aria-label': 'Период трафика'},
                    PERIODS.map(([value, label]) => el('button', {class: 'small', 'aria-pressed': String(days === value),
                        onclick: () => { days = value; load(); }}, label))),
                trafficChart(nodes.filter((node) => node.role !== 'exit').map((node) => snapshots[node.name]).filter(Boolean), days, loading)),
            el('section', {class: 'dashboard-section'}, el('h3', {}, 'Серверы'),
                el('div', {class: 'dashboard-servers'}, nodes.map(serverCard))));
    }

    $('modal-body').replaceChildren(view);
    $('modal').hidden = false;
    load();
}
