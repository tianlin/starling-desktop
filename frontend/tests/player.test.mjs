import {test} from 'node:test';
import assert from 'node:assert/strict';
import {Player} from '../dist/player.js';
class AudioMock extends EventTarget {
  paused=true;currentTime=0;duration=120;volume=1;playbackRate=1;src='';
  seekable={length:1,start:()=>0,end:()=>120};error=null;
  load(){this.dispatchEvent(new Event('loadedmetadata'));}
  async play(){this.paused=false;this.dispatchEvent(new Event('playing'));}
  pause(){this.paused=true;this.dispatchEvent(new Event('pause'));}
  removeAttribute(name){if(name==='src')this.src='';}
}
const item=id=>({kind:'episode',id,title:id,sourceUrl:'https://www.xiaoyuzhoufm.com/episode/'+id,restricted:false});
function fixture(resolver,{save=()=>null,epoch=()=>1}={}){
 const audio=new AudioMock(),calls=[];
 const api=async(action,p)=>{calls.push({action,p}); if(action==='playback.resolve')return resolver(p);if(action==='progress.save')return save(p);return null;};
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
 const {p}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}),{save:()=>{if(fail)throw Error('disk locked');}});
 await p.play(item('a'));await p.persist();assert.match(p.error,/disk locked/);
 fail=false;await p.persist();assert.equal(p.error,'');await p.dispose();
});
test('a successful checkpoint cannot clear an unresolved media failure',async()=>{
 let fail=true;
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}),{save:()=>{if(fail)throw Error('disk locked');}});
 await p.play(item('a'));await p.persist();audio.error={code:3};audio.dispatchEvent(new Event('error'));
 const mediaFailure=p.error;assert.match(mediaFailure,/音频无法播放/);
 fail=false;await p.persist();assert.equal(p.error,mediaFailure);await p.dispose();
});
test('playing does not hide a checkpoint failure before saving actually recovers',async()=>{
 const {p,audio}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}),{save:()=>{throw Error('disk locked');}});
 await p.play(item('a'));await p.persist();audio.dispatchEvent(new Event('playing'));
 assert.match(p.error,/disk locked/);await p.dispose();
});
test('a late checkpoint failure from an old selection cannot change the new selection error',async()=>{
 let rejectSave,resolveNext;
 const {p}=fixture(({id})=>id==='b'?new Promise(resolve=>resolveNext=resolve):({item:item(id),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}),{save:()=>new Promise((_,reject)=>rejectSave=reject)});
 await p.play(item('a'));const saving=p.persist();await new Promise(setImmediate);
 p.clear();const playing=p.play(item('b'));rejectSave(Error('old disk failure'));await saving;await new Promise(setImmediate);
 assert.equal(p.item.id,'b');assert.equal(p.state,'resolving');assert.equal(p.error,'');
 resolveNext({item:item('b'),url:'https://media.xyzcdn.net/b.wav',position:0,epoch:1});await playing;p.clear();await p.dispose();
});
test('a checkpoint failure cannot change the player after the account epoch changes',async()=>{
 let currentEpoch=1,rejectSave;
 const {p}=fixture(()=>({item:item('a'),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:1}),{epoch:()=>currentEpoch,save:()=>new Promise((_,reject)=>rejectSave=reject)});
 await p.play(item('a'));const saving=p.persist();await new Promise(setImmediate);currentEpoch=2;
 rejectSave(Error('old account failure'));await saving;assert.equal(p.error,'');p.clear();await p.dispose();
});
for(const transition of ['selection','clear','account'])test(`a rejected old resume cannot change player state after ${transition}`,async()=>{
 let currentEpoch=1,rejectPlay;
 const {p,audio}=fixture(({id})=>({item:item(id),url:'https://media.xyzcdn.net/a.wav',position:0,epoch:currentEpoch}),{epoch:()=>currentEpoch});
 await p.play(item('a'));p.pause();const normalPlay=audio.play.bind(audio);
 audio.play=()=>new Promise((_,reject)=>rejectPlay=reject);const resuming=p.toggle();audio.play=normalPlay;
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
