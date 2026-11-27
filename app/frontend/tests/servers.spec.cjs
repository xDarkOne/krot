const {test, expect, entry, exit} = require('./desktop-fixture.cjs');

test('switches between servers inside one cascade and checks the selected server', async ({page, openDesktop}) => {
    const calls = await openDesktop();
    const info = page.getByRole('region', {name: 'Серверы'});
    const switcher = info.getByRole('group', {name: 'Выбор сервера'});
    const entryButton = switcher.getByRole('button', {name: /192\.0\.2\.4/});
    const exitButton = switcher.getByRole('button', {name: /192\.0\.2\.5/});
    await expect(page.locator('#nodes').getByRole('button')).toHaveCount(1);
    await expect(entryButton).toHaveAttribute('aria-pressed', 'true');
    await expect(info.getByText(/Ubuntu/)).toBeVisible();
    await expect(page.getByRole('heading', {name: 'Клиенты'})).toBeVisible();
    await expect.poll(() => calls('Status').map(([name]) => name).sort()).toEqual([exit.name, entry.name]);

    await exitButton.press('Enter');
    await expect(exitButton).toBeFocused();
    await expect(exitButton).toHaveAttribute('aria-pressed', 'true');
    await expect(info.getByText(/Debian/)).toBeVisible();
    await expect(page.getByRole('heading', {name: 'Клиенты'})).toHaveCount(0);
    await info.getByRole('button', {name: 'Проверить', exact: true}).click();
    await expect.poll(() => calls('Status').slice(2)).toEqual([[exit.name]]);
});

test('reports traffic and confirms a reboot for the intended server', async ({page, openDesktop}) => {
    const calls = await openDesktop({
        ServerStatistics: (name, days) => {
            const isEntry = name === entry.name;
            return {
                uptime: isEntry ? '8 дней 16 ч' : '19 дней 3 ч',
                booted_at: 1795129200, interface: 'eth0', rx: 4000000000, tx: 8000000000,
                has_history: isEntry,
                traffic: isEntry ? Array.from({length: days}, (_, i) => ({
                    at: 1795752000 - (days - 1 - i) * 86400,
                    bytes: {awg: 1000000000, xhttp: 2000000000, pq: 3000000000, hy2: 4000000000},
                })) : [],
            };
        },
        RestartServer: () => {},
    });
    const info = page.getByRole('region', {name: 'Серверы'});
    await info.getByRole('button', {name: 'Статистика'}).click();
    const dashboard = page.locator('.dashboard');
    await expect(dashboard.getByText('8 дней 16 ч')).toBeVisible();
    await expect(dashboard.getByText('19 дней 3 ч')).toBeVisible();
    await expect(dashboard.locator('.dashboard-bar')).toHaveCount(7);
    await dashboard.getByRole('button', {name: 'Месяц'}).click();
    await expect(dashboard.locator('.dashboard-bar')).toHaveCount(30);
    expect(calls('ServerStatistics').sort()).toEqual([
        [exit.name, 30], [exit.name, 7], [entry.name, 30], [entry.name, 7],
    ]);

    const target = dashboard.getByRole('article').filter({hasText: exit.host});
    const reboot = target.getByRole('button', {name: 'Перезагрузить', exact: true});
    await reboot.click();
    await target.getByRole('button', {name: 'Отмена'}).click();
    expect(calls('RestartServer')).toEqual([]);
    await reboot.click();
    await target.getByRole('button', {name: 'Подтвердить перезагрузку'}).click();
    await expect(target.getByText(/Перезагрузка запрошена/)).toBeVisible();
    await expect(reboot).toBeDisabled();
    expect(calls('RestartServer')).toEqual([[exit.name, exit.host]]);
});

test('creates and verifies a backup separately from downloading an existing one', async ({page, openDesktop}) => {
    let finishBackup;
    let archives = [];
    const file = {name: 'new.tar.gz.age', at: '27.11.2026 14:52', size: 4096, path: 'mock'};
    const calls = await openDesktop({
        Backups: (node) => node === exit.name ? archives : [],
        CreateBackup: async () => {
            await new Promise((resolve) => { finishBackup = resolve; });
            archives = [file];
            return {file, files: 12};
        },
        PullBackup: () => ({file: {name: 'previous.tar.gz.age'}, files: 8}),
    });
    await page.getByRole('group', {name: 'Выбор сервера'}).getByRole('button', {name: /192\.0\.2\.5/}).click();
    const panel = page.locator('.card').filter({has: page.getByRole('heading', {name: 'Резервные копии'})});
    const create = panel.getByRole('button', {name: 'Создать копию сейчас'});
    const pull = panel.getByRole('button', {name: 'Забрать свежую копию'});
    await create.click();
    await expect(panel.getByRole('button', {name: 'Создаю и скачиваю…'})).toBeDisabled();
    await expect(pull).toBeDisabled();
    await expect.poll(() => typeof finishBackup).toBe('function');
    finishBackup();
    await expect(panel.getByText(/new\.tar\.gz\.age.*12 файлов/)).toBeVisible();
    await expect(panel.getByText('27.11.2026 14:52 · 4 КБ')).toBeVisible();
    await pull.click();
    await expect(panel.getByText(/previous\.tar\.gz\.age.*8 файлов/)).toBeVisible();
    expect(calls('CreateBackup')).toEqual([[exit.name]]);
    expect(calls('PullBackup')).toEqual([[exit.name]]);
});
