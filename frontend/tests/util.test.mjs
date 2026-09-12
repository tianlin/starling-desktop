import {test} from 'node:test';import assert from 'node:assert/strict';
import {formatTime,parseTimestamp,canUseSpace,libraryStatus} from '../dist/util.js';
test('time formatting and chapter validation',()=>{
 assert.equal(formatTime(3661),'1:01:01');assert.equal(formatTime(NaN),'0:00');
 assert.equal(parseTimestamp('01:23:45'),5025);assert.equal(parseTimestamp('99:99'),null);
});
test('typing space is never hijacked',()=>{
 assert.equal(canUseSpace({tagName:'INPUT',isContentEditable:false,closest:()=>null}),false);
 assert.equal(canUseSpace({tagName:'DIV',isContentEditable:true,closest:()=>null}),false);
 assert.equal(canUseSpace({tagName:'BODY',isContentEditable:false,closest:()=>null}),true);
});
test('unknown pagination end never displays fully loaded',()=>{
 assert.match(libraryStatus({status:'unknown_end',items:[{}],complete:false}),/未确认/);
 assert.match(libraryStatus({status:'complete',items:[{}],complete:true}),/结束/);
});
