// Browser acceptance: run after starting the Docker stack and serving public/.
// Install with npm install --prefix scripts/browser and
// scripts/browser/node_modules/.bin/playwright install chromium.
const assert = require('node:assert/strict');
const playwright = require(process.env.PLAYWRIGHT_MODULE || './browser/node_modules/playwright');

const apiBase = process.env.LINKGO_API || 'http://127.0.0.1:8090';
const webBase = process.env.LINKGO_WEB || 'http://127.0.0.1:8088';
const requestID = () => `browser-${Date.now()}-${Math.random().toString(16).slice(2)}`;

async function post(path, token, body) {
    const response = await fetch(apiBase + path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
        body: JSON.stringify(body),
    });
    const value = await response.json();
    assert.ok(response.ok, `${path}: ${response.status} ${JSON.stringify(value)}`);
    return value;
}

async function loginPage(page, username, groupID) {
    await page.goto(webBase);
    await page.locator('#username').fill(username);
    await page.locator('#password').fill('123456');
    await page.locator('#login-btn').click();
    await page.waitForFunction(() => document.querySelector('#logs')?.textContent?.includes('登录初始化完成'), null, { timeout: 15000 });
    await page.waitForFunction(() => document.querySelector('#logs')?.textContent?.includes('WebSocket 已连接'), null, { timeout: 15000 });
    await page.waitForTimeout(1500); // let the existing history sync finish its startup burst
    await page.locator('#target-type').selectOption('group');
    await page.locator('#target-id').fill(groupID);
    await page.locator('#open-chat-btn').click();
    await page.locator('#task-panel').waitFor({ state: 'visible' });
    await page.locator('#task-panel summary').click();
    await page.locator('#task-group-name').getByText(groupID).waitFor();
}

(async () => {
    const admin = await post('/api/v1/login', null, { username: 'userA', password: '123456' });
    const groupID = `GIMP1B${Date.now()}`;
    await post('/api/v1/group/create', admin.token, { group_id: groupID, name: '浏览器验收群', members: ['1001', '1002'] });
    const browser = await playwright.chromium.launch({
        headless: true,
        ...(process.env.PLAYWRIGHT_EXECUTABLE ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE } : {}),
    });
    const context = await browser.newContext({ ignoreHTTPSErrors: process.env.LINKGO_ALLOW_LOCAL_CERT === '1' });
    const a = await context.newPage();
    const b = await context.newPage();
    const errors = [];
    for (const p of [a, b]) p.on('pageerror', (e) => errors.push(e.message));
    try {
        await loginPage(a, 'userA', groupID);
        await a.locator('#task-new-btn').click();
        await a.locator('#task-title').fill(`浏览器测试任务 ${groupID}`);
        await a.locator('#task-description').fill('提交两条学习笔记');
        let droppedResponse = false;
        const createRequestIDs = [];
        await a.route(`**/api/v1/groups/${groupID}/tasks`, async (route) => {
            if (route.request().method() !== 'POST') return route.continue();
            createRequestIDs.push(route.request().postDataJSON().request_id);
            if (!droppedResponse) {
                droppedResponse = true;
                const committed = await route.fetch();
                assert.equal(committed.status(), 201);
                return route.abort('failed');
            }
            return route.continue();
        });
        await a.locator('#task-create-btn').click();
        await a.locator('#task-status').getByText('无法连接 Gateway', { exact: false }).waitFor();
        await a.locator('#task-create-btn').click();
        assert.equal(createRequestIDs.length, 2);
        assert.equal(createRequestIDs[0], createRequestIDs[1], 'retry changed request_id after lost response');
        await a.unroute(`**/api/v1/groups/${groupID}/tasks`);
        const taskList = await fetch(`${apiBase}/api/v1/groups/${groupID}/tasks`, { headers: { Authorization: `Bearer ${admin.token}` } }).then((r) => r.json());
        assert.equal(taskList.tasks.length, 1, 'lost response created duplicate drafts');
        await a.getByRole('button', { name: '发布任务' }).waitFor();
        await a.locator('#task-detail input[placeholder="任务标题"]').fill(`浏览器测试任务 ${groupID}（已编辑）`);
        await a.getByRole('button', { name: '保存修改' }).click();
        await a.locator('#task-detail').getByText(`浏览器测试任务 ${groupID}（已编辑）`).waitFor();
        await a.getByRole('button', { name: '发布任务' }).click();
        await a.locator('#task-detail').getByText('状态：published', { exact: false }).waitFor();
        console.log('PASS browser: admin created and published');

        await loginPage(b, 'userB', groupID);
        await b.locator('#messages .bubble').filter({ hasText: `已发布：浏览器测试任务 ${groupID}` }).first().waitFor();
        await b.locator('#task-refresh-btn').click();
        await b.getByRole('button', { name: '打开任务' }).first().click();
        await b.locator('#task-submit-text').fill('第一版笔记');
        await b.getByRole('button', { name: '提交任务' }).click();
        await b.locator('#task-detail').getByText('我的提交：submitted', { exact: false }).waitFor();
        console.log('PASS browser: member submitted');

        await a.locator('#task-refresh-btn').click();
        await a.getByPlaceholder('退回理由（退回时必填）').fill('请补充例子');
        await a.getByRole('button', { name: '退回' }).click();
        await a.locator('#task-detail').getByText('returned', { exact: false }).first().waitFor();
        await b.locator('#target-type').selectOption('user');
        await b.locator('#target-id').fill('9002');
        await b.locator('#open-chat-btn').click();
        const notice = b.locator('#messages .bubble').filter({ hasText: `审核退回：浏览器测试任务 ${groupID}` }).last();
        await notice.waitFor({ timeout: 15000 });
        if (await b.locator('#task-panel').isVisible() && await b.locator('#task-panel').evaluate((element) => element.open)) {
            await b.locator('#task-panel summary').click();
        }
        await notice.click();
        await b.locator('#task-detail').getByText('退回理由：请补充例子', { exact: false }).waitFor();
        await b.locator('#task-submit-text').fill('第二版笔记，有例子');
        await b.getByRole('button', { name: '修改后重交' }).click();
        console.log('PASS browser: return reason shown and member resubmitted');

        await a.locator('#task-refresh-btn').click();
        await a.getByRole('button', { name: '通过' }).click();
        await a.locator('#task-detail').getByText('通过 1', { exact: false }).waitFor();
        await a.getByRole('button', { name: '关闭任务' }).click();
        await a.locator('#task-detail').getByText('状态：closed', { exact: false }).waitFor();
        await b.locator('#task-refresh-btn').click();
        await b.locator('#task-detail').getByText('我的提交：approved', { exact: false }).waitFor();
        await loginPage(b, 'userB', groupID);
        await b.getByRole('button', { name: '打开任务' }).first().click();
        await b.locator('#task-detail').getByText('我的提交：approved', { exact: false }).waitFor();
        assert.deepEqual(errors, []);
        console.log(`PASS browser: approved, closed, refreshed; group=${groupID}`);
    } catch (error) {
        console.error('admin status:', await a.locator('#status-text').textContent());
        console.error('admin logs:', (await a.locator('#logs').textContent()).slice(-800));
        console.error('member status:', await b.locator('#status-text').textContent());
        console.error('member task:', (await b.locator('#task-detail').textContent()).slice(-500));
        console.error('member logs:', (await b.locator('#logs').textContent()).slice(-800));
        throw error;
    } finally {
        await browser.close();
    }
})().catch((error) => { console.error(error); process.exitCode = 1; });
