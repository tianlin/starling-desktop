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
function fixture(resolver){
 const audio=new AudioMock(),calls=[];
 const api=async(action,p)=>{calls.push({action,p}); if(action==='playback.resolve')return resolver(p);return null;};
 const p=new Player(audio,api,()=>1,()=>{});return {p,audio,calls};
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
