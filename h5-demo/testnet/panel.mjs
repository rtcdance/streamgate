import { Fixture, OPERATIONS, loadBuild, validateConfig } from './fixture.mjs';
const $ = id => document.getElementById(id);
let fixture;
let busy = false;
function render() {
    if (!fixture) return;
    $('ledger').textContent = JSON.stringify(fixture.ledger, null, 2);
    $('recipient').value = fixture.ledger.recipient || fixture.config.recipient;
    $('recipient').disabled = true;
    const selected = $('operation').value;
    $('operation').replaceChildren(...fixture.ledger.operations.map(op => {
        const option = document.createElement('option'); option.value = op.id; option.textContent = op.id + ' · ' + op.status; return option;
    }));
    if (selected) $('operation').value = selected;
    for (const button of document.querySelectorAll('button')) button.disabled = busy;
    for (const button of document.querySelectorAll('[data-op]')) {
        const index = OPERATIONS.indexOf(button.dataset.op);
        button.disabled = busy || location.origin !== fixture.config.transactionOrigin || fixture.ledger.operations.some(op => (op.id === button.dataset.op && op.status !== 'rejected') || !['confirmed', 'rejected'].includes(op.status)) ||
            OPERATIONS.slice(0, index).some(id => !fixture.confirmed(id));
    }
}
async function perform(action, write = false, sending = false) {
    if (busy) return;
    busy = true; render(); $('status').textContent = sending ? '正在准备交易，请在 MetaMask 核对并决定是否批准…' : '正在只读核对…';
    try {
        // Web Locks prevents two same-origin panels from sending the same intent.
        // Keep the lock across the wallet approval dialog and persistent result.
        if (write) {
            if (!navigator.locks) throw Error('浏览器不支持交易互斥锁，禁止发送');
            await navigator.locks.request(fixture.key, { ifAvailable: true }, async lock => {
                if (!lock) throw Error('另一个页面正在操作本轮钱包，请等待');
                await action();
            });
        } else await action();
        $('status').textContent = sending ? '已保存交易结果。请只读对账；继续下一步前完成两个模式的业务观察。' : '只读核对完成。receipt 不是完整验收结论。';
    } catch (error) { $('status').textContent = error.message; }
    finally { busy = false; render(); }
}
try {
    if (!['localhost', '127.0.0.1'].includes(location.hostname) || location.protocol !== 'http:') throw Error('仅允许本机测试网入口');
    const result = await fetch('/testnet.config.json', { cache: 'no-store' });
    if (!result.ok) throw Error('缺少冻结候选配置；请先完成 CI 产物准备');
    const config = validateConfig(await result.json());
    const artifacts = await loadBuild(config);
    fixture = new Fixture(config, artifacts, window.ethereum, localStorage, window.ethers);
    $('candidate').textContent = '候选 ' + config.candidateSHA + ' · Ethereum Sepolia 11155111 · 交易主入口 ' + config.transactionOrigin + '/testnet/';
    $('account').textContent = '指定操作地址 A：' + config.controller;
    $('connect').onclick = () => perform(() => fixture.account(true));
    for (const button of document.querySelectorAll('[data-op]')) button.onclick = () => perform(() => fixture.submit(button.dataset.op, $('recipient').value.trim()), true, true);
    $('reconcile').onclick = () => perform(() => fixture.reconcile($('operation').value, $('hash').value.trim()), true);
    $('export').onclick = () => {
        const blob = new Blob([JSON.stringify(fixture.ledger, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url;
        link.download = 'streamgate-sepolia-' + config.candidateSHA.slice(0, 8) + '.json'; link.click(); URL.revokeObjectURL(url);
    };
    window.addEventListener('storage', event => { if (event.key === fixture.key) { try { fixture.reload(); render(); } catch (error) { $('status').textContent = error.message; } } });
    $('status').textContent = '候选归档已核对。连接 MetaMask，填写 B，再按顺序操作。'; render();
} catch (error) { $('status').textContent = 'BLOCKED：' + error.message; }
