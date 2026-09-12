import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Application } from '../dist/shell.js';

test('late bootstrap cannot replace a newer account or redraw stale state', async () => {
    const pending = [];
    globalThis.window = { go: { main: { App: { Call: () => new Promise(resolve => pending.push(resolve)) } } } };
    const app = Object.assign(Object.create(Application.prototype), {
        boot: { session: { epoch: 1, state: 'guest' } }, reloadGeneration: 0,
        player: { observeGeneration(value) { this.generation = value; } },
        applySettings() {}, drawAccount() { this.drawn = this.boot.session.state; },
    });
    const old = app.reload();
    const fresh = app.reload();
    pending[1](JSON.stringify({ ok: true, data: { session: { epoch: 3, state: 'connected' }, playbackGeneration: 41 } }));
    await fresh;
    pending[0](JSON.stringify({ ok: true, data: { session: { epoch: 1, state: 'guest' } } }));
    await old;
    assert.equal(app.boot.session.epoch, 3);
    assert.equal(app.drawn, 'connected');
    assert.equal(app.player.generation, 41);
    delete globalThis.window;
});

test('startup restore bootstrap cannot overwrite an interactive login', async () => {
    const guest = { session: { epoch: 1, state: 'guest' }, settings: { experimentalAccount: true }, history: [] };
    const connected = { ...guest, session: { epoch: 3, state: 'connected' } };
    let releaseStartup, signalWaiting;
    const waiting = new Promise(resolve => { signalWaiting = resolve; });
    let bootstraps = 0;
    globalThis.window = { go: { main: { App: { Call: async action => {
        let data = {};
        if (action === 'bootstrap') {
            bootstraps++;
            if (bootstraps === 2) {
                signalWaiting();
                return new Promise(resolve => { releaseStartup = resolve; });
            }
            data = bootstraps === 1 ? guest : connected;
        }
        return JSON.stringify({ ok: true, data });
    } } } } };
    const app = Object.assign(Object.create(Application.prototype), {
        reloadGeneration: 0, player: { observeGeneration() {} }, wire() {}, applySettings() {},
        drawAccount() { this.drawn = this.boot.session.state; },
        nativeEvents() {}, async navigate() {}, notice(e) { throw e; },
    });
    const startup = app.start();
    await waiting;
    await app.reload();
    releaseStartup(JSON.stringify({ ok: true, data: guest }));
    await startup;
    assert.equal(app.boot.session.epoch, 3);
    assert.equal(app.drawn, 'connected');
    delete globalThis.window;
});
