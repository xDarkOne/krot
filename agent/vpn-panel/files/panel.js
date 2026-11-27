const tg = window.Telegram?.WebApp;
const statusText = document.querySelector('#status');
const profiles = document.querySelector('#profiles');
const detail = document.querySelector('#detail');
const create = document.querySelector('#create');
const nameInput = document.querySelector('#name');
const combined = document.querySelector('#combined');

async function api(action, args = {}) {
    const response = await fetch('api/', {
        method: 'POST',
        headers: {'Content-Type': 'application/json', 'X-Telegram-Init-Data': tg.initData},
        body: JSON.stringify({action, ...args}),
    });
    const value = await response.json();
    if (!response.ok) throw new Error(value.error || 'Запрос завершился ошибкой');
    return value;
}

function node(tag, text) {
    const element = document.createElement(tag);
    if (text) element.textContent = text;
    return element;
}

async function run(callback) {
    statusText.textContent = '';
    try {
        await callback();
    } catch (error) {
        statusText.textContent = error.message;
    }
}

function button(text, callback) {
    const control = node('button', text);
    control.addEventListener('click', async () => {
        control.disabled = true;
        try { await run(callback); } finally { control.disabled = false; }
    });
    return control;
}

function show(card) {
    const name = card.name;
    detail.hidden = false;
    detail.replaceChildren(node('h2', name));
    if (Number.isFinite(card.traffic_24h)) {
        const mb = (card.traffic_24h / 1000000).toLocaleString('ru-RU', {maximumFractionDigits: 2});
        detail.append(node('p', `Трафик за сутки: ${mb} МБ`));
    }
    for (const [label, value] of [
        ['Подписка', card.sub_url],
        ['Открыть в INCY', card.incy_url],
        ['Подписка без AWG', card.compat_url],
        ['Профиль AmneziaVPN', card.amnezia_url],
        ['Маршруты Happ', card.routing?.happ],
        ['Маршруты INCY', card.routing?.incy],
        ['Прокси Telegram', card.telegram_proxy?.https],
    ]) {
        if (!value) continue;
        const line = node('p');
        line.className = 'row';
        line.append(node('span', label), button('Копировать', async () => {
            await navigator.clipboard.writeText(value);
            statusText.textContent = 'Ссылка скопирована';
        }));
        detail.append(line);
    }

    if (card.has_mtproxy && !card.telegram_proxy) {
        detail.append(button('Создать персональный прокси Telegram', async () => {
            show(await api('tg-add', {name}));
        }));
    }
    const label = node('label', 'День оплаты');
    const day = node('input');
    day.type = 'number';
    day.min = 1;
    day.max = 31;
    day.value = card.payment_day ?? '';
    label.append(day);
    detail.append(label, button('Сохранить день', async () => {
        show(await api('payment', {name, day: day.value ? day.valueAsNumber : null}));
    }));

    detail.append(node('h3', 'Устройства подписки'));
    if (!card.devices.length) {
        detail.append(node('p', 'Устройств с идентификатором пока нет'));
    } else {
        const list = node('ul');
        for (const device of card.devices) {
            const line = node('li');
            const when = new Date(device.last_seen * 1000).toLocaleString();
            line.append(node('span', `${device.platform} · ${when} `),
                button('Убрать', async () => show(await api('remove_device', {name, id: device.id}))));
            list.append(line);
        }
        detail.append(list, button('Сбросить список', async () => {
            if (window.confirm('Очистить историю устройств этого профиля?')) {
                show(await api('reset_devices', {name}));
            }
        }));
    }
    detail.append(node('p'), button('Удалить подписку', async () => {
        if (!window.confirm(`Удалить подписку «${name}»? Ссылки перестанут работать.`)) return;
        await api('delete', {name});
        detail.hidden = true;
        detail.replaceChildren();
        await list();
    }));
}

async function list() {
    const {names, can_issue_awg} = await api('list');
    document.querySelector('#awg-choice').hidden = !can_issue_awg;
    if (!can_issue_awg) combined.checked = false;
    profiles.replaceChildren(...names.map(name => button(name, async () => show(await api('profile', {name})))));
}

if (tg?.initData) {
    tg.ready();
    create.hidden = false;
    create.addEventListener('submit', event => {
        event.preventDefault();
        run(async () => {
            const card = await api('create', {name: nameInput.value.trim(), combined: combined.checked});
            nameInput.value = '';
            show(card);
            await list();
        });
    });
    run(list);
} else {
    statusText.textContent = 'Откройте панель кнопкой в личном чате с ботом.';
}
