const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

const source = fs.readFileSync(path.join(__dirname, '../../h5-demo/js/api.js'), 'utf8');

function infer(url, stored) {
    const storage = new Map(stored ? [['streamgate_api_base', stored]] : []);
    const context = {
        URL,
        window: { location: new URL(url) },
        localStorage: {
            getItem: key => storage.get(key) ?? null,
            setItem: (key, value) => storage.set(key, value),
        },
        console: { log() {}, warn() {} },
    };
    return vm.runInNewContext(`${source}\nAPI_BASE`, context);
}

for (const hostname of ['localhost', '127.0.0.1', '[::1]']) {
    test(`fresh ${hostname} dynamic port uses its secure-context origin`, () => {
        const origin = `http://${hostname}:37653`;
        assert.equal(infer(`${origin}/demo/`), origin);
    });
}

test('custom backend on an arbitrary localhost dev port survives reload', () => {
    assert.equal(infer('http://localhost:5173/demo/', 'https://backend.example/'), 'https://backend.example');
});

test('known acceptance port corrects the historical stale backend', () => {
    assert.equal(infer('http://localhost:29091/demo/', 'http://localhost:18000'), 'http://localhost:29091');
});

test('file preview retains its configured backend', () => {
    assert.equal(infer('file:///tmp/demo/index.html', 'https://backend.example/'), 'https://backend.example');
});

test('other hosts retain a configured backend', () => {
    assert.equal(infer('https://demo.example/', 'https://backend.example/'), 'https://backend.example');
});

test('other hosts without configuration retain the existing default', () => {
    assert.equal(infer('https://demo.example/'), 'http://localhost:18000');
});
