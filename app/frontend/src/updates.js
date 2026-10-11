// Обновления модулей: на узлах стоит версия кода старее, чем в этом пульте.
// Ход обновления приходит событиями deploy-log (как у установки в мастере) в #wlog/#wbar/#wcap.
import {Updates, ApplyUpdates} from '../wailsjs/go/main/App';
import {$, el, toast, errText} from './util.js';

let plan = [];
let onDone = null;

export async function checkUpdates(done) {
    onDone = done;
    const bar = $('upd');
    try {
        plan = (await Updates()) || [];
    } catch (e) {
        plan = [];   // нет связи с узлом — об этом скажет экран узла
    }
    if (!plan.length) { bar.hidden = true; return; }
    const nodes = [...new Set(plan.map((s) => s.node))].join(', ');
    bar.replaceChildren(
        el('span', {}, `Доступны обновления: модулей ${plan.length} (${nodes}).`),
        el('button', {class: 'small', onclick: openUpdates}, 'Подробнее'));
    bar.hidden = false;
}

function openUpdates() {
    const go = el('button', {}, 'Обновить');
    const result = el('div', {});
    go.addEventListener('click', async () => {
        go.disabled = true; go.textContent = 'Обновляю…';
        try {
            await ApplyUpdates();
            $('wbar').style.width = '100%';
            result.replaceChildren(el('div', {class: 'checks ok'}, '✓ Все модули обновлены и проверены.'));
            toast('Обновление завершено');
            $('upd').hidden = true;
            if (onDone) onDone();
        } catch (e) {
            result.replaceChildren(el('div', {class: 'checks bad'}, '✗ ' + errText(e) + ' — шаг откатан; «Обновить» продолжит с него.'));
            go.disabled = false; go.textContent = 'Обновить';
        }
    });
    $('modal-body').replaceChildren(
        el('h2', {}, 'Обновления'),
        el('p', {class: 'muted'}, 'В этой версии приложения изменились скрипты или закреплённые версии программ. Модули обновляются по одному: снимок → откат по таймеру, если связь пропадёт → установка → проверка. Службы перезапускаются, только если их настройки или программа действительно изменились.'),
        el('ul', {class: 'verdict'}, plan.map((s) => el('li', {}, `→ ${s.title} · ${s.node}`))),
        el('div', {id: 'wcap', class: 'muted'}), el('div', {class: 'pbar'}, el('div', {id: 'wbar', style: 'width:0%'})),
        el('pre', {class: 'install-log', id: 'wlog'}),
        result,
        el('div', {class: 'row'}, go));
    $('modal').hidden = false;
}
