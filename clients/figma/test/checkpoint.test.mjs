import {test} from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import fs from 'node:fs';
const source=fs.readFileSync(new URL('../code.js',import.meta.url),'utf8');
function setup(){
 let seq=0;const nodes=new Map(),sent=[];
 const root={id:'root',type:'DOCUMENT',children:[]};
 function node(type,parent,props={}){
  const n={id:'n'+(++seq),type,name:type,parent,removed:false,...props};nodes.set(n.id,n);
  if(parent)parent.children.push(n);
  if(type==='PAGE'||type==='FRAME')n.children=[];
  if(type==='PAGE')n.selection=[];
  n.remove=()=>{n.removed=true;nodes.delete(n.id);if(n.parent)n.parent.children=n.parent.children.filter(c=>c!==n);for(const c of [...(n.children||[])])c.remove();};
  n.resize=(w,h)=>{n.width=w;n.height=h;};
  n.clone=()=>{
   const make=(old,parent)=>{const copy=node(old.type,parent,Object.fromEntries(['name','characters','fontName','width','height'].filter(k=>k in old).map(k=>[k,old[k]])));for(const c of old.children||[])make(c,copy);return copy;};return make(n,n.parent);
  };
  return n;
 }
 const page=node('PAGE',root,{name:'Original'}),text=node('TEXT',page,{characters:'before',fontName:{family:'Inter',style:'Regular'}});page.selection=[text];
 const saved=new Map();
 const f={clientStorage:{getAsync:async k=>saved.get(k),setAsync:async(k,v)=>saved.set(k,JSON.parse(JSON.stringify(v))),deleteAsync:async k=>saved.delete(k)},fileKey:'file-a',root,currentPage:page,mixed:Symbol(),ui:{postMessage:m=>sent.push(m)},showUI(){},on(){},getNodeByIdAsync:async id=>nodes.get(id),loadFontAsync:async()=>{},setCurrentPageAsync:async p=>{f.currentPage=p;},createFrame:()=>node('FRAME',f.currentPage,{width:100,height:100})};
 vm.runInNewContext(source,{figma:f,__html__:'',console});sent.length=0;
 const session=s=>f.ui.onmessage({kind:'session',session:s});session('s1');
 async function call(name,args={},sid='s1'){
  const callId='host-'+(++seq);await f.ui.onmessage({kind:'tool',session:sid,callId,name,args,fileKey:'file-a',pageId:f.currentPage.id,selectionIds:[text.id]});
  const out=sent.findLast(x=>x.kind==='tool-result'&&x.callId===callId);assert.ok(out,'missing tool response');return out;
 }
 async function ok(name,args={}){const out=await call(name,args);assert.equal(out.error,undefined);return out.result;}
 return {f,page,text,nodes,root,call,ok,session,saved,restart:()=>{vm.runInNewContext(source,{figma:f,__html__:'',console});session('s1');}};
}

test('Figma checkpoint forks isolate edits, adoption advances, host completion publishes and cleans',async()=>{
 const x=setup();await x.ok('figma_checkpoint',{action:'begin'});
 const a=await x.ok('figma_checkpoint',{action:'fork'}),b=await x.ok('figma_checkpoint',{action:'fork'});
 const left=await x.ok('figma_page',{branch:a.branch}),right=await x.ok('figma_page',{branch:b.branch});
 await x.ok('figma_set_text',{branch:a.branch,id:left.nodes[0].id,text:'baseline'});
 await x.ok('figma_set_text',{branch:b.branch,id:right.nodes[0].id,text:'candidate'});
 assert.equal(x.text.characters,'before');assert.equal(x.nodes.get(left.nodes[0].id).characters,'baseline');
 const missing=await x.call('figma_set_text',{id:x.text.id,text:'forbidden'});assert.match(missing.error,/explicit branch/);
 const foreign=await x.call('figma_set_text',{branch:a.branch,id:right.nodes[0].id,text:'forbidden'});assert.match(foreign.error,/bound resource/);
 await x.ok('figma_checkpoint',{action:'adopt',branch:b.branch});
 const stale=await x.call('figma_set_text',{branch:a.branch,id:left.nodes[0].id,text:'forbidden'});assert.match(stale.error,/stale/);
 const next=await x.ok('figma_checkpoint',{action:'fork'}),nextPage=await x.ok('figma_page',{branch:next.branch});assert.equal(nextPage.nodes[0].text,'candidate');
 await x.ok('figma_create_frame',{branch:next.branch,name:'new frame',width:320,height:240});
 await x.ok('figma_checkpoint',{action:'adopt',branch:next.branch});
 const ready=await x.ok('figma_checkpoint',{action:'finish'});assert.equal(x.text.characters,'before');
 await x.ok('__magi_checkpoint_complete',{token:ready.checkpoint_token});
 assert.equal(x.text.characters,'candidate');assert.equal(x.page.children[1].name,'new frame');assert.equal(x.root.children.length,1);
 const status=await x.ok('figma_checkpoint',{action:'status'});assert.equal(status.phase,'cleaned');assert.equal(status.history.length,2);
 await x.ok('__magi_checkpoint_complete',{token:ready.checkpoint_token});assert.equal(x.page.children.length,2,'retry duplicated final frame');
});

test('Figma checkpoints reject original conflicts, untracked branch edits and foreign sessions',async()=>{
 const x=setup();await x.ok('figma_checkpoint',{action:'begin'});const a=await x.ok('figma_checkpoint',{action:'fork'});
 const page=await x.ok('figma_page',{branch:a.branch});x.nodes.get(page.nodes[0].id).characters='outside tool';
 let rejected=await x.call('figma_checkpoint',{action:'adopt',branch:a.branch});assert.match(rejected.error,/outside/);
 const b=await x.ok('figma_checkpoint',{action:'fork'});await x.ok('figma_checkpoint',{action:'adopt',branch:b.branch});
 const ready=await x.ok('figma_checkpoint',{action:'finish'});x.text.characters='user edit';
 rejected=await x.call('__magi_checkpoint_complete',{token:ready.checkpoint_token});assert.match(rejected.error,/original page changed/);assert.equal(x.text.characters,'user edit');assert.ok(x.root.children.length>1);
 x.session('s2');rejected=await x.call('figma_page',{branch:b.branch},'s2');assert.match(rejected.error,/Begin a checkpoint/);
});

test('Figma session changes while fonts load leave both original and branch unchanged',async()=>{
 const x=setup();await x.ok('figma_checkpoint',{action:'begin'});const b=await x.ok('figma_checkpoint',{action:'fork'});const page=await x.ok('figma_page',{branch:b.branch});
 x.f.loadFontAsync=async()=>{x.session('s2');};const result=await x.call('figma_set_text',{branch:b.branch,id:page.nodes[0].id,text:'forbidden'});
 assert.match(result.error,/session changed/);assert.equal(x.text.characters,'before');assert.equal(x.nodes.get(page.nodes[0].id).characters,'before');
});


test('Figma checkpoint survives plugin restart before host completion',async()=>{
 const x=setup();await x.ok('figma_checkpoint',{action:'begin'});const b=await x.ok('figma_checkpoint',{action:'fork'});const page=await x.ok('figma_page',{branch:b.branch});
 await x.ok('figma_set_text',{branch:b.branch,id:page.nodes[0].id,text:'persisted candidate'});await x.ok('figma_checkpoint',{action:'adopt',branch:b.branch});
 const ready=await x.ok('figma_checkpoint',{action:'finish'});x.restart();
 const status=await x.ok('figma_checkpoint',{action:'status'});assert.equal(status.ready,true);
 await x.ok('__magi_checkpoint_complete',{token:ready.checkpoint_token});assert.equal(x.text.characters,'persisted candidate');assert.equal(x.root.children.length,1);
});
