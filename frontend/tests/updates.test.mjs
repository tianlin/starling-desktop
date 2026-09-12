import { test } from 'node:test';
import assert from 'node:assert/strict';
import { UpdatesController, sortUpdates, dateGroup } from '../dist/updates.js';
const session = {epoch:1,state:'connected',identity:{id:'a'}};
const item = (id,published) => ({id,published,kind:'episode',title:id});
const view = (items=[],extra={}) => ({items,cursor:'next',complete:false,status:'ready',epoch:1,pages:1,revision:1,...extra});
test('first visit reads cache then refreshes; fresh revisit retains screen; stale refreshes', async()=>{
 const calls=[]; let now=1000;
 const c=new UpdatesController(async(_,p)=>{calls.push(p.mode);return view([item(p.mode)]);},()=>now);
 c.setSession(session); await c.enter(); assert.deepEqual(calls,['cached','refresh']);
 c.state.displayPage=2;c.leave();await c.enter();assert.equal(c.state.displayPage,2);assert.equal(calls.length,2);
 now+=600001;c.leave();await c.enter();assert.deepEqual(calls,['cached','refresh','refresh']);
});
test('error preserves content and successful time; retry allowed',async()=>{
 let fail=false;const c=new UpdatesController(async()=>{if(fail)throw Error('offline');return view([item('a')]);},()=>123);
 c.setSession(session);await c.enter();fail=true;await c.load('refresh');assert.equal(c.state.view.items[0].id,'a');assert.equal(c.state.lastSuccess,123);assert.equal(c.state.error,'offline');
 fail=false;await c.load('refresh');assert.equal(c.state.error,'');
});
test('leaving cancels before retry; late data never overwrites current state',async()=>{
 const calls=[];let resolveOld;let resolveCancel;
 const c=new UpdatesController(async(_,p)=>{calls.push(p.mode);if(p.mode==='cached')return view();if(p.mode==='cancel')return new Promise(r=>resolveCancel=r);if(calls.filter(x=>x==='refresh').length===1)return new Promise(r=>resolveOld=r);return view([item('new')]);});
 c.setSession(session);const first=c.enter();await new Promise(r=>setImmediate(r));c.leave();const second=c.enter();await new Promise(r=>setImmediate(r));assert.deepEqual(calls,['cached','refresh','cancel']);
 resolveCancel();await second;resolveOld(view([item('old')]));await first;assert.equal(c.state.view.items[0].id,'new');
 c.setSession({...session,epoch:2,state:'guest'});assert.equal(c.state.view,null);
});
test('cache cursor cannot be resumed after failed initial refresh',async()=>{
 const c=new UpdatesController(async(_,p)=>{if(p.mode==='cached')return view([item('cached')]);throw Error('offline');});c.setSession(session);await c.enter();assert.equal(c.state.view.cursor,'');
});
test('sort is descending, deterministic and unknown dates last; groups use local calendar',()=>{
 assert.deepEqual(sortUpdates([item('z'),item('b','2026-09-12'),item('a','2026-09-12'),item('x','bad')]).map(x=>x.id),['a','b','x','z']);
 const now=new Date(2026,8,12,12);assert.equal(dateGroup(new Date(2026,8,12,1).toISOString(),now),'今天');assert.equal(dateGroup(new Date(2026,8,11,23).toISOString(),now),'昨天');assert.equal(dateGroup(undefined,now),'日期未知');
});
import { renderUpdates } from '../dist/updates.js';
import { renderItemCard } from '../dist/item-card.js';
test('more sorts all loaded items including a newer later-page episode',async()=>{
 const c=new UpdatesController(async(_,p)=>view(p.mode==='more'?[item('old','2026-09-01'),item('new','2026-09-12')]:[item('old','2026-09-01')]));c.setSession(session);await c.enter();await c.load('more');assert.deepEqual(c.state.view.items.map(x=>x.id),['new','old']);
});
test('renderer limits display to 60 and uses keyboard buttons for podcast and shared actions',()=>{
 class Node {constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.attributes={};this.listeners={};}append(...nodes){this.children.push(...nodes);}setAttribute(k,v){this.attributes[k]=v;}addEventListener(k,v){this.listeners[k]=v;}set innerHTML(_){throw Error('unsafe HTML');}}
 const previous=globalThis.document;globalThis.document={createElement:tag=>new Node(tag)};
 try {
  const c=new UpdatesController(async()=>view());c.state.view=view(Array.from({length:61},(_,i)=>({...item(String(i),'2026-09-12'),podcastId:'p',podcastTitle:'节目',description:'<script>literal</script>'})),{status:'unknown_end'});
  const actions={details:async()=>{},podcast:async()=>{},play:async()=>{},queue:async()=>{},bookmark:async()=>{},external:async()=>{},notice:()=>{}};
  const flat=n=>[n,...n.children.flatMap(flat)];const nodes=flat(renderUpdates(c,it=>renderItemCard(it,actions,undefined,true)));
  assert.equal(nodes.filter(n=>n.tag==='article').length,60);
  assert.equal(nodes.find(n=>n.className==='cover-link').tag,'button');
  assert.equal(nodes.find(n=>n.textContent==='＋ 稍后听').tag,'button');
  assert.equal(nodes.find(n=>n.className==='episode-description').textContent,'<script>literal</script>');
  assert.equal(nodes.some(n=>n.className==='inline-warning'),false);assert.equal(nodes.some(n=>n.textContent==='暂无订阅更新'),false);
  const next=nodes.find(n=>n.textContent==='下一屏');next.listeners.click();assert.equal(c.state.displayPage,1);
 }finally{globalThis.document=previous;}
});
test('cache invalidation clears retained data and awaits old cancellation before a new cache/refresh cycle',async()=>{
 const calls=[];let releaseMore,releaseCancel;
 const c=new UpdatesController(async(_,p)=>{calls.push(p.mode);if(p.mode==='more')return new Promise(r=>releaseMore=r);if(p.mode==='cancel')return new Promise(r=>releaseCancel=r);return view([item(p.mode)]);});
 c.setSession(session);await c.enter();c.state.displayPage=1;c.state.scrollTop=300;const old=c.load('more');await new Promise(r=>setImmediate(r));
 c.invalidate();assert.equal(c.state.view,null);assert.equal(c.state.lastSuccess,0);assert.equal(c.state.displayPage,0);assert.equal(c.state.scrollTop,0);
 const next=c.enter();await new Promise(r=>setImmediate(r));assert.deepEqual(calls,['cached','refresh','more','cancel']);releaseMore(view([item('old')]));await old;assert.equal(c.state.view,null);
 releaseCancel();await next;assert.deepEqual(calls,['cached','refresh','more','cancel','cached','refresh']);assert.equal(c.state.view.items[0].id,'refresh');
});
