import { test } from 'node:test';
import assert from 'node:assert/strict';
import { CommentsController } from '../dist/comments.js';

const session = (epoch = 1, id = 'alice') => ({ epoch, state: 'connected', identity: { id, nickname: id } });
const comment = id => ({ id, author: { id: 'u', nickname: '听友' }, text: '正文\n<script>text</script>', createdAt: '2026-09-12T00:00:00Z', replyCount: 1 });
const page = (ids, cursor = '', complete = true) => ({ items: ids.map(comment), cursor, complete });

test('comments are lazy, paginated with ID deduplication, and retained on revisit', async () => {
    const calls = [];
    const c = new CommentsController(async (action, payload) => { calls.push({action, payload}); return payload.cursor ? page(['a', 'b']) : page(['a'], 'next', false); });
    c.setSession(session()); c.open('ep');
    assert.equal(calls.length, 0);
    await c.load(); await c.load();
    assert.deepEqual(c.state.items.map(x => x.id), ['a', 'b']);
    assert.equal(calls[1].payload.cursor, 'next');
    c.leave(); c.open('ep'); await c.load();
    assert.equal(calls.length, 2);
});

test('route and account changes discard late responses and clear private data', async () => {
    const pending = [];
    const c = new CommentsController(() => new Promise(resolve => pending.push(resolve)));
    c.setSession(session()); c.open('ep'); const first = c.load();
    c.leave(); c.open('other'); pending.shift()(page(['old'])); await first;
    assert.deepEqual(c.state.items, []);
    c.open('ep'); const second = c.load();
    c.setSession(session(2, 'bob')); pending.shift()(page(['alice'])); await second;
    assert.deepEqual(c.state.items, []);
    c.setSession({ epoch: 3, state: 'guest' }); await c.load();
    assert.equal(pending.length, 0);
});

test('reply pagination is independent and failure preserves loaded comments', async () => {
    let fail = false;
    const c = new CommentsController(async (action, payload) => {
        if (fail) throw new Error('暂时失败');
        if (action === 'comments.thread') return page(['reply']);
        return page(['parent'], 'next', false);
    });
    c.setSession(session()); c.open('ep'); await c.load(); await c.loadThread('parent');
    assert.deepEqual(c.state.threads.get('parent').items.map(x => x.id), ['reply']);
    assert.deepEqual(c.state.items.map(x => x.id), ['parent']);
    fail = true; await c.load();
    assert.equal(c.state.items.length, 1); assert.equal(c.state.error, '暂时失败');
});

test('reopening a route can retry while its old request is pending', async () => {
    const pending = [];
    const c = new CommentsController(() => new Promise(resolve => pending.push(resolve)));
    c.setSession(session()); c.open('ep'); const old = c.load();
    c.leave(); c.open('ep'); const current = c.load();
    pending[0](page(['stale'])); await old;
    assert.equal(c.state.loading, true);
    assert.deepEqual(c.state.items, []);
    pending[1](page(['fresh'])); await current;
    assert.equal(c.state.loading, false);
    assert.deepEqual(c.state.items.map(x => x.id), ['fresh']);
});

test('re-expanding replies retains their current page until explicit load more', async () => {
    let requests = 0;
    const c = new CommentsController(async () => { requests++; return page(['reply'], 'next', false); });
    c.setSession(session()); c.open('ep');
    await c.loadThread('parent');
    c.state.expanded.delete('parent');
    await c.loadThread('parent');
    assert.equal(requests, 1);
    await c.loadThread('parent', true);
    assert.equal(requests, 2);
});
