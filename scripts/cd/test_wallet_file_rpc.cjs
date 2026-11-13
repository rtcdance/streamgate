// Synthetic file/process contracts; these never establish wallet admission.
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const client = () => import('../h5-demo-acceptance/lib/wallet_file_rpc.mjs');

function fixture(t, behavior = '') {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'sg-file-rpc-'));
    fs.chmodSync(directory, 0o700);
    t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
    const helper = path.join(directory, 'synthetic.cjs');
    fs.writeFileSync(helper, `const fs=require('node:fs'),path=require('node:path');
const lines=require('node:readline').createInterface({input:process.stdin});
console.log('RPC_READY');
lines.on('line',line=>{const f=JSON.parse(line),r=JSON.parse(fs.readFileSync(path.join(__dirname,f.input)));
${behavior}
const result={method:r.method,code:'OK',data:{valid:false,pid:process.pid}};
fs.writeFileSync(path.join(__dirname,f.output),JSON.stringify(result),{flag:'wx',mode:0o600});
console.log('RPC_OBSERVED method='+r.method+' code=OK');});
lines.on('close',()=>{process.stderr.write('unit private stderr');});`, { mode: 0o600 });
    return { directory, command: [process.execPath, helper] };
}

test('one process handles serialized private requests and exits at EOF', async t => {
    const { PrivateFileRPC } = await client(), f = fixture(t);
    const rpc = await new PrivateFileRPC(f.command, f.directory).start();
    t.after(() => rpc.close());
    const results = await Promise.all(Array.from({ length: 10 }, () => rpc.request('verify', { token: 'synthetic-private-token' })));
    assert.equal(new Set(results.map(item => item.value.data.pid)).size, 1);
    assert.equal(rpc.events.length, 10);
    assert.deepEqual(rpc.events.map(item => item.sequence), Array.from({ length: 10 }, (_, index) => index + 1));
    for (let index = 1; index <= 10; index++) {
        assert.equal(fs.statSync(path.join(f.directory, `${index}.response.json`)).mode & 0o777, 0o600);
    }
    const terminal = await rpc.close();
    assert.equal(terminal.code, 0); assert.equal(terminal.signal, null);
    assert.match(terminal.stderrSHA256, /^[a-f0-9]{64}$/);
    assert(!JSON.stringify(rpc.events).includes('synthetic-private-token'));
});

test('nonprivate directories are refused before spawning a client', async t => {
    const { PrivateFileRPC } = await client(), f = fixture(t);
    fs.chmodSync(f.directory, 0o755);
    const rpc = new PrivateFileRPC(f.command, f.directory);
    await assert.rejects(rpc.start(), /private request directory/);
    assert.equal(rpc.child, undefined);
});

test('early client failure retains its original exit and never restarts or exposes stderr', async t => {
    const { PrivateFileRPC } = await client(), f = fixture(t, "process.stderr.write('secret-unit-marker'); process.exit(7);");
    const rpc = await new PrivateFileRPC(f.command, f.directory).start();
    const originalPID = rpc.child.pid;
    await assert.rejects(rpc.request('verify', { token: 'synthetic-private-token' }), error => {
        assert(!error.message.includes('secret-unit-marker')); return true;
    });
    const terminal = await rpc.close();
    assert.equal(terminal.code, 7); assert.equal(rpc.child.pid, originalPID);
    assert.equal(rpc.sequence, 1); assert.equal(rpc.events.length, 0);
});
