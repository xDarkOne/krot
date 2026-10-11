// Обновление самого приложения: при запуске и раз в 6 часов смотрим последний релиз;
// если он новее — полоса сверху с кнопкой «Обновить» (скачать, сверить, перезапустить).
import {Version, CheckUpdate, ApplyUpdate} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {$, el, toast, errText} from './util.js';

const EVERY = 6 * 60 * 60 * 1000;

export async function initAppUpdate() {
    try {
        const v = await Version();
        $('ver').textContent = v === 'dev' ? 'dev' : 'v' + v;
    } catch (e) { /* без версии — не беда */ }
    check();
    setInterval(check, EVERY);
}

// проверка без мыши: KROT_APP_OPEN=update — сразу обновиться, если есть куда
export async function updateNow() {
    try { await ApplyUpdate(); } catch (e) { toast('Обновление не удалось: ' + errText(e), true); }
}

EventsOn('update-log', (l) => { const s = $('appupd-step'); if (s) s.textContent = l; });

async function check() {
    const bar = $('appupd');
    let c;
    try { c = await CheckUpdate(); } catch (e) { return; }
    if (!c.newer) { bar.hidden = true; return; }
    const go = el('button', {class: 'small'}, c.canApply ? 'Обновить' : 'Скачать');
    const step = el('span', {id: 'appupd-step', class: 'small'});
    go.addEventListener('click', async () => {
        if (!c.canApply) { window.open(c.url, '_blank'); return; }
        go.disabled = true; go.textContent = 'Обновляю…';
        try {
            await ApplyUpdate();   // при успехе окно закроется и откроется новая версия
        } catch (e) {
            toast('Обновление не удалось: ' + errText(e), true);
            go.disabled = false; go.textContent = 'Обновить';
        }
    });
    bar.replaceChildren(...[
        el('span', {}, `Доступна новая версия Крота: ${c.latest} (у вас ${c.current}).`),
        c.canApply ? null : el('span', {class: 'small'}, c.reason),
        go, step].filter(Boolean));
    bar.hidden = false;
}
