// Окно «Версии»: эталон (что ставит этот пульт), что стоит на узлах, что вышло у авторов.
import {Versions} from '../wailsjs/go/main/App';
import {$, el, errText} from './util.js';

export async function openVersions() {
    $('modal-body').replaceChildren(el('h2', {}, 'Версии'), el('div', {class: 'muted'}, 'Спрашиваю узлы и GitHub…'));
    $('modal').hidden = false;
    let v;
    try { v = await Versions(); } catch (e) {
        $('modal-body').replaceChildren(el('h2', {}, 'Версии'), el('div', {class: 'err'}, errText(e)));
        return;
    }
    const head = el('tr', {}, el('th', {}, 'Компонент'), el('th', {}, 'Эталон'),
        v.nodes.map((n) => el('th', {}, n)), el('th', {}, 'Свежий'));
    const rows = v.rows.map((r) => {
        const cell = (x) => el('td', {class: x && x !== r.ref ? 'checks bad' : ''}, x || '—');
        return [
            el('tr', {}, el('td', {}, r.name), el('td', {}, el('code', {}, r.ref)),
                v.nodes.map((n) => cell(r.nodes[n])),
                el('td', {class: r.latest && r.latest !== r.ref ? 'checks' : 'muted'}, r.latest || '?')),
            el('tr', {}, el('td', {colspan: String(3 + v.nodes.length), class: 'muted small'}, r.note)),
        ];
    });
    const errs = Object.entries(v.errors || {}).map(([n, e]) => el('div', {class: 'err small'}, `${n}: ${e}`));
    $('modal-body').replaceChildren(
        el('h2', {}, 'Версии'),
        el('p', {class: 'muted'}, 'Эталон — версии, которые ставит это приложение. Новую версию сначала проверяют на тестовых серверах, потом поднимают в эталоне — и она приходит на узлы обновлением.'),
        ...errs,
        el('table', {class: 'dns'}, head, rows.flat()));
}
