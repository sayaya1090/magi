import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createRequire} from 'node:module';
const require=createRequire(new URL('../../clients/web/e2e/package.json',import.meta.url));
const {chromium}=require('playwright');
const browser=await chromium.launch({headless:true});
try {
 const page=await browser.newPage();
 const calls=[];page.on('request',r=>calls.push(r.url()));
 await page.setContent('<html lang="en"><body></body></html>');
 await page.addScriptTag({content:await readFile(new URL('web/mermaid.min.js',import.meta.url),'utf8')});
 await page.addScriptTag({content:await readFile(new URL('web/view.js',import.meta.url),'utf8')});
 // JetBrains' Markdown preview may already have replaced the source with SVG.
 // Its style text must never be fed back to Mermaid as diagram source.
 await page.evaluate(()=>{
  const pre=document.createElement('pre');pre.id='ide-native';
  pre.innerHTML='<code class="language-mermaid"><svg id="mermaid-generated-dark" viewBox="0 0 100 40"><style>#mermaid-generated-dark{fill:#ccc}</style><text x="5" y="20">Native diagram</text></svg></code>';
  document.body.append(pre);window.magiMermaid.scan();
 });
 assert.equal(await page.locator('#ide-native svg').count(),1,'native rendered SVG must be preserved');
 await page.locator('#ide-native').evaluate(e=>e.remove());
 const nativeSource='flowchart LR\n A[에이전트] --> B[개발자]';
 await page.evaluate(source=>{
  const pre=document.createElement('pre');
  pre.innerHTML='<div data-fence-content=""><img class="copy-icon"></div><code class="language-mermaid"><svg id="native-kept" viewBox="0 0 100 40"><style>#native-kept{fill:gray}</style><g class="node"><text x="5" y="20">에이전트</text></g></svg></code>';
  pre.querySelector('[data-fence-content]').dataset.fenceContent=btoa(String.fromCharCode(...new TextEncoder().encode(source)));
  window.nativeDiagram=pre.querySelector('svg');document.body.append(pre);window.magiMermaid.scan();
 },nativeSource);
 await page.locator('.magi-diagram #native-kept').waitFor();
 assert.equal(await page.evaluate(()=>document.querySelector('#native-kept')===window.nativeDiagram),true,'native SVG must be moved, not re-rendered');
 assert.equal(await page.locator('.magi-diagram details pre').textContent(),nativeSource,'copy source must retain UTF-8, never SVG CSS');
 await page.getByRole('button',{name:'Zoom in',exact:true}).click();
 assert.equal(await page.locator('#native-kept').evaluate(e=>e.style.width),'125px');
 await page.getByRole('button',{name:'Fit',exact:true}).click();
 assert.equal(await page.locator('#native-kept').evaluate(e=>e.getBoundingClientRect().width),100,'small diagram keeps intrinsic size');
 const fitButton=page.getByRole('button',{name:'Fit',exact:true});
 await fitButton.focus();
 assert.equal(await fitButton.locator('.magi-diagram-tooltip').isVisible(),true,'keyboard tooltip');
 await page.locator('.magi-diagram').evaluate(e=>e.remove());
 const insert=async(source,streaming=false)=>page.evaluate(({source,streaming})=>{
  const p=document.createElement('pre'),c=document.createElement('code');c.dataset.lang='mermaid';c.dataset.streaming=String(streaming);c.textContent=source;
  // Real IDE markup includes a copy-button icon beside the code; it is not a diagram.
  const button=document.createElement('div');button.innerHTML='<img class="copy-icon">';p.append(button,c);document.body.append(p);
 },{source,streaming});
 await insert('flowchart LR\n A[Agent] --> B[Developer]');
 await page.locator('.magi-diagram svg[role=img]').waitFor();
 const fitWidth=await page.locator('.magi-diagram svg[role=img]').evaluate(e=>parseFloat(e.style.width));
 await page.getByRole('button',{name:'Zoom in',exact:true}).click();
 assert.ok(Math.abs(await page.locator('.magi-diagram svg[role=img]').evaluate(e=>parseFloat(e.style.width))-fitWidth*1.25)<.01);
 await page.getByRole('button',{name:'Fit',exact:true}).click();
 await page.locator('.node').first().click();
 assert.equal(await page.getByRole('button',{name:'Copy question'}).isEnabled(),true);
 assert.match(await page.locator('[role=status]').innerText(),/Selected/);
 await insert('flowchart LR\n A[Agent] --> B[Developer]');
 await page.waitForFunction(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length===2);
 const ids=await page.locator('.magi-diagram svg[role=img]').evaluateAll(es=>es.map(e=>e.id));assert.notEqual(ids[0],ids[1]);
 await insert('sequenceDiagram\n participant A as Agent\n participant B as Developer\n A->>B: Explain');
 await page.waitForFunction(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length===3);
 await insert('classDiagram\n class Service\n class Repository\n Service --> Repository');
 await page.waitForFunction(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length===4);
 await insert('stateDiagram-v2\n [*] --> Running\n Running --> Done');
 await page.waitForFunction(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length===5);
 await insert('flowchart LR\n A-->');
 await page.waitForFunction(()=>document.body.innerText.includes('Diagram could not be rendered'));
 await insert('flowchart LR\n A[<img src=x onerror=alert(1)>]');
 await page.waitForFunction(()=>document.body.innerText.includes('Use plain labels'));
 await insert('flowchart LR\n A-->B',true);
 assert.equal(await page.locator('code[data-streaming=true]').count(),1);
 // Let the insertion observer settle; only the completion attribute changes next.
 await page.waitForTimeout(250);
 await page.locator('code[data-streaming=true]').evaluate(e=>e.dataset.streaming='false');
 await page.waitForFunction(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length===6,{},{timeout:2000});
 await insert('flowchart TD\n'+Array.from({length:25},(_,i)=>`N${i} --> N${i+1}`).join('\n'));
 await page.waitForFunction(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length===7);
 const tall=page.locator('.magi-diagram svg[role=img]').last();
 // A tall chart (a sequence diagram, a long TD flow) is fitted to the column's width, not shrunk
 // into a short box where its text cannot be read; the whole of it shows and the page scrolls.
 assert.ok(await tall.evaluate(e=>{const c=e.closest('.magi-diagram-canvas'),w=e.viewBox.baseVal.width;return e.getBoundingClientRect().width>=Math.min(w,c.clientWidth)-1;}),'tall chart keeps a readable width');
 assert.ok(await tall.evaluate(e=>{const c=e.closest('.magi-diagram-canvas');return c.scrollHeight<=c.clientHeight+1;}),'tall chart is not clipped inside its box');
 assert.equal(calls.length,0,'offline diagram must not request external resources');
 await page.setViewportSize({width:390,height:800});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'narrow viewport overflow');
 // Match IDE light/dark surfaces and check the controls rather than assuming
 // currentColor yields a suitable border. The same icons remain labelled.
 for (const theme of ['light','dark']) {
  await page.evaluate(theme=>{
   document.body.style.background=theme==='dark'?'#1e1f22':'#ffffff';
   document.body.style.color=theme==='dark'?'#dfe1e5':'#24292f';
  },theme);
  const button=page.getByRole('button',{name:'Zoom in',exact:true}).first();
  const colors=await button.evaluate(e=>({border:getComputedStyle(e).borderTopColor,text:getComputedStyle(e).color}));
  assert.notEqual(colors.border,colors.text,'border must not use the full text color');
  assert.equal(await button.locator('svg[aria-hidden=true]').count(),1);
  await page.keyboard.press('Tab');
  await button.focus();
  assert.notEqual(await button.evaluate(e=>getComputedStyle(e).outlineStyle),'none');
  await page.locator('.magi-diagram').first().screenshot({path:`/tmp/magi-diagram-${theme}.png`});
 }
 // Metadata survives a plain Markdown fence, with accessible local questions.
 await page.setViewportSize({width:1100,height:800});
 await insert('flowchart LR\n A[Agent] --> B[Developer]\n%% magi-descriptions {"Agent":"Reads the request and prepares a reply."}\n%% magi-animate');
 await page.waitForFunction(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length===8);
 const interactive=page.locator('.magi-diagram').last();
 const node=interactive.locator('.node').first();
 await node.click();
 assert.equal(await interactive.locator('aside p').textContent(),'Reads the request and prepares a reply.');
 // Details go below the picture as notes; the diagram keeps the whole column width.
 const positions=await interactive.evaluate(e=>{const c=e.querySelector('.magi-diagram-canvas').getBoundingClientRect(),i=e.querySelector('aside').getBoundingClientRect(),l=e.querySelector('.magi-diagram-layout').getBoundingClientRect();return {canvasBottom:c.bottom,canvasW:c.width,layoutW:l.width,infoTop:i.top,infoW:i.width};});
 assert.ok(positions.infoTop>=positions.canvasBottom,'details appear below the diagram '+JSON.stringify(positions));
 assert.ok(positions.canvasW>=positions.layoutW-1,'a selected node does not narrow the diagram '+JSON.stringify(positions));
 await node.click({button:'right'});
 assert.equal(await interactive.getByLabel('Follow-up question').evaluate(e=>e===document.activeElement),true);
 await interactive.getByLabel('Follow-up question').fill('How does Agent retry?');
 await page.evaluate(()=>Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{window.copiedQuestion=text;}}}));
 await interactive.getByRole('button',{name:'Copy question',exact:true}).click();
 assert.match(await page.evaluate(()=>window.copiedQuestion),/^How does Agent retry\?\n\n```mermaid/);
 const edge=interactive.locator('.flowchart-link').first();
 assert.equal(await edge.evaluate(e=>getComputedStyle(e).animationName),'magi-flow');
 await interactive.getByLabel('Animate flow').uncheck();
 assert.equal(await edge.evaluate(e=>getComputedStyle(e).animationName),'none');
 await interactive.getByLabel('Animate flow').check();
 await page.emulateMedia({reducedMotion:'reduce'});
 assert.equal(await edge.evaluate(e=>getComputedStyle(e).animationName),'none');
 await page.emulateMedia({reducedMotion:'no-preference'});
 await interactive.locator('.node').last().click();
 assert.match(await interactive.locator('aside p').textContent(),/No explanation was supplied/);
 await node.focus();await page.keyboard.press('Shift+F10');
 assert.equal(await interactive.getByLabel('Follow-up question').evaluate(e=>e===document.activeElement),true);
 await page.evaluate(()=>Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async()=>{throw Error('denied');}}}));
 await interactive.getByRole('button',{name:'Copy question',exact:true}).click();
 assert.match(await interactive.getByLabel('Question to copy',{exact:true}).inputValue(),/```mermaid/);
 const copiedOnce=await interactive.getByLabel('Question to copy',{exact:true}).inputValue();
 await interactive.getByRole('button',{name:'Copy question',exact:true}).click();
 assert.equal(await interactive.getByLabel('Question to copy',{exact:true}).inputValue(),copiedOnce,'retries never duplicate context');
 await page.setViewportSize({width:390,height:800});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'details fit narrow screens');
 await interactive.screenshot({path:'/tmp/magi-diagram-details.png'});
 // Named arrows (A route@--> B) carry only their name; flow cases must still move them.
 const n=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await insert('flowchart LR\n A[Start] route@--> B[Check]\n B save@--> C[Save]\n B --> D[Reject]\n%% magi-flows {"Case 1":["Start","Check","Save"]}');
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,n+1);
 const named=page.locator('figure.magi-diagram').last();
 await named.locator('.magi-diagram-cases input[value="Case 1"]').check();
 assert.equal(await named.evaluate(f=>[...f.querySelectorAll('.flowchart-link')].filter(e=>getComputedStyle(e).animationName==='magi-flow').map(e=>e.getAttribute('data-id')).sort().join(',')),'route,save','named arrows follow their case');
 // A diagram drawn while hidden must not freeze its named arrows' ends (all nodes measure 0 then).
 const m=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await page.evaluate(()=>{const d=document.createElement('div');d.id='hiddenbox';d.style.display='none';d.innerHTML='<pre><code class="language-mermaid"></code></pre>';d.querySelector('code').textContent='flowchart LR\n A[Start] route@--> B[Check]\n B save@--> C[Save]\n%% magi-flows {"Case 1":["Start","Check","Save"]}';document.body.append(d);});
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,m+1);
 await page.evaluate(()=>{document.getElementById('hiddenbox').style.display='block';});
 const shown=page.locator('#hiddenbox figure.magi-diagram');
 await shown.locator('.magi-diagram-cases input[value="Case 1"]').check();
 assert.equal(await shown.evaluate(f=>[...f.querySelectorAll('.flowchart-link')].filter(e=>getComputedStyle(e).animationName==='magi-flow').map(e=>e.getAttribute('data-id')).sort().join(',')),'route,save','named arrows drawn while hidden follow their case once shown');
 // A composer host (JetBrains, the web console) gets no question field at all, and picking a case
 // lets go of the selected node.
 await page.evaluate(()=>{window.magiDiagramComposer=true;});
 const c0=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await insert('flowchart LR\n A[Start] --> B[Check]\n B --> C[Save]\n%% magi-flows {"Case 1":["Start","Check","Save"]}');
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,c0+1);
 const host=page.locator('figure.magi-diagram').last();
 await host.locator('svg .node').filter({hasText:'Check'}).first().click();
 assert.equal(await host.evaluate(f=>f.querySelectorAll('textarea').length+f.querySelectorAll('.magi-diagram-info label').length),0,'composer host has no question field');
 assert.equal(await host.getByRole('button',{name:'Copy question'}).count(),0,'composer host has no copy button');
 assert.equal(await host.locator('svg .magi-selected').count(),1);
 await host.locator('.magi-diagram-cases label').filter({hasText:'Case 1'}).click();
 assert.equal(await host.locator('svg .magi-selected').count(),0,'picking a case lets go of the node');
 // Arrows are picked like nodes: chip label "A → B", notes below, only that arrow flows, and it lifts.
 const e0=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await page.evaluate(()=>{window.__picked=[];addEventListener('magi-diagram-select',e=>window.__picked.push(e.detail.label));});
 await insert('flowchart LR\n A[Start] --> B[Check]\n B --> C[Save]');
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,e0+1);
 const arrows=page.locator('figure.magi-diagram').last();
 await arrows.locator('.magi-edge-hit[data-edge-of="L_A_B_0"]').click({force:true,timeout:3000});
 const got=await arrows.evaluate(f=>({heading:f.querySelector('aside strong').textContent,near:[...f.querySelectorAll('.flowchart-link.magi-flow-near')].map(e=>e.getAttribute('data-id')).join(','),sel:f.querySelectorAll('svg .magi-selected').length,lift:getComputedStyle(f.querySelector('.flowchart-link.magi-edge-selected')).filter,hitW:getComputedStyle(f.querySelector('.magi-edge-hit')).strokeWidth}));
 assert.equal(got.heading,'Start → Check','arrow notes '+JSON.stringify(got));
 assert.equal(got.near,'L_A_B_0','only the picked arrow flows '+JSON.stringify(got));
 assert.equal(got.sel,0,'no node stays selected');
 assert.notEqual(got.lift,'none','picked arrow is elevated');
 assert.equal(got.hitW,'14px','arrow hit area');
 assert.equal(await page.evaluate(()=>window.__picked.at(-1)),'Start → Check','arrow chip label');
 await arrows.locator('svg .node').filter({hasText:'Save'}).first().click();
 assert.equal(await arrows.locator('.flowchart-link.magi-edge-selected').count(),0,'picking a node lets go of the arrow');
 // Sequence messages are relation lines like flowchart arrows; the same path picks them.
 const q0=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await insert('sequenceDiagram\n participant U as User\n participant D as Daemon\n U->>D: submit\n D-->>U: reply');
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,q0+1);
 const seq=page.locator('figure.magi-diagram').last();
 await seq.locator('.magi-edge-hit[data-edge-of="i1"]').click({force:true,timeout:3000});
 const sg=await seq.evaluate(f=>({heading:f.querySelector('aside strong').textContent,near:[...f.querySelectorAll('.magi-edge.magi-flow-near')].map(e=>e.getAttribute('data-id')).join(','),lift:getComputedStyle(f.querySelector('.magi-edge.magi-edge-selected')).filter}));
 assert.equal(sg.heading,'Daemon → User: reply','message notes '+JSON.stringify(sg));
 assert.equal(sg.near,'i1','only the picked message moves '+JSON.stringify(sg));
 assert.notEqual(sg.lift,'none','picked message is elevated');
 assert.equal(await page.evaluate(()=>window.__picked.at(-1)),'Daemon → User: reply','message chip label');
 await seq.locator('.messageText').filter({hasText:'submit'}).click({force:true});
 assert.equal(await seq.evaluate(f=>f.querySelector('aside strong').textContent),'User → Daemon: submit','message text picks its message');
 console.log('Mermaid render, interaction, repeated IDs, malformed input, policy, streaming and offline tests passed');
} finally {await browser.close();}
