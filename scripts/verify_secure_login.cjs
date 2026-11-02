// Browser check for the local HTTPS edge. A self-signed local certificate is
// accepted only inside this test context; real deployments need a trusted CA.
const assert = require('node:assert/strict');
const playwright = require(process.env.PLAYWRIGHT_MODULE || './browser/node_modules/playwright');

(async () => {
    const browser = await playwright.chromium.launch({
        headless: true,
        ...(process.env.PLAYWRIGHT_EXECUTABLE ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE } : {}),
    });
    const context = await browser.newContext({ ignoreHTTPSErrors: true });
    const page = await context.newPage();
    const requests = [];
    const sockets = [];
    const errors = [];
    page.on('request', (request) => requests.push(request.url()));
    page.on('websocket', (socket) => sockets.push(socket.url()));
    page.on('pageerror', (error) => errors.push(error.message));
    try {
        await page.goto('https://127.0.0.1:8443/');
        await page.locator('#username').fill('userA');
        await page.locator('#password').fill('123456');
        await page.locator('#login-btn').click();
        await page.waitForFunction(() => document.querySelector('#status-text')?.textContent?.includes('在线 1001'), null, { timeout: 20000 });
        assert.ok(requests.some((url) => url.endsWith('/api/v1/login')));
        assert.ok(requests.some((url) => url.endsWith('/api/v1/ws-ticket')));
        assert.ok(sockets.length > 0, 'browser did not open a WebSocket');
        assert.ok(requests.every((url) => url.startsWith('https://')), `insecure request: ${requests.find((url) => !url.startsWith('https://'))}`);
        assert.ok(sockets.every((url) => url.startsWith('wss://') && url.includes('?ticket=') && !url.includes('token=')), `insecure WebSocket: ${sockets[0]}`);
        assert.deepEqual(errors, []);
        console.log('PASS browser HTTPS login, HTTPS ticket, WSS connection, no JWT in socket URL');
    } finally {
        await browser.close();
    }
})().catch((error) => { console.error(error); process.exitCode = 1; });
