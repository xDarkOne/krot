const path = require('node:path');
const {test: base, expect} = require('@playwright/test');

const entry = {name: 'ru', role: 'entry', host: '192.0.2.4'};
const exit = {name: 'eu', role: 'exit', host: '192.0.2.5'};
const dist = path.resolve(__dirname, '../dist');
const origin = 'http://krot.test';

const test = base.extend({
    openDesktop: async ({page}, use) => {
        const errors = [];
        const unexpected = [];
        page.on('pageerror', (error) => errors.push(error.message));
        await page.route('**/*', (route) => route.abort());
        await page.route(origin + '/**', (route) => {
            const {pathname} = new URL(route.request().url());
            return route.fulfill({path: path.join(dist, pathname === '/' ? 'index.html' : pathname.slice(1))});
        });
        await use(async (overrides = {}) => {
            const calls = [];
            const methods = {
                GetState: () => ({inventory: 'inventory.yaml', nodes: [entry, exit]}),
                Status: (node) => ({reachable: true, os: node === entry.name ? 'Ubuntu' : 'Debian', kernel: '6.12'}),
                Clients: () => ({errors: {}}),
                Modules: () => [],
                Backups: () => [],
                Updates: () => [],
                CheckUpdate: () => ({newer: false}),
                Version: () => 'dev',
                ...overrides,
            };
            await page.exposeFunction('krotBridge', (name, args) => {
                calls.push([name, ...args]);
                if (!Object.hasOwn(methods, name)) {
                    unexpected.push(name);
                    throw new Error(`Unexpected Wails call: ${name}`);
                }
                return methods[name](...args);
            });
            await page.addInitScript(() => {
                const app = new Proxy({}, {
                    get: (_, method) => (...args) => window.krotBridge(method, args),
                });
                window.go = {main: {App: app}};
                window.runtime = {EventsOnMultiple: () => {}};
            });
            await page.goto(origin);
            return (method) => calls.filter(([name]) => name === method).map(([, ...args]) => args);
        });
        expect(errors, 'uncaught errors in the desktop frontend').toEqual([]);
        expect(unexpected, 'unmocked Wails calls').toEqual([]);
    },
});

module.exports = {test, expect, entry, exit};
