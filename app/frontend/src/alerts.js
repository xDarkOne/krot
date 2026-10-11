// Окно «Оповещения в Telegram»: токен бота → кто ему писал (свой chat id) → тест → применить на узлах.
import {AlertsState, AlertsCheck, AlertsSave, AlertsApply} from '../wailsjs/go/main/App';
import {$, el, toast, errText} from './util.js';

export async function openAlerts() {
    let st = {configured: false, chats: '', via: ''};
    try { st = await AlertsState(); } catch (e) { toast(errText(e), true); return; }
    const token = el('input', {type: 'password', autocomplete: 'off', style: 'width:100%',
        placeholder: st.configured ? 'сохранён — оставьте пустым, чтобы не менять' : '123456789:AA…'});
    const chats = el('input', {value: st.chats, style: 'width:100%', placeholder: 'chat id через пробел'});
    const found = el('div', {});
    const status = el('div', {});
    const check = el('button', {class: 'ghost'}, 'Проверить бота');
    const save = el('button', {}, 'Отправить тест и сохранить');
    const apply = el('button', {class: 'ghost', disabled: st.configured ? null : ''}, 'Применить на узлах');

    check.addEventListener('click', async () => {
        check.disabled = true; check.textContent = 'Проверяю…';
        try {
            const r = await AlertsCheck(token.value.trim());
            const items = (r.chats || []).map((c) => el('li', {},
                el('button', {class: 'ghost small', onclick: () => {
                    const have = chats.value.split(/[\s,;]+/).filter(Boolean);
                    if (!have.includes(c.id)) chats.value = [...have, c.id].join(' ');
                }}, 'добавить'), ` ${c.id} — ${c.name}`));
            found.replaceChildren(el('div', {class: 'checks ok'}, `✓ Бот ${r.bot}`),
                items.length ? el('div', {}, el('div', {class: 'muted small'}, 'Писали боту недавно:'), el('ul', {class: 'verdict'}, items))
                    : el('div', {class: 'muted small'}, 'Боту никто не писал. Откройте его в Telegram, отправьте /start и проверьте снова — тут появится ваш id.'));
        } catch (e) {
            found.replaceChildren(el('div', {class: 'err'}, errText(e)));
        }
        check.disabled = false; check.textContent = 'Проверить бота';
    });
    save.addEventListener('click', async () => {
        save.disabled = true; save.textContent = 'Отправляю…';
        try {
            await AlertsSave(token.value.trim(), chats.value);
            status.replaceChildren(el('div', {class: 'checks ok'}, '✓ Тестовое сообщение дошло, настройки сохранены. Осталось применить на узлах.'));
            apply.disabled = false;
        } catch (e) {
            status.replaceChildren(el('div', {class: 'err'}, errText(e)));
        }
        save.disabled = false; save.textContent = 'Отправить тест и сохранить';
    });
    apply.addEventListener('click', async () => {
        apply.disabled = true; apply.textContent = 'Применяю…';
        status.replaceChildren(el('div', {class: 'muted'}, 'Переустанавливаю мониторинг на узлах — по одному, с проверкой…'));
        try {
            const done = await AlertsApply();
            status.replaceChildren(el('div', {class: 'checks ok'},
                done && done.length ? `✓ Узлы шлют оповещения: ${done.join(', ')}` : 'Мониторинг не стоит ни на одном узле — поставьте модуль «Мониторинг».'));
        } catch (e) {
            status.replaceChildren(el('div', {class: 'err'}, errText(e)));
        }
        apply.disabled = false; apply.textContent = 'Применить на узлах';
    });

    $('modal-body').replaceChildren(
        el('h2', {}, 'Оповещения в Telegram'),
        el('p', {class: 'muted'}, 'Узлы пишут в Telegram, когда что-то сломалось и когда починилось. Нужен свой бот:'),
        el('ol', {class: 'need'},
            el('li', {}, 'В Telegram откройте @BotFather → /newbot → придумайте имя. Он пришлёт токен.'),
            el('li', {}, 'Откройте своего бота и отправьте ему /start — так он узнает, куда писать.'),
            el('li', {}, 'Вставьте токен ниже и нажмите «Проверить бота».')),
        el('div', {class: 'form'}, el('label', {}, 'Токен бота', token), el('label', {}, 'Кому писать (chat id)', chats)),
        found,
        el('div', {class: 'row'}, check, save, apply),
        status,
        el('div', {class: 'muted small', style: 'margin-top:10px'},
            `Запросы к Telegram идут с узла ${st.via}: из России Telegram API без VPN часто недоступен. Токен хранится в зашифрованном хранилище.`));
    $('modal').hidden = false;
}
