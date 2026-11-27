const {defineConfig} = require('@playwright/test');

module.exports = defineConfig({
    testDir: './tests',
    forbidOnly: !!process.env.CI,
    use: {
        browserName: 'chromium',
        viewport: {width: 1150, height: 840},
        colorScheme: 'dark',
    },
});
