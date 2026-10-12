import {test} from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import fs from 'node:fs';
const source=fs.readFileSync(new URL('../code.js',import.meta.url),'utf8');
function setup(){const sent=[], fonts=[];const node={id:'text:1',name:'Label',type:'TEXT',characters:'before',fontName:{family:'Inter',style:'Regular'},parent:{id:'page-a',type:'PAGE'}};const f={fileKey:'file-a',currentPage:{id:'page-a',selection:[node],children:[node]},ui:{postMessage:m=>sent.push(m)},showUI(){},on(){},getNodeByIdAsync:async id=>id===node.id?node:null,loadFontAsync:async font=>fonts.push(font),mixed:Symbol(),createFrame(){throw Error('not used')}};vm.runInNewContext(source,{figma:f,__html__:'',console});sent.length=0;f.ui.onmessage({kind:'session',session:'s1'});return {f,node,sent,fonts};}
const request={session:'s1',kind:'tool',callId:'c1',name:'figma_set_text',fileKey:'file-a',pageId:'page-a',args:{id:'text:1',text:'after'}};
test('text editing loads font and returns original call ID',async()=>{const x=setup();await x.f.ui.onmessage(request);assert.equal(x.node.characters,'after');assert.equal(x.fonts.length,1);assert.equal(x.sent[0].callId,'c1');assert.equal(x.sent[0].result.text,'after');});
test('foreign file and stale page cannot mutate',async()=>{for(const field of ['fileKey','pageId']){const x=setup();await x.f.ui.onmessage({...request,[field]:'other'});assert.equal(x.node.characters,'before');assert.ok(x.sent[0].error);}});
test('font failure is explicit and leaves content unchanged',async()=>{const x=setup();x.f.loadFontAsync=async()=>{throw Error('missing font')};await x.f.ui.onmessage(request);assert.equal(x.node.characters,'before');assert.match(x.sent[0].error,/missing font/);});
test('selection reads captured IDs rather than later selection',async()=>{const x=setup();x.f.currentPage.selection=[];await x.f.ui.onmessage({...request,name:'figma_selection',args:{},selectionIds:['text:1']});assert.equal(x.sent[0].result[0].id,'text:1');});
test('malformed messages do not produce tool results',async()=>{const x=setup();for(const m of [null,[],{}, {...request,args:[]}])await x.f.ui.onmessage(m);assert.equal(x.sent.length,0);});
test('page changes during font loading block mutation',async()=>{const x=setup();x.f.loadFontAsync=async()=>{x.f.currentPage.id='page-b'};await x.f.ui.onmessage(request);assert.equal(x.node.characters,'before');assert.ok(x.sent[0].error);});
test('explicit fallback document identity binds once',async()=>{const x=setup();x.f.fileKey=undefined;await x.f.ui.onmessage({kind:'bind',documentId:'document_123'});await x.f.ui.onmessage({kind:'bind',documentId:'different_123'});await x.f.ui.onmessage({...request,fileKey:'local:document_123'});assert.equal(x.node.characters,'after');});
test('another page node and unexpected arguments cannot mutate',async()=>{const x=setup();x.node.parent.id='different';await x.f.ui.onmessage(request);assert.equal(x.node.characters,'before');assert.ok(x.sent[0].error);const y=setup();await y.f.ui.onmessage({...request,args:{...request.args,shell:'no'}});assert.equal(y.node.characters,'before');assert.ok(y.sent[0].error);});
test('mixed-font text loads each distinct font before editing',async()=>{const x=setup();x.node.fontName=x.f.mixed;x.node.getRangeAllFontNames=()=>[{family:'Inter',style:'Regular'},{family:'Inter',style:'Bold'},{family:'Inter',style:'Regular'}];await x.f.ui.onmessage(request);assert.deepEqual(x.fonts.map(f=>f.style),['Regular','Bold']);assert.equal(x.node.characters,'after');});
test('missing fonts reject without mutation',async()=>{const x=setup();x.node.hasMissingFont=true;await x.f.ui.onmessage(request);assert.equal(x.node.characters,'before');assert.equal(x.fonts.length,0);assert.match(x.sent[0].error,/missing font/);});

test('left panel tracks viewport size and zoom; window mode releases placement and close clears timer',async()=>{
 const sizes=[],positions=[],listeners={};let tick,cleared=false;
 const f={currentPage:{id:'page',selection:[],children:[]},viewport:{bounds:{x:100,y:200,width:1000,height:800},zoom:1},ui:{postMessage(){},resize:(...x)=>sizes.push(x),reposition:(...x)=>positions.push(x)},showUI(){},on:(name,fn)=>listeners[name]=fn};
 vm.runInNewContext(source,{figma:f,__html__:'',console,setInterval:fn=>{tick=fn;return 7},clearInterval:id=>{assert.equal(id,7);cleared=true}});
 assert.deepEqual(sizes.at(-1),[400,760]);assert.deepEqual(positions.at(-1),[100,200]);
 f.viewport.bounds.width=500;f.viewport.bounds.height=600;f.viewport.zoom=2;tick();
 assert.deepEqual(sizes.at(-1),[400,1160]);assert.deepEqual(positions.at(-1),[100,200]);
 await f.ui.onmessage({kind:'window-mode',mode:'window'});const count=positions.length;
 f.viewport.bounds.x=500;tick();assert.equal(positions.length,count);
 await f.ui.onmessage({kind:'window-mode',mode:'left'});assert.ok(positions.length>count);
 listeners.close();assert.equal(cleared,true);
});
