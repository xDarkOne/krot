const {test, expect, entry} = require('./desktop-fixture.cjs');

test('retries an unsuccessful module update and closes with Окей after success', async ({page, openDesktop}) => {
    let attempts = 0;
    let finishUpdate;
    const calls = await openDesktop({
        Updates: () => [{node: entry.name, title: 'Xray'}],
        ApplyUpdates: async () => {
            if (++attempts === 1) throw new Error('Тестовая ошибка обновления');
            await new Promise((resolve) => { finishUpdate = resolve; });
        },
    });
    await page.locator('#upd').getByRole('button', {name: 'Подробнее'}).click();
    const dialog = page.locator('#modal');
    const update = dialog.getByRole('button', {name: 'Обновить', exact: true});
    await update.click();
    await expect(dialog.getByText(/Тестовая ошибка обновления/)).toBeVisible();
    await expect(update).toBeEnabled();
    await update.click();
    await expect(dialog.getByRole('button', {name: 'Обновляю…'})).toBeDisabled();
    await expect.poll(() => typeof finishUpdate).toBe('function');
    finishUpdate();
    const ok = dialog.getByRole('button', {name: 'Окей', exact: true});
    await expect(ok).toBeFocused();
    await expect(ok).toBeEnabled();
    await page.keyboard.press('Enter');
    await expect(dialog).toBeHidden();
    await expect(page.locator('#upd')).toBeHidden();
    expect(calls('ApplyUpdates')).toHaveLength(2);
});

test('requires a successful check and SSH key setup before advancing in the wizard', async ({page, openDesktop}) => {
    let reachable = false;
    const calls = await openDesktop({
        WReach: () => reachable ? {ok: true, banner: 'SSH'} : {ok: false, problem: 'Узел недоступен'},
        WCheck: () => ({worst: 'ok', findings: []}),
        WBootstrap: () => {},
    });
    await page.locator('#new').click();
    const wizard = page.locator('.wizard');
    await wizard.locator('.scheme').filter({hasText: 'Один зарубежный сервер'}).getByRole('button', {name: 'Выбрать'}).click();
    const check = wizard.getByRole('button', {name: 'Проверить', exact: true});
    const key = wizard.getByRole('button', {name: 'Перевести на ключ'});
    const next = wizard.getByRole('button', {name: 'Дальше'});
    await wizard.getByLabel('IP-адрес').fill(entry.host);
    const credentialsField = wizard.getByLabel('Пароль root');
    await credentialsField.fill('fixture');
    await check.click();
    await expect(wizard.getByText(/Узел недоступен/)).toBeVisible();
    await expect(key).toBeDisabled();
    expect(calls('WCheck')).toEqual([]);

    reachable = true;
    await check.click();
    await expect(key).toBeEnabled();
    await expect(next).toBeDisabled();
    await key.click();
    await expect(credentialsField).toHaveValue('');
    await next.click();
    await expect(wizard.getByRole('heading', {name: 'Домен', exact: true})).toBeVisible();
    expect(calls('WBootstrap')).toEqual([[{
        name: 'vpn', host: entry.host, role: 'solo', rootPassword: 'fixture', via: '', viaName: '',
    }]]);
});

test('checks, saves and applies Telegram alert settings', async ({page, openDesktop}) => {
    const calls = await openDesktop({
        AlertsState: () => ({configured: false, chats: '', via: entry.name}),
        AlertsCheck: () => ({bot: 'fixture_bot', chats: [{id: '42', name: 'Operator'}]}),
        AlertsSave: () => {},
        AlertsApply: () => [entry.name],
    });
    await page.locator('#alerts').click();
    const dialog = page.locator('#modal');
    const apply = dialog.getByRole('button', {name: 'Применить на узлах'});
    await expect(apply).toBeDisabled();
    await dialog.getByLabel('Токен бота').fill('fixture');
    await dialog.getByRole('button', {name: 'Проверить бота'}).click();
    await expect(dialog.getByText(/fixture_bot/)).toBeVisible();
    await dialog.getByRole('button', {name: 'добавить', exact: true}).click();
    await expect(dialog.getByLabel('Кому писать (chat id)')).toHaveValue('42');
    await dialog.getByRole('button', {name: 'Отправить тест и сохранить'}).click();
    await expect(dialog.getByText(/настройки сохранены/)).toBeVisible();
    await apply.click();
    await expect(dialog.getByText(/Узлы шлют оповещения: ru/)).toBeVisible();
    expect(calls('AlertsCheck')).toEqual([['fixture']]);
    expect(calls('AlertsSave')).toEqual([['fixture', '42']]);
    expect(calls('AlertsApply')).toEqual([[]]);
});
