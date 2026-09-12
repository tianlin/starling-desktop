import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Application } from '../dist/shell.js';

test('saved-session failure is visible once while temporary session remains connected', async () => {
    const data={session:{epoch:3,state:'connected',persistent:false,storageWarning:'保存失败，仅本次连接有效'}};
    globalThis.window={go:{main:{App:{Call:async()=>JSON.stringify({ok:true,data})}}}};
    const notices=[];
    const app=Object.assign(Object.create(Application.prototype),{
        reloadGeneration:0, player:{observeGeneration(){}}, applySettings(){}, drawAccount(){}, notice(s){notices.push(s);},
    });
    try {
        await app.reload(); await app.reload();
        assert.deepEqual(notices,[data.session.storageWarning]);
        assert.equal(app.boot.session.state,'connected');
        assert.equal(app.boot.session.persistent,false);
    } finally { delete globalThis.window; }
});

test('failed final persistence never acknowledges successful quit', async () => {
    const callbacks=new Map(), calls=[], errors=[];
    const oldNavigator=Object.getOwnPropertyDescriptor(globalThis,'navigator');
    Object.defineProperty(globalThis,'navigator',{value:{},configurable:true});
    globalThis.window={runtime:{EventsOn:(name,fn)=>callbacks.set(name,fn)},go:{main:{App:{Call:async action=>{calls.push(action);return JSON.stringify({ok:true});}}}}};
    const app=Object.assign(Object.create(Application.prototype),{
        boot:{session:{epoch:1}},player:{pause(){},persist:async()=>{throw Error('synthetic disk failure');}},notice:e=>errors.push(e.message),
    });
    try {
        app.nativeEvents(); callbacks.get('desktop:before-quit')();
        await new Promise(resolve=>setImmediate(resolve));
        assert.deepEqual(calls,[]); assert.deepEqual(errors,['synthetic disk failure']);
        app.player.persist=async()=>{};
        callbacks.get('desktop:before-quit')();
        await new Promise(resolve=>setImmediate(resolve));
        assert.deepEqual(calls,['progress.flush','desktop.quitReady']);
    } finally {
        delete globalThis.window;
        if(oldNavigator) Object.defineProperty(globalThis,'navigator',oldNavigator); else delete globalThis.navigator;
    }
});

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
test('successful cache clear invalidates retained updates; stale account completion cannot clear new account state',async()=>{
 let finish;globalThis.window={go:{main:{App:{Call:()=>new Promise(r=>finish=r)}}}};
 let invalidations=0;const app=Object.assign(Object.create(Application.prototype),{boot:{session:{epoch:1}},updates:{invalidate(){invalidations++;}},updatesReturn:true,route:'settings'});
 try { const clearing=app.clearCache();assert.equal(invalidations,0);finish(JSON.stringify({ok:true,data:{}}));await clearing;assert.equal(invalidations,1);assert.equal(app.updatesReturn,false);
 const stale=app.clearCache();app.boot.session.epoch=2;finish(JSON.stringify({ok:true,data:{}}));await stale;assert.equal(invalidations,1);
 }finally{delete globalThis.window;}
});
test('updates podcast cover passes the canonical official URL to detail failure fallback',()=>{
 class Node {constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.listeners={};}append(...nodes){this.children.push(...nodes);}setAttribute(){}addEventListener(k,v){this.listeners[k]=v;}}
 const previous=globalThis.document;globalThis.document={createElement:tag=>new Node(tag)};let selected;
 try { const app=Object.assign(Object.create(Application.prototype),{details:async it=>{selected=it;}});
 const card=app.itemCard({id:'episode',kind:'episode',title:'单集',podcastId:'abc123',podcastTitle:'节目',sourceUrl:'https://www.xiaoyuzhoufm.com/episode/episode'},undefined,true);
 card.children.find(n=>n.className==='cover-link').listeners.click();assert.equal(selected.sourceUrl,'https://www.xiaoyuzhoufm.com/podcast/abc123');assert.equal(selected.kind,'podcast');assert.equal(selected.id,'abc123');
 }finally{globalThis.document=previous;}
});
test('failed cache clear keeps usable retained updates',async()=>{
 globalThis.window={go:{main:{App:{Call:async()=>JSON.stringify({ok:false,error:{code:'STORAGE',message:'disk failure'}})}}}};
 let invalidations=0;const app=Object.assign(Object.create(Application.prototype),{boot:{session:{epoch:1}},updates:{invalidate(){invalidations++;}},route:'settings'});
 try {await assert.rejects(app.clearCache(),/disk failure/);assert.equal(invalidations,0);}finally{delete globalThis.window;}
});
