import { test } from 'node:test';
import assert from 'node:assert/strict';
import { CommentsController, renderComments } from '../dist/comments.js';
import { APIError } from '../dist/api.js';

const session = (epoch = 1, id = 'alice') => ({ epoch, state: 'connected', identity: { id, nickname: id } });
const comment = id => ({ id, author: { id: 'u', nickname: '听友' }, text: '正文\n<script>text</script>', createdAt: '2026-09-12T00:00:00Z', replyCount: 1 });
const created = id => ({ ...comment(id), author: { id: 'alice', nickname: 'alice' } });
const nested = (id = 'nested') => ({ ...comment(id), primaryCommentId: 'parent', replyTo: { id: 'parent', nickname: '听友', summary: '原文' } });
const replyCreated = (id = 'new-reply', to = 'parent') => ({ ...created(id), primaryCommentId: 'parent', replyTo: { id: to, nickname: '听友', summary: '原文' } });
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

test('late reads update their own episode cache and account changes clear private data', async () => {
    const pending = [];
    const c = new CommentsController(() => new Promise(resolve => pending.push(resolve)));
    c.setSession(session()); c.open('ep'); const first = c.load();
    c.leave(); c.open('other'); pending.shift()(page(['old'])); await first;
    assert.deepEqual(c.state.items, []);
    c.open('ep'); assert.deepEqual(c.state.items.map(x => x.id), ['old']); const second = c.refresh();
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

test('reopening a route shares its pending request without issuing duplicate reads', async () => {
    const pending = [];
    const c = new CommentsController(() => new Promise(resolve => pending.push(resolve)));
    c.setSession(session()); c.open('ep'); const old = c.load();
    c.leave(); c.open('ep'); const current = c.load();
    assert.equal(pending.length, 1);
    pending[0](page(['fresh'])); await old; await current;
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

test('drafts survive navigation and confirmed create keeps separate publication feedback', async () => {
    let resolve, payload;
    const c = new CommentsController(async (_, args) => { payload = args; return new Promise(r => { resolve = r; }); });
    c.setSession(session()); c.open('ep'); c.setDraft('hello\nworld');
    const sending = c.submit(); const original = c.state;
    c.setDraft('cannot edit pending'); assert.equal(c.state.draft, 'hello\nworld');
    await c.submit();
    c.open('other'); c.setDraft('other draft');
    resolve({ comment: created('created') }); await sending;
    assert.equal(c.state.draft, 'other draft'); assert.equal(original.draft, '');
    c.open('ep'); assert.equal(c.state.confirmed.get('created').id, 'created'); assert.deepEqual(c.state.items, []);
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
    const pending = []; let fail = false;
    const c = new CommentsController(async action => {
        if (action === 'comments.create') return { comment: created('created') };
        if (fail) throw new Error('refresh failed');
        return new Promise(resolve => { pending.push(resolve); });
    });
    c.setSession(session()); c.open('ep'); const reading = c.load(); c.setDraft('new'); await c.submit();
    pending[0](page(['old'])); await reading;
    pending.at(-1)(page(['fresh'])); await new Promise(resolve => setImmediate(resolve));
    assert.equal(c.state.confirmed.has('created'), true);
    assert.deepEqual(c.state.items.map(x => x.id), ['fresh']);
    fail = true; await c.refresh(); assert.deepEqual(c.state.items.map(x => x.id), ['fresh']);
});

test('sorts cache their own cursors and scroll, share drafts, and restore per episode', async () => {
    const calls = [];
    const c = new CommentsController(async (_, args) => { calls.push(args); return page([args.order], args.order + '-next', false); });
    c.setSession(session()); c.open('ep'); await c.load(); c.setDraft('shared'); c.rememberScroll(400);
    assert.equal(c.state.order, 'hot'); await c.selectOrder('latest'); c.rememberScroll(800);
    assert.equal(c.state.draft, 'shared'); assert.deepEqual(c.state.items.map(x => x.id), ['latest']);
    await c.selectOrder('hot'); assert.equal(c.state.scroll, 400); assert.equal(c.state.cursor, 'hot-next');
    await c.selectOrder('latest'); c.open('other'); assert.equal(c.state.order, 'hot'); c.open('ep');
    assert.equal(c.state.order, 'latest'); assert.equal(c.state.scroll, 800); assert.equal(calls.length, 2);
});

test('late sort response settles only its own cache and does not redraw active order', async () => {
    const pending = [];
    const c = new CommentsController(() => new Promise(resolve => pending.push(resolve)));
    c.setSession(session()); c.open('ep'); const hot = c.load(); const latest = c.selectOrder('latest');
    pending[1](page(['latest'])); await latest;
    let draws = 0; c.onChange = () => { draws++; };
    pending[0](page(['hot'])); await hot;
    assert.deepEqual(c.state.items.map(x => x.id), ['latest']); assert.equal(draws, 0);
    await c.selectOrder('hot'); assert.deepEqual(c.state.items.map(x => x.id), ['hot']);
});

test('new root keeps server sort order and feedback disappears once fetched by ID', async () => {
    let published = false;
    const c = new CommentsController(async (action, args) => {
        if (action === 'comments.create') { published = true; return { comment: created('new') }; }
        return page(published && args.order === 'latest' ? ['new', 'older'] : ['popular']);
    });
    c.setSession(session()); c.open('ep'); await c.load(); c.setDraft('new'); await c.submit();
    await new Promise(resolve => setImmediate(resolve));
    assert.deepEqual(c.state.items.map(x => x.id), ['popular']); assert.equal(c.state.confirmed.has('new'), true);
    await c.selectOrder('latest'); assert.deepEqual(c.state.items.map(x => x.id), ['new', 'older']);
    assert.equal(c.state.confirmed.has('new'), false);
});

test('refresh failure and next-page cursors remain scoped to their sort', async () => {
    const calls = []; let fail = false;
    const c = new CommentsController(async (_, args) => {
        calls.push(args);
        if (fail) throw new Error('refresh failed');
        return page([args.order + (args.cursor ? '-page2' : '-page1')], args.cursor ? '' : args.order + '-next', !!args.cursor);
    });
    c.setSession(session()); c.open('ep'); await c.load(); await c.selectOrder('latest');
    fail = true; await c.refresh(); assert.deepEqual(c.state.items.map(x => x.id), ['latest-page1']);
    assert.equal(c.state.error, 'refresh failed');
    await c.selectOrder('hot'); assert.equal(c.state.error, '');
    fail = false; await c.load(); assert.equal(calls.at(-1).cursor, 'hot-next');
    assert.deepEqual(c.state.items.map(x => x.id), ['hot-page1', 'hot-page2']);
    await c.selectOrder('latest'); assert.equal(c.state.cursor, 'latest-next'); assert.equal(c.state.error, 'refresh failed');
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

test('reply target drafts remain separate across root, nested, cancel and sorting', async () => {
    const c = new CommentsController(async action => action === 'comments.thread' ? { ...page([]), items: [nested()] } : page(['parent']));
    c.setSession(session()); c.open('ep'); await c.load(); await c.loadThread('parent'); c.setDraft('root draft');
    c.selectReply('parent'); c.setDraft('parent draft');
    c.selectReply('nested', 'parent'); c.setDraft('nested draft'); await c.selectOrder('latest');
    assert.equal(c.state.draft, 'nested draft'); assert.equal(c.state.replyTarget.id, 'nested');
    c.cancelReply(); assert.equal(c.state.draft, 'root draft');
    c.selectReply('parent'); assert.equal(c.state.draft, 'parent draft');
    c.selectReply('nested', 'parent'); assert.equal(c.state.draft, 'nested draft');
});

test('reply send locks target, uses explicit relationship, and settles only original thread', async () => {
    let resolve, payload;
    const c = new CommentsController(async (action, args) => {
        if (action === 'comments.create') { payload = args; return new Promise(r => { resolve = r; }); }
        return action === 'comments.thread' ? { ...page([]), items: [nested()] } : page(['parent']);
    });
    c.setSession(session()); c.open('ep'); await c.load(); await c.loadThread('parent');
    c.selectReply('nested', 'parent'); c.setDraft('reply'); const owner = c.state; const sending = c.submit();
    c.cancelReply(); c.selectReply('parent'); assert.equal(c.state.replyTarget.id, 'nested');
    c.open('other'); c.setDraft('other'); resolve({ comment: replyCreated('new-reply', 'nested') }); await sending;
    assert.equal(c.state.draft, 'other'); assert.equal(owner.sending, false);
    assert.equal(owner.threads.get('parent').confirmed.has('new-reply'), true); assert.equal(owner.confirmed.size, 0);
    assert.deepEqual(owner.items.map(x => x.id), ['parent']);
    assert.equal(payload.primaryCommentId, 'parent'); assert.equal(payload.replyToCommentId, 'nested');
});

test('missing target relationship disables send and never falls back to root', async () => {
    let creates = 0;
    const c = new CommentsController(async action => { if (action === 'comments.create') creates++; return page(['parent']); });
    c.setSession(session()); c.open('ep'); await c.load(); c.selectReply('missing', 'parent'); c.setDraft('reply'); await c.submit();
    assert.equal(creates, 0); assert.equal(c.state.replyTarget.id, 'missing'); assert.equal(c.state.replyTarget.valid, false);
    assert.match(c.state.sendError, /刷新/); assert.equal(c.state.draft, 'reply');
});

test('target-invalid rejection preserves target draft and requires renewed evidence', async () => {
    const c = new CommentsController(async action => { if (action === 'comments.create') throw new APIError({ code: 'COMMENT_TARGET_INVALID', message: 'target gone' }); return page(['parent']); });
    c.setSession(session()); c.open('ep'); await c.load(); c.selectReply('parent'); c.setDraft('reply'); await c.submit();
    assert.equal(c.state.uncertain, false); assert.equal(c.state.replyTarget.valid, false); assert.equal(c.state.draft, 'reply');
    c.cancelReply(); assert.equal(c.state.draft, ''); c.selectReply('parent'); assert.equal(c.state.draft, 'reply');
    await c.refreshTarget(); assert.equal(c.state.replyTarget.valid, true);
});

test('reply uncertainty refreshes its thread, stays target scoped and deduplicates confirmation', async () => {
    const calls = []; let fail = true;
    const c = new CommentsController(async (action, args) => {
        calls.push({ action, args });
        if (action === 'comments.create') { if (fail) throw new Error('lost'); return { comment: replyCreated() }; }
        if (action === 'comments.thread') return { ...page([]), items: fail ? [] : [replyCreated()] };
        return page(['parent']);
    });
    c.setSession(session()); c.open('ep'); await c.load(); c.selectReply('parent'); c.setDraft('reply'); await c.submit();
    assert.equal(c.state.uncertain, true); await c.refreshTarget(); assert.equal(calls.at(-1).action, 'comments.thread');
    c.cancelReply(); assert.equal(c.state.uncertain, false); c.selectReply('parent'); assert.equal(c.state.uncertain, true);
    fail = false; await c.submit(true); await new Promise(resolve => setImmediate(resolve));
    assert.equal(c.state.threads.get('parent').confirmed.size, 0);
    assert.deepEqual(c.state.threads.get('parent').items.map(x => x.id), ['new-reply']); assert.equal(c.state.confirmed.size, 0);
});

test('reply create with mismatched relation or after account change cannot enter a list', async () => {
    let resolve;
    const c = new CommentsController(async action => action === 'comments.create' ? new Promise(r => { resolve = r; }) : page(['parent']));
    c.setSession(session()); c.open('ep'); await c.load(); c.selectReply('parent'); c.setDraft('reply');
    const wrong = c.submit(); resolve({ comment: replyCreated('new', 'other') }); await wrong;
    assert.equal(c.state.uncertain, true); assert.equal(c.state.draft, 'reply');
    const late = c.submit(true); c.setSession(session(2, 'bob')); resolve({ comment: replyCreated() }); await late;
    assert.equal(c.state.draft, ''); assert.equal(c.state.threads.size, 0);
});
