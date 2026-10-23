// Regression: a /health 5xx that the page's own retry recovered from must not
// fail a spec. The demo's healthCheck retries 502/503 twice; a single cold-start
// 502 followed by a 200 is a blip, while a URL whose LAST status is still 5xx is
// a real mid-run outage and stays a hard error.
const assert = require('node:assert/strict');
const { EventEmitter } = require('node:events');
const { test } = require('node:test');
const path = require('node:path');
const { pathToFileURL } = require('node:url');

const MODULE = pathToFileURL(
    path.join(__dirname, '../h5-demo-acceptance/lib/common.mjs'),
).href;

function fakePage() {
    const page = new EventEmitter();
    page.url = () => 'http://localhost:36791/';
    return page;
}

const RESOURCE_502 = {
    type: () => 'error',
    text: () => 'Failed to load resource: the server responded with a status of 502 (Bad Gateway)',
    location: () => ({ url: 'http://localhost:36791/health' }),
};
const API_502 = {
    type: () => 'error',
    text: () => 'API Error [/health]: Error: HTTP 502\n    at APIService.request (http://localhost:36791/demo/js/api.js?v=23:93:29)',
    location: () => ({ url: 'http://localhost:36791/demo/js/api.js?v=23' }),
};
const healthResponse = (status) => ({
    status: () => status,
    url: () => 'http://localhost:36791/health',
});

async function collect(events) {
    const { attachProbes } = await import(MODULE);
    const page = fakePage();
    const probes = attachProbes(page, '/tmp/sg-probe-test');
    for (const [name, arg] of events) page.emit(name, arg);
    return probes.errors;
}

test('a 5xx recovered by the page retry is transient, and stays visible', async () => {
    const errors = await collect([
        ['response', healthResponse(502)],
        ['console', RESOURCE_502],
        ['console', API_502],
        ['response', healthResponse(200)],
    ]);
    const consoleErrors = errors.filter((e) => e.kind === 'console');
    assert.equal(consoleErrors.length, 2);
    for (const e of consoleErrors) {
        assert.equal(e.transient, true, 'recovered 5xx must be transient');
        assert.match(e.note || '', /answered 200/);
    }
});

test('a 5xx whose last status is still 5xx stays a hard error', async () => {
    const errors = await collect([
        ['response', healthResponse(502)],
        ['console', RESOURCE_502],
        ['console', API_502],
    ]);
    const consoleErrors = errors.filter((e) => e.kind === 'console');
    assert.equal(consoleErrors.length, 2);
    for (const e of consoleErrors) {
        assert.notEqual(e.transient, true, 'unrecovered 5xx must stay hard');
    }
});

test('page errors are never reclassified by a later health success', async () => {
    const pageError = new Error('boom');
    const errors = await collect([
        ['pageerror', pageError],
        ['response', healthResponse(200)],
    ]);
    assert.equal(errors.length, 1);
    assert.equal(errors[0].kind, 'pageerror');
    assert.notEqual(errors[0].transient, true);
});
