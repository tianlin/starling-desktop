import { test } from 'node:test';
import assert from 'node:assert/strict';
import { CommentsController, renderComments } from '../dist/comments.js';
import { APIError } from '../dist/api.js';

const session = (epoch = 1, id = 'alice') => ({ epoch, state: 'connected', identity: { id, nickname: id } });
const comment = id => ({ id, author: { id: 'u', nickname: '听友' }, text: '正文\n<script>text</script>', createdAt: '2026-09-12T00:00:00Z', replyCount: 1 });
const created = id => ({ ...comment(id), author: { id: 'alice', nickname: 'alice' } });
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

test('drafts survive navigation and only confirmed create clears and prepends', async () => {
    let resolve, payload;
    const c = new CommentsController(async (_, args) => { payload = args; return new Promise(r => { resolve = r; }); });
    c.setSession(session()); c.open('ep'); c.setDraft('hello\nworld');
    const sending = c.submit(); const original = c.state;
    c.setDraft('cannot edit pending'); assert.equal(c.state.draft, 'hello\nworld');
    await c.submit();
    c.open('other'); c.setDraft('other draft');
    resolve({ comment: created('created') }); await sending;
    assert.equal(c.state.draft, 'other draft'); assert.equal(original.draft, '');
    c.open('ep'); assert.equal(c.state.items[0].id, 'created');
    assert.equal(c.state.sending, false); assert.ok(payload.requestId);
    c.setDraft('private'); c.setSession(session(2, 'bob')); assert.equal(c.state.draft, '');
});

test('uncertain send preserves draft and explicit retry uses a new request ID', async () => {
    const ids = [];
    const c = new CommentsController(async (_, args) => { ids.push(args.requestId); throw new Error('network'); });
    c.setSession(session()); c.open('ep'); c.setDraft('  '); await c.submit(); assert.equal(ids.length, 0);
    c.setDraft('draft'); await c.submit();
    assert.equal(c.state.uncertain, true); assert.equal(c.state.draft, 'draft');
    await c.submit(); assert.equal(ids.length, 1);
    await c.submit(true); assert.equal(ids.length, 2); assert.notEqual(ids[0], ids[1]);
});

test('malformed and duplicate result are uncertain; authorization failure retains draft', async () => {
    for (const result of [{}, new APIError({ code: 'COMMENT_DUPLICATE', message: 'duplicate' }), new APIError({ code: 'UNAUTHORIZED', message: 'login' })]) {
        const c = new CommentsController(async () => { if (result instanceof Error) throw result; return result; });
        c.setSession(session()); c.open('ep'); c.setDraft('draft'); await c.submit();
        assert.equal(c.state.draft, 'draft'); assert.equal(c.state.sending, false);
        assert.equal(c.state.uncertain, !(result instanceof APIError && result.code === 'UNAUTHORIZED'));
    }
});

test('successful create survives an older read and refresh preserves content on failure', async () => {
    let resolveRead, fail = false;
    const c = new CommentsController(async action => {
        if (action === 'comments.create') return { comment: created('created') };
        if (fail) throw new Error('refresh failed');
        return new Promise(resolve => { resolveRead = resolve; });
    });
    c.setSession(session()); c.open('ep'); const reading = c.load(); c.setDraft('new'); await c.submit();
    resolveRead(page(['old'])); await reading;
    assert.deepEqual(c.state.items.map(x => x.id), ['created', 'old']);
    fail = true; await c.refresh(); assert.deepEqual(c.state.items.map(x => x.id), ['created', 'old']);
});

test('refresh invalidates old read, resets pagination and guards repeated cursors', async () => {
    const pending = [];
    const c = new CommentsController(() => new Promise(resolve => pending.push(resolve)));
    c.setSession(session()); c.open('ep'); const old = c.load(); const refresh = c.refresh();
    pending[1](page(['fresh'], 'cursor', false)); await refresh;
    pending[0](page(['stale'], 'stale', false)); await old;
    assert.deepEqual(c.state.items.map(x => x.id), ['fresh']);
    const more = c.load(); pending[2](page(['more'], 'cursor', false)); await more;
    assert.equal(c.state.cursor, ''); assert.equal(c.state.complete, false);
    assert.deepEqual(c.state.items.map(x => x.id), ['fresh']);
    assert.match(c.state.error, /分页/);
});

test('create response for a different author is uncertain and keeps draft', async () => {
    const c = new CommentsController(async () => ({ comment: comment('wrong-author') }));
    c.setSession(session()); c.open('ep'); c.setDraft('draft'); await c.submit();
    assert.equal(c.state.uncertain, true); assert.equal(c.state.draft, 'draft'); assert.deepEqual(c.state.items, []);
});

test('send response after logout cannot restore private content', async () => {
    let resolve;
    const c = new CommentsController(() => new Promise(r => { resolve = r; }));
    c.setSession(session()); c.open('ep'); c.setDraft('secret'); const sending = c.submit();
    c.setSession({ epoch: 2, state: 'guest' }); resolve({ comment: comment('private') }); await sending;
    assert.equal(c.state.draft, ''); assert.deepEqual(c.state.items, []);
});

test('same-account needs-login state keeps draft while disabling requests', async () => {
    let requests = 0;
    const c = new CommentsController(async () => { requests++; return page([]); });
    c.setSession(session()); c.open('ep'); c.setDraft('draft');
    c.setSession({ ...session(), state: 'needs_login' });
    assert.equal(c.state.draft, 'draft'); assert.equal(c.connected, false);
    await c.submit(); await c.load(); assert.equal(requests, 0);
});

test('explicit platform rejection preserves draft without uncertain resend flow', async () => {
    for (const code of ['REQUEST_REJECTED', 'EXPIRED']) {
        const c = new CommentsController(async () => { throw new APIError({ code, message: 'rejected' }); });
        c.setSession(session()); c.open('ep'); c.setDraft('draft'); await c.submit();
        assert.equal(c.state.uncertain, false); assert.equal(c.state.draft, 'draft');
    }
});

test('composer and comment rendering use literal text and preserve newlines', async () => {
    class Node {
        constructor(tag) { this.tag = tag; this.children = []; this.dataset = {}; this.attributes = {}; }
        append(...children) { this.children.push(...children); }
        setAttribute(key, value) { this.attributes[key] = value; }
        addEventListener() {}
        set innerHTML(_) { throw new Error('User content must never be parsed as HTML'); }
    }
    const previous = globalThis.document;
    globalThis.document = { createElement: tag => new Node(tag) };
    try {
        const c = new CommentsController(async () => page(['literal']));
        c.setSession(session()); c.open('ep'); await c.load(); c.setDraft('<img src=x>\nnext');
        const root = renderComments(c, () => {});
        const flat = node => [node, ...node.children.flatMap(flat)];
        const nodes = flat(root);
        assert.equal(nodes.find(node => node.className === 'comment-text').textContent, comment('literal').text);
        assert.equal(nodes.find(node => node.tag === 'textarea').value, '<img src=x>\nnext');
        assert.equal(nodes.some(node => node.tag === 'script' || node.tag === 'img'), false);
        assert.equal(nodes.find(node => node.textContent === '发表评论').disabled, false);
    } finally { globalThis.document = previous; }
});

test('acknowledging a published uncertain comment only clears draft without another request', async () => {
    let requests = 0;
    const c = new CommentsController(async () => { requests++; throw new Error('lost response'); });
    c.setSession(session()); c.open('ep'); c.setDraft('draft'); await c.submit();
    c.acknowledgePublished();
    assert.equal(requests, 1); assert.equal(c.state.draft, ''); assert.equal(c.state.uncertain, false);
    assert.equal(c.state.sendError, ''); assert.deepEqual(c.state.items, []);
});
