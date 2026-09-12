import {test} from 'node:test';
import assert from 'node:assert/strict';
import {Player} from '../dist/player.js';
class AudioMock extends EventTarget {
  paused=true;currentTime=0;duration=120;volume=1;playbackRate=1;src='';readyState=4;seeking=false;
  seekable={length:1,start:()=>0,end:()=>120};error=null;
  load(){this.dispatchEvent(new Event('loadedmetadata'));}
  async play(){this.paused=false;this.dispatchEvent(new Event('playing'));}
  pause(){this.paused=true;this.dispatchEvent(new Event('pause'));}
  removeAttribute(name){if(name==='src')this.src='';}
}
const item=id=>({kind:'episode',id,title:id,sourceUrl:'https://www.xiaoyuzhoufm.com/episode/'+id,restricted:false});
test('normal loading and restoring a checkpoint never flash a seek warning before media is ready',async()=>{
 for(const position of [0,42]) {
  const audio=new AudioMock(),messages=[];
  audio.readyState=0;audio.seekable={length:0,start:()=>0,end:()=>120};
  audio.load=()=>{};
  let finishPlay;
  audio.play=()=>new Promise(resolve=>{finishPlay=resolve;});
  const p=new Player(audio,async action=>action==='playback.resolve'?{item:item('a'),url:'https://media.xyzcdn.net/a.wav',position,epoch:1}:action==='progress.prepare'?{position,sync:{state:'idle',pending:0}}:null,()=>1,()=>messages.push(p.error||p.notice));
  const pending=p.play(item('a'));await new Promise(setImmediate);
  audio.readyState=1;audio.dispatchEvent(new Event('loadedmetadata'));
  audio.dispatchEvent(new Event('progress'));
  assert.equal(p.state,'buffering');assert.equal(p.seekable,false);
  assert.ok(messages.every(message=>message===''),'ordinary initial loading must not display a warning');
  audio.seekable={length:1,start:()=>0,end:()=>120};audio.readyState=3;audio.dispatchEvent(new Event('canplay'));
  audio.paused=false;audio.dispatchEvent(new Event('playing'));finishPlay();await pending;
  assert.equal(audio.currentTime,position);assert.equal(p.notice,'');
  assert.ok(messages.every(message=>message===''));
  await p.dispose();
 }
});
function fixture(resolver,{save=()=>null,epoch=()=>1}={}){
 const audio=new AudioMock(),calls=[],resolved=new Map();
 const api=async(action,p)=>{calls.push({action,p}); if(action==='playback.resolve'){const result=await resolver(p);resolved.set(p.id,result);return result;}if(action==='progress.prepare')return {position:resolved.get(p.eid)?.position??0,sync:{state:'idle',pending:0}};if(action==='progress.save')return save(p);return null;};
 const p=new Player(audio,api,epoch,()=>{});return {p,audio,calls};
}
test('latest selection wins while earlier resolution arrives late',async()=>{
 let first;const {p,audio}=fixture(({id,requestId})=>id==='a'?new Promise(r=>first=r):Promise.resolve({item:item(id),url:'https://media.xyzcdn.net/b.wav',position:0,epoch:1}));
 const a=p.play(item('a'));await new Promise(setImmediate);await p.play(item('b'));first({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1});await a;
 assert.equal(p.item.id,'b');assert.equal(audio.src,'https://media.xyzcdn.net/b.wav');await p.dispose();
});
test('pause during resolution prevents late autoplay',async()=>{
 let finish;const {p,audio}=fixture(()=>new Promise(r=>finish=r));const pending=p.play(item('a'));await new Promise(setImmediate);p.pause();finish({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1});await pending;
 assert.equal(audio.paused,true);assert.equal(audio.src,'');await p.dispose();
});
test('seek is clamped and paused state is persisted using captured epoch',async()=>{
 const {p,audio,calls}=fixture(()=>Promise.resolve({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:42,epoch:1}));
 await p.play(item('a'));assert.equal(audio.currentTime,42);p.seek(900);assert.equal(audio.currentTime,120);p.pause();await p.flush();
 const saved=calls.filter(x=>x.action==='progress.save').at(-1);assert.equal(saved.p.epoch,1);assert.equal(saved.p.progress.position,120);await p.dispose();
});
test('restoring a previous item does not resolve or autoplay',async()=>{
 const {p,audio,calls}=fixture(()=>{throw Error('should not resolve')});p.restore({item:item('a'),position:42,duration:120,ended:false,updatedAt:''});
 assert.equal(p.item.id,'a');assert.equal(p.position,42);assert.equal(audio.src,'');assert.equal(calls.length,0);await p.dispose();
});
test('ended is the only event that marks a track complete',async()=>{
 const {p,audio,calls}=fixture(()=>Promise.resolve({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}));
 await p.play(item('a'));audio.currentTime=120;p.pause();await p.flush();assert.equal(calls.filter(x=>x.action==='progress.save').at(-1).p.progress.ended,false);
 audio.dispatchEvent(new Event('ended'));await p.flush();assert.equal(calls.filter(x=>x.action==='progress.save').at(-1).p.progress.ended,true);await p.dispose();
});
test('a late media error cannot restart playback after a user pause',async()=>{
 const {p,audio,calls}=fixture(()=>Promise.resolve({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}));
 await p.play(item('a'));p.pause();audio.error={code:2};audio.dispatchEvent(new Event('error'));await new Promise(setImmediate);
 assert.equal(calls.filter(x=>x.action==='playback.resolve').length,1);assert.equal(audio.paused,true);await p.dispose();
});
test('initial resume waits until media becomes seekable',async()=>{
 const {p,audio}=fixture(()=>Promise.resolve({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:42,epoch:1}));
 audio.seekable={length:0,start:()=>0,end:()=>120};await p.play(item('a'));assert.equal(audio.currentTime,0);
 audio.seekable={length:1,start:()=>0,end:()=>120};audio.dispatchEvent(new Event('canplay'));assert.equal(audio.currentTime,42);await p.dispose();
});
test('partial buffering retains the resume target until that position becomes seekable',async()=>{
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:42,epoch:1}));
 audio.seekable={length:1,start:()=>0,end:()=>10};await p.play(item('a'));
 audio.seekable={length:1,start:()=>0,end:()=>120};audio.dispatchEvent(new Event('progress'));
 assert.equal(audio.currentTime,42);await p.dispose();
});
test('early playback events cannot overwrite a resume checkpoint that is still unreachable',async()=>{
 let checkpoint=42;
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:checkpoint,epoch:1}),{save:({progress})=>{checkpoint=progress.position;}});
 audio.seekable={length:1,start:()=>0,end:()=>10};await p.play(item('a'));
 audio.currentTime=2;audio.dispatchEvent(new Event('timeupdate'));p.pause();await p.persist();
 assert.equal(checkpoint,42);assert.equal(p.position,2);
 audio.seekable={length:1,start:()=>0,end:()=>120};audio.dispatchEvent(new Event('progress'));
 assert.equal(audio.currentTime,42);audio.currentTime=43;await p.persist();assert.equal(checkpoint,43);await p.dispose();
});
test('a temporarily rejected currentTime assignment does not discard the resume target',async()=>{
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:42,epoch:1}));
 let current=0,reject=true;
 Object.defineProperty(audio,'currentTime',{get:()=>current,set:value=>{if(reject)throw new DOMException('not ready','InvalidStateError');current=value;}});
 await p.play(item('a'));reject=false;audio.dispatchEvent(new Event('canplay'));
 assert.equal(current,42);await p.dispose();
});
test('an explicit successful seek replaces a pending automatic resume',async()=>{
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:42,epoch:1}));
 audio.seekable={length:0,start:()=>0,end:()=>120};await p.play(item('a'));
 audio.seekable={length:1,start:()=>0,end:()=>120};p.seek(5);audio.dispatchEvent(new Event('progress'));
 assert.equal(audio.currentTime,5);await p.dispose();
});
test('a successful checkpoint clears the preceding save failure',async()=>{
 let fail=true;
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}),{save:()=>{if(fail)throw Error('disk locked');}});
 await p.play(item('a'));audio.currentTime=1;await p.persist();assert.match(p.error,/disk locked/);
 fail=false;await p.persist();assert.equal(p.error,'');await p.dispose();
});
test('a successful checkpoint cannot clear an unresolved media failure',async()=>{
 let fail=true;
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}),{save:()=>{if(fail)throw Error('disk locked');}});
 await p.play(item('a'));audio.currentTime=1;await p.persist();audio.error={code:3};audio.dispatchEvent(new Event('error'));
 const mediaFailure=p.error;assert.match(mediaFailure,/音频无法播放/);
 fail=false;await p.persist();assert.equal(p.error,mediaFailure);await p.dispose();
});
test('playing does not hide a checkpoint failure before saving actually recovers',async()=>{
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}),{save:()=>{throw Error('disk locked');}});
 await p.play(item('a'));audio.currentTime=1;await p.persist();audio.dispatchEvent(new Event('playing'));
 assert.match(p.error,/disk locked/);await p.dispose();
});
test('a late checkpoint failure from an old selection cannot change the new selection error',async()=>{
 let rejectSave,resolveNext;
 const {p,audio}=fixture(({id})=>id==='b'?new Promise(resolve=>resolveNext=resolve):({item:item(id),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}),{save:()=>new Promise((_,reject)=>rejectSave=reject)});
 await p.play(item('a'));audio.currentTime=1;const saving=p.persist();await new Promise(setImmediate);
 p.clear();const playing=p.play(item('b'));rejectSave(Error('old disk failure'));await saving;await new Promise(setImmediate);
 assert.equal(p.item.id,'b');assert.equal(p.state,'resolving');assert.equal(p.error,'');
 resolveNext({item:item('b'),url:'https://media.xyzcdn.net/b.wav',position:0,epoch:1});await playing;p.clear();await p.dispose();
});
test('a checkpoint failure cannot change the player after the account epoch changes',async()=>{
 let currentEpoch=1,rejectSave;
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}),{epoch:()=>currentEpoch,save:()=>new Promise((_,reject)=>rejectSave=reject)});
 await p.play(item('a'));audio.currentTime=1;const saving=p.persist();await new Promise(setImmediate);currentEpoch=2;
 rejectSave(Error('old account failure'));await saving;assert.equal(p.error,'');p.clear();await p.dispose();
});
for(const transition of ['selection','clear','account'])test(`a rejected old resume cannot change player state after ${transition}`,async()=>{
 let currentEpoch=1,rejectPlay;
 const {p,audio}=fixture(({id})=>({item:item(id),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:currentEpoch}),{epoch:()=>currentEpoch});
 await p.play(item('a'));p.pause();const normalPlay=audio.play.bind(audio);
 audio.play=()=>new Promise((_,reject)=>rejectPlay=reject);const resuming=p.toggle();await new Promise(setImmediate);audio.play=normalPlay;
 if(transition==='selection')await p.play(item('b'));
 if(transition==='clear')p.clear();
 if(transition==='account')currentEpoch=2;
 const expected={state:p.state,error:p.error};rejectPlay(Error('late play rejection'));await resuming;
 assert.deepEqual({state:p.state,error:p.error},expected);p.clear();await p.dispose();
});
test('a zero-width seekable range disables seeking and explains the limitation',async()=>{
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}));
 audio.seekable={length:1,start:()=>0,end:()=>0};await p.play(item('a'));
 assert.equal(p.seekable,false);assert.equal(p.seek(5),false);assert.match(p.notice,/定位/);
 audio.seekable={length:1,start:()=>0,end:()=>120};audio.dispatchEvent(new Event('progress'));
 assert.equal(p.seekable,true);assert.equal(p.notice,'');await p.dispose();
});
test('unseekable playback displays the actual time while retaining the previous checkpoint',async()=>{
 let checkpoint=42;
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:checkpoint,epoch:1}),{save:({progress})=>{checkpoint=progress.position;}});
 audio.seekable={length:1,start:()=>0,end:()=>0};await p.play(item('a'));audio.currentTime=2;audio.dispatchEvent(new Event('timeupdate'));await p.persist();
 assert.equal(p.position,2);assert.equal(checkpoint,42);assert.match(p.notice,/原进度.*保留/);await p.dispose();
});
test('natural playback reaching the retained checkpoint resumes progress saving',async()=>{
 let checkpoint=42;
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:checkpoint,epoch:1}),{save:({progress})=>{checkpoint=progress.position;}});
 audio.seekable={length:1,start:()=>0,end:()=>0};await p.play(item('a'));audio.currentTime=43;audio.dispatchEvent(new Event('timeupdate'));await p.persist();
 assert.equal(checkpoint,43);assert.equal(p.position,43);assert.doesNotMatch(p.notice,/原进度/);
 audio.currentTime=120;audio.dispatchEvent(new Event('ended'));await p.flush();assert.equal(checkpoint,120);await p.dispose();
});
test('playback requests carry increasing generations seeded from the backend',async()=>{
 const {p,calls}=fixture(({id})=>({item:item(id),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}));
 p.observeGeneration(41);await p.play(item('a'));
 const first=calls.find(c=>c.action==='playback.resolve').p;assert.equal(first.generation,42);
 p.pause();const cancelled=calls.find(c=>c.action==='playback.cancel').p;
 assert.equal(cancelled.generation,first.generation);assert.equal(cancelled.requestId,first.requestId);
 p.observeGeneration(1);await p.play(item('b'));
 const latest=calls.filter(c=>c.action==='playback.resolve').at(-1).p;
 assert.ok(latest.generation>first.generation);assert.notEqual(latest.requestId,first.requestId);await p.dispose();
});
test('pausing after a media failure preserves manual retry through a new resolution',async()=>{
 const {p,audio,calls}=fixture(({id})=>({item:item(id),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}));
 const normalPlay=audio.play.bind(audio);audio.play=async()=>{throw Error('expired media');};
 assert.equal(await p.play(item('a')),false);assert.equal(p.state,'error');p.pause();
 assert.equal(p.state,'error');audio.play=normalPlay;await p.toggle();
 assert.equal(calls.filter(c=>c.action==='playback.resolve').length,2);assert.equal(p.state,'playing');await p.dispose();
});

test('prepare completes before playback and automatic restoration does not save',async()=>{
 const audio=new AudioMock(),calls=[];let finish;
 const p=new Player(audio,async(a,v)=>{calls.push({a,v});if(a==='playback.resolve')return {item:item('a'),url:'a',position:4};if(a==='progress.prepare')return new Promise(r=>finish=r);},()=>1,()=>{});
 const playing=p.play(item('a'));await new Promise(setImmediate);assert.equal(audio.paused,true);
 finish({position:40,sync:{state:'synced',pending:0}});await playing;assert.equal(audio.currentTime,40);
 audio.dispatchEvent(new Event('timeupdate'));p.pause();await p.persist();assert.equal(calls.filter(c=>c.a==='progress.save').length,0);
 p.seek(50);await p.flush();assert.equal(calls.find(c=>c.a==='progress.save').v.urgent,true);p.clear();await p.dispose();
});
test('a late prepare cannot hijack the newer selection',async()=>{
 const audio=new AudioMock();let finish;
 const p=new Player(audio,async(a,v)=>{if(a==='playback.resolve')return {item:item(v.id),url:v.id,position:0};if(a==='progress.prepare')return v.eid==='a'?new Promise(r=>finish=r):{position:20,sync:{state:'idle',pending:0}};},()=>1,()=>{});
 const first=p.play(item('a'));await new Promise(setImmediate);await p.play(item('b'));finish({position:80,sync:{state:'synced',pending:0}});await first;assert.equal(audio.src,'b');assert.equal(audio.currentTime,20);p.clear();await p.dispose();
});
test('conflict waits for an explicit local or cloud choice',async()=>{
 const audio=new AudioMock(),calls=[];
 const p=new Player(audio,async(a,v)=>{calls.push({a,v});if(a==='playback.resolve')return {item:item('a'),url:'a',position:20};if(a==='progress.prepare')return {position:20,sync:{state:'conflict',pending:1},conflict:{localPosition:20,cloudPosition:80,token:'decision'}};if(a==='progress.choose')return {position:80,sync:{state:'synced',pending:0}};},()=>1,()=>{});
 const playing=p.play(item('a'));await new Promise(setImmediate);assert.equal(audio.paused,true);assert.equal(p.conflict.cloudPosition,80);await p.chooseProgress('cloud');await playing;assert.equal(audio.currentTime,80);assert.equal(calls.find(c=>c.a==='progress.choose').v.choice,'cloud');p.clear();await p.dispose();
});
for (const target of [20,0]) test(`loaded paused resume accepts cloud rewind to ${target}`,async()=>{
 const audio=new AudioMock();let prepares=0;
 const p=new Player(audio,async(a)=>a==='playback.resolve'?{item:item('a'),url:'a',position:80}:a==='progress.prepare'?{position:prepares++?target:80,sync:{state:'synced',pending:0}}:null,()=>1,()=>{});
 await p.play(item('a'));p.pause();audio.seekable={length:0,start:()=>0,end:()=>120};await p.toggle();
 audio.dispatchEvent(new Event('timeupdate'));audio.seekable={length:1,start:()=>0,end:()=>120};audio.dispatchEvent(new Event('progress'));
 assert.equal(audio.currentTime,target);assert.equal(prepares,2);p.clear();await p.dispose();
});
test('explicit start and paused user seek retain the requested position',async()=>{
 const audio=new AudioMock(),calls=[];
 const p=new Player(audio,async(a)=>{calls.push(a);if(a==='playback.resolve')return {item:item('a'),url:'a',position:80};if(a==='progress.prepare')return {position:80,sync:{state:'synced',pending:0}};},()=>1,()=>{});
 await p.play(item('a'),15);assert.equal(audio.currentTime,15);assert.equal(calls.includes('progress.prepare'),false);
 p.pause();p.seek(10);await p.toggle();assert.equal(audio.currentTime,10);assert.equal(calls.includes('progress.prepare'),false);p.clear();await p.dispose();
});
test('pause promotes the last periodic checkpoint only once without duplicate pause or persist saves',async()=>{
 const {p,audio,calls}=fixture(()=>({item:item('a'),url:'a',position:0}));await p.play(item('a'));
 audio.currentTime=6;audio.dispatchEvent(new Event('timeupdate'));await p.flush();p.pause();await p.persist();p.pause();await p.persist();
 const saves=calls.filter(c=>c.action==='progress.save');assert.deepEqual(saves.map(c=>c.p.urgent),[false,true]);assert.deepEqual(saves.map(c=>c.p.progress.position),[6,6]);p.clear();await p.dispose();
});
for (const conflict of [false,true]) test(`user seek wins while resume ${conflict?'conflict is awaiting choice':'prepare is pending'}`,async()=>{
 const audio=new AudioMock();let finish,reads=0;
 const p=new Player(audio,async(a)=>{
  if(a==='playback.resolve')return {item:item('a'),url:'a',position:10};
  if(a==='progress.prepare')return reads++?new Promise(resolve=>finish=resolve):{position:10,sync:{state:'synced',pending:0}};
 },()=>1,()=>{});
 await p.play(item('a'));p.pause();const resuming=p.toggle();await new Promise(setImmediate);
 if(conflict){finish({position:10,sync:{state:'conflict',pending:1},conflict:{localPosition:10,cloudPosition:20,token:'old'}});await new Promise(setImmediate);assert.ok(p.conflict);}
 assert.equal(p.seek(90),true);
 if(!conflict)finish({position:10,sync:{state:'synced',pending:0}});
 await resuming;assert.equal(audio.currentTime,90);assert.equal(audio.paused,false);assert.equal(p.conflict,undefined);p.clear();await p.dispose();
});
