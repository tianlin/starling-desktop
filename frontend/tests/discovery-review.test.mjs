import {test} from 'node:test';
import assert from 'node:assert/strict';
import {Application} from '../dist/shell.js';
import {DiscoveryController} from '../dist/discovery.js';
const session = {epoch: 1, state: 'connected', identity: {id: 'a'}};
const podcast = (id, title=id) => ({id, title, kind: 'podcast', restricted: false, sourceUrl: ''});
const page = (items=[], extra={}) => ({items, users: [], subscriptions: {}, cursor: '', complete: true, ...extra});
const deferred = () => { let resolve, reject; const promise = new Promise((a,b) => {resolve=a;reject=b;}); return {promise,resolve,reject}; };
function shell(bridge) {
    const oldDocument = globalThis.document, oldWindow = globalThis.window;
    globalThis.document = {querySelector: selector => selector === '#audio' ? {addEventListener(){}} : {querySelectorAll(){return [];}}};
    globalThis.window = {go: {main: {App: {Call: async (action, raw) => JSON.stringify({ok:true, data:await bridge(action, JSON.parse(raw))})}}}};
    const app = new Application();
    app.boot = {session}; app.route = 'discovery'; app.drawDiscovery = () => {}; app.drawLibrary = () => {};
    app.opened = []; app.notices = [];
    app.showDetail = item => {app.opened.push(item.id); app.route = 'detail';};
    app.notice = error => app.notices.push(error);
    app.discovery.setSession(session);
    return {app, restore(){globalThis.document=oldDocument;globalThis.window=oldWindow;}};
}
const newerActions = {
    text: async app => { await app.discovery.submit('new text'); },
    reentry: async app => { app.discovery.leave(); app.route='home'; app.route='discovery'; await app.discovery.enter(); },
    category: async app => { await app.discovery.selectKind('episode'); },
    creator: async app => { await app.discovery.openCreator({id:'u',nickname:'U'}); },
    creator_back: async app => { app.discovery.backToSearch(); },
};
for (const [name, act] of Object.entries(newerActions)) {
    for (const error of [false,true]) test(`URL ${error?'error':'success'} is discarded after newer ${name} intent`, async () => {
        const link=deferred();
        const {app,restore}=shell(async(action,p)=>action==='openLink'?link.promise:action==='discovery.search'?page([podcast('new')]):action==='discovery.creator'?{creator:{id:p.id,nickname:'U'},...page()}: {terms:[]});
        try {
            await app.discovery.enter();
            const old = app.discovery.submit('https://example.com/old');
            await act(app);
            const retained = app.discovery.state.page;
            error ? link.reject(Error('old link failure')) : link.resolve(podcast('old'));
            await old;
            assert.equal(app.route,'discovery');
            assert.deepEqual(app.opened,[]);
            assert.deepEqual(app.notices,[]);
            assert.equal(app.discovery.state.page,retained);
        } finally {restore();}
    });
}
test('newest URL wins over earlier URL and its late failure',async()=>{
    const first=deferred(),second=deferred();
    const {app,restore}=shell(async(a,p)=>a==='openLink'?(p.text.endsWith('first')?first.promise:second.promise):{terms:[]});
    try {
        await app.discovery.enter();
        const old=app.discovery.submit('https://example.com/first');
        const latest=app.discovery.submit('https://example.com/second');
        second.resolve(podcast('second'));await latest;
        first.reject(Error('obsolete'));await old;
        assert.deepEqual(app.opened,['second']);assert.deepEqual(app.notices,[]);
    }finally{restore();}
});
test('current URL error is delivered while old text results cannot overwrite URL intent',async()=>{
    const text=deferred(),link=deferred();
    const {app,restore}=shell(async a=>a==='openLink'?link.promise:a==='discovery.search'?text.promise:{terms:[]});
    try {
        await app.discovery.enter();const old=app.discovery.submit('old text');
        const latest=app.discovery.submit('https://example.com/current');
        link.reject(Error('current failure'));await latest;
        text.resolve(page([podcast('obsolete')]));await old;
        assert.equal(app.discovery.state.page,null);
        assert.equal(app.notices.length,1);assert.equal(app.notices[0].message,'current failure');
    }finally{restore();}
});
test('empty draft retains valid submitted-query pagination',async()=>{
    const requests=[];const c=new DiscoveryController(async(a,p)=>{
        if(a!=='discovery.search')return {terms:[]};requests.push(p);
        return page([podcast(p.cursor?'second':'first')],{cursor:p.cursor?'':'next',complete:!!p.cursor});
    });
    c.setSession(session);await c.enter();await c.submit('submitted');c.state.query='';await c.more();
    assert.equal(requests.length,2);assert.equal(requests[1].query,'submitted');assert.equal(requests[1].cursor,'next');
    assert.deepEqual(c.state.page.items.map(i=>i.id),['first','second']);assert.equal(c.state.query,'');
});
test('server-incorporated subscription retires fallback and preserves later removal and metadata',async()=>{
    const responses=[{items:[podcast('p','fresh metadata')],complete:true,cursor:'',status:'complete',epoch:1},{items:[],complete:true,cursor:'',status:'complete',epoch:1}];
    const {app,restore}=shell(async()=>responses.shift());
    try {
        app.route='subscriptions';app.listKind='subscriptions';app.list={items:[]};
        app.confirmedSubscriptions.set('p',podcast('p','old receipt'));app.mergeConfirmedSubscriptions();
        assert.equal(app.list.items[0].title,'old receipt');
        await app.loadLibrary('refresh');
        assert.equal(app.list.items[0].title,'fresh metadata');assert.equal(app.confirmedSubscriptions.size,0);
        await app.loadLibrary('refresh');assert.deepEqual(app.list.items,[]);
    }finally{restore();}
});
test('subscription fallback survives refresh errors before server incorporation',async()=>{
    const {app,restore}=shell(async()=>{throw Error('offline');});
    try {
        app.route='subscriptions';app.listKind='subscriptions';app.list={items:[]};
        app.confirmedSubscriptions.set('p',podcast('p','receipt'));app.mergeConfirmedSubscriptions();
        await app.loadLibrary('refresh');
        assert.equal(app.list.items[0].title,'receipt');assert.equal(app.confirmedSubscriptions.size,1);
    }finally{restore();}
});
