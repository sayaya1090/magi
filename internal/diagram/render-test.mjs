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
 // Selecting a node or an arrow keeps the reader's zoom; only Fit resets it.
 const zoomed=()=>page.locator('.magi-diagram svg[role=img]').evaluate(e=>parseFloat(e.style.width));
 await page.locator('.node').first().click();
 assert.ok(Math.abs(await zoomed()-fitWidth*1.25)<.01,'selecting a node keeps the zoom');
 await page.locator('.magi-edge-hit').first().dispatchEvent('click');
 assert.ok(Math.abs(await zoomed()-fitWidth*1.25)<.01,'selecting an arrow keeps the zoom');
 // Shift+wheel over the diagram zooms it; a plain wheel leaves the zoom to the page scroll.
 const overDiagram=await page.locator('.magi-diagram-canvas').first().boundingBox();
 await page.mouse.move(overDiagram.x+overDiagram.width/2,overDiagram.y+overDiagram.height/2);
 const beforeWheel=await zoomed();
 await page.keyboard.down('Shift'); await page.mouse.wheel(0,-100); await page.keyboard.up('Shift');
 await page.waitForFunction(w=>parseFloat(document.querySelector('.magi-diagram svg[role=img]').style.width)>w+.01,beforeWheel,{timeout:2000});
 const grown=await zoomed();
 await page.keyboard.down('Shift'); await page.mouse.wheel(0,100); await page.keyboard.up('Shift');
 await page.waitForFunction(w=>parseFloat(document.querySelector('.magi-diagram svg[role=img]').style.width)<w-.01,grown,{timeout:2000});
 const shrunk=await zoomed();
 await page.mouse.wheel(0,-100); await page.waitForTimeout(100);
 assert.ok(Math.abs(await zoomed()-shrunk)<.01,'a wheel without Shift does not zoom');
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
 // Material 3 cards have rounded corners (shape medium 12dp), and on a dark answer bubble a node is
 // outlined: its container was 1.00-1.22:1 against the bubble.
 const shape=await node.evaluate(n=>{const s=n.querySelector('.label-container')||n.querySelector(':scope > rect');const c=getComputedStyle(s);return {tag:s.tagName,rx:c.rx,stroke:c.stroke,width:c.strokeWidth};});
 assert.equal(shape.rx,'12px','node corners are rounded '+JSON.stringify(shape));
 assert.notEqual(shape.stroke,'none','node is outlined '+JSON.stringify(shape));
 assert.equal(shape.width,'1px','node outline is 1dp '+JSON.stringify(shape));
 // Clicking the background lets go of the selected node, so its elevation drops again.
 await interactive.locator('svg[role=img]').click({position:{x:2,y:2}});
 assert.equal(await node.evaluate(n=>n.classList.contains('magi-selected')),false,'background click deselects the node');
 assert.equal(await node.evaluate(n=>n.getAttribute('aria-pressed')),'false');
 assert.equal(await interactive.locator('aside').evaluate(e=>e.hidden),true,'notes close with the selection');
 await node.click();
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
 // Without notes of its own, an element still opens the follow-up question here, but no filler text.
 assert.equal(await interactive.locator('aside').evaluate(e=>e.hidden),false,'the question field stays reachable');
 assert.equal(await interactive.locator('aside p').evaluate(e=>e.hidden||!e.textContent),true,'no "no explanation" filler');
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
 // Labels use M3 body-medium: 14px, 0.25px tracking, 20px between lines, the host's whole family list;
 // edge labels sit on surface-container-highest and take on-surface (dark #E8E2D8).
 const typeCount=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await page.evaluate(()=>{document.body.style.fontFamily='-apple-system, "Segoe UI", sans-serif';});
 await insert('flowchart LR\n A["`Two line\nlabel`"] -->|check| B[One]');
 await page.waitForFunction(c=>document.querySelectorAll('.magi-diagram svg[role=img]').length===c+1,typeCount);
 const type=await page.locator('.magi-diagram').last().evaluate(box=>{const svg=box.querySelector('svg[role=img]'),t=svg.querySelector('.node text'),c=getComputedStyle(t);
  const lines=[...t.children].filter(e=>e.tagName.toLowerCase()==='tspan').map(e=>e.getBoundingClientRect().top);
  const r=t.closest('.node').querySelector('rect').getBoundingClientRect(),b=t.getBoundingClientRect();
  return {fam:c.fontFamily,size:c.fontSize,ls:c.letterSpacing,gap:lines[1]-lines[0],inside:b.top>=r.top&&b.bottom<=r.bottom,edge:getComputedStyle(svg.querySelector('.edgeLabel text')).fill};});
 assert.match(type.fam,/sans-serif/,'the host fallback list survives '+JSON.stringify(type));
 assert.equal(type.size,'14px'); assert.equal(type.ls,'0.25px','body-medium tracking '+JSON.stringify(type));
 assert.ok(Math.abs(type.gap-20)<0.6,'20px between label lines '+JSON.stringify(type));
 assert.ok(type.inside,'re-spaced lines stay inside the node '+JSON.stringify(type));
 assert.equal(type.edge,'rgb(232, 226, 216)','edge label text is on-surface '+JSON.stringify(type));
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
 // Only elements that have notes carry an (i) badge and a hover preview; a legend says what it means.
 // Every element stays pickable (its chip still goes to the composer), but one without notes opens
 // no empty notes, and opening or closing notes never changes the diagram's height.
 const d0=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await insert('flowchart LR\n P[Plain] --> T[Told]\n%% magi-descriptions {"Told":"Has notes.","Plain → Told":"Arrow notes."}');
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,d0+1);
 const desc=page.locator('figure.magi-diagram').last();
 const look=await desc.evaluate(f=>{const n=t=>[...f.querySelectorAll('svg .node')].find(n=>n.textContent.trim()===t);return {badges:f.querySelectorAll('svg .magi-note-badge').length,told:n('Told').querySelectorAll('.magi-note-badge').length,plain:n('Plain').querySelectorAll('.magi-note-badge').length,label:n('Told').textContent.trim(),aria:n('Told').getAttribute('aria-label')||'',legend:f.querySelectorAll('.magi-diagram-legend').length};});
 assert.equal(look.badges,2,'one badge per described node and arrow '+JSON.stringify(look));
 assert.equal(look.told,1,'the described node carries a badge '+JSON.stringify(look));
 assert.equal(look.plain,0,'a node without notes carries none '+JSON.stringify(look));
 assert.equal(look.label,'Told','the badge does not change the label (or the chip) '+JSON.stringify(look));
 assert.match(look.aria,/has notes/,'the badge is announced');
 assert.equal(look.legend,1,'a diagram with notes shows the legend');
 // The badge only means "has notes"; the legend must not read as if only badged elements attach.
 const lg=await desc.locator('.magi-diagram-legend').textContent();
 assert.match(lg,/has notes/,'legend names the badge '+lg);
 assert.match(lg,/every element/i,'legend says every element can be attached '+lg);
 assert.equal(await desc.locator('.magi-diagram-legend .magi-legend-hint').count(),1,'the attach hint is apart from the badge '+lg);
 assert.equal(await page.locator('figure.magi-diagram').filter({hasText:'Daemon'}).locator('.magi-diagram-legend').count(),0,'a diagram without notes shows no legend');
 const figH=()=>desc.evaluate(f=>Math.round(f.getBoundingClientRect().height));
 const h0=await figH();
 await desc.locator('svg .node').filter({hasText:'Told'}).first().hover();
 await page.waitForTimeout(600);
 assert.equal(await desc.locator('.magi-diagram-peek').isVisible(),true,'hovering a described node previews its notes');
 assert.equal(await desc.locator('.magi-diagram-peek').textContent(),'Has notes.');
 await page.mouse.move(1,1); await page.waitForTimeout(100);
 assert.equal(await desc.locator('.magi-diagram-peek').isVisible(),false,'the preview leaves with the pointer');
 await desc.locator('svg .node').filter({hasText:'Plain'}).first().click();
 assert.equal(await desc.locator('svg .magi-selected').count(),1,'a node without notes is still picked');
 assert.equal(await page.evaluate(()=>window.__picked.at(-1)),'Plain','its chip still goes to the composer');
 assert.equal(await desc.locator('aside').evaluate(e=>e.hidden),true,'a node without notes opens no empty notes');
 const h1=await figH();
 await desc.locator('svg .node').filter({hasText:'Told'}).first().click();
 assert.equal(await desc.locator('aside').evaluate(e=>e.hidden),false,'a described node opens its notes');
 assert.equal(await desc.locator('aside p').textContent(),'Has notes.');
 const h2=await figH();
 await desc.locator('svg[role=img]').click({position:{x:2,y:2}});
 const h3=await figH();
 assert.deepEqual([h1,h2,h3],[h0,h0,h0],'opening and closing notes keeps the diagram height');
 // A long description is previewed whole: no clamp, wider than the old 280px, and kept inside the
 // diagram box (an embedded panel may clip anything outside it).
 const L0=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 const longText='Line one explains what this component owns and why it exists in the design.\nLine two names the file and function that implement it, with the reason it is pure.\nLine three says how it is tested without a model, and what would break if it changed.\nLine four is here only to be longer than three lines.';
 await insert('flowchart LR\n Q[Quiet] --> W[Wordy]\n%% magi-descriptions '+JSON.stringify({Wordy:longText}));
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,L0+1);
 const lng=page.locator('figure.magi-diagram').filter({hasText:'Wordy'}).last();
 await lng.locator('svg .node').filter({hasText:'Wordy'}).first().hover();
 await page.waitForTimeout(600);
 const pk=await lng.evaluate(f=>{const p=f.querySelector('.magi-diagram-peek'),r=p.getBoundingClientRect(),b=f.getBoundingClientRect();return {shown:!p.hidden,text:p.textContent,clipped:p.scrollHeight>p.clientHeight+1,w:Math.round(r.width),right:r.right-b.right,bottom:r.bottom-b.bottom,left:r.left-b.left,top:r.top-b.top};});
 assert.equal(pk.shown,true,'long preview shows '+JSON.stringify(pk));
 assert.equal(pk.text,longText,'the preview carries the whole description');
 assert.equal(pk.clipped,false,'the preview is not clipped '+JSON.stringify(pk));
 assert.ok(pk.w>300,'a long preview is wider than 280px '+JSON.stringify(pk));
 assert.ok(pk.right<=1&&pk.left>=-1&&pk.bottom<=1&&pk.top>=-1,'the preview stays inside the diagram box '+JSON.stringify(pk));
 await page.mouse.move(1,1);
 // Every badge sits just right of its label (never on the last glyph), on the label's line, whatever
 // the shape: rect, circle, diamond (a transformed polygon), hexagon, a participant box, a bare actor.
 const b0=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await insert('flowchart LR\n R[Rect] --> C((Circle))\n C --> D{Diamond}\n D --> H{{Hexagon}}\n%% magi-descriptions {"Rect":"r","Circle":"c","Diamond":"d","Hexagon":"h"}');
 await insert('sequenceDiagram\n actor P as Person\n participant V as Viewer\n P->>V: ask\n%% magi-descriptions {"Person":"p","Viewer":"v"}');
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,b0+2);
 const placed=await page.evaluate(()=>[...document.querySelectorAll('figure.magi-diagram')].slice(-2).flatMap(f=>[...f.querySelectorAll('svg .node.magi-described, svg text.magi-described')].map(n=>{
  const text=n.tagName.toLowerCase()==='text', lab=text?n:(n.querySelector('.nodeLabel')||n.querySelector('foreignObject')||n.querySelector('text')), host=text?n.parentNode:n;
  const lb=lab.getBoundingClientRect();
  const cs=[...host.querySelectorAll(':scope > .magi-note-badge circle.b')].map(c=>c.getBoundingClientRect()).map(r=>({x:r.x+r.width/2,y:r.y+r.height/2}));
  const c=cs.sort((a,b)=>Math.abs(a.x-lb.right)-Math.abs(b.x-lb.right))[0];
  return {name:n.textContent.trim(),found:!!c,dx:c?Math.round(c.x-lb.right):null,dy:c?Math.round(c.y-(lb.top+lb.height/2)):null};})));
 assert.ok(placed.length>=8,'every described shape and participant is measured '+JSON.stringify(placed));
 assert.ok(placed.every(p=>p.found),'every described element carries a badge, a bare label too '+JSON.stringify(placed));
 assert.ok(placed.every(p=>p.dx>=8&&p.dx<=20),'the badge sits just right of the label, not on its last glyph '+JSON.stringify(placed));
 assert.ok(placed.every(p=>Math.abs(p.dy)<=3),'the badge is centred on the label line '+JSON.stringify(placed));
 // The preview answers the whole shape, not only its label: a sequence participant box away from its name.
 const hv0=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await insert('sequenceDiagram\n participant V as Viewer\n participant D as Daemon\n V->>D: submit\n%% magi-descriptions {"Viewer":"Box notes."}');
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,hv0+1);
 const hb=page.locator('figure.magi-diagram').last();
 await hb.scrollIntoViewIfNeeded();
 const spot=await hb.evaluate(f=>{const t=[...f.querySelectorAll('svg text.actor')].find(e=>e.textContent.trim()==='Viewer');const r=t.parentNode.querySelector(':scope > rect').getBoundingClientRect(),l=t.getBoundingClientRect();return {x:r.left+16,y:r.top+r.height/2,clear:r.left+16<l.left};});
 assert.ok(spot.clear,'the probe point is off the label '+JSON.stringify(spot));
 await page.mouse.move(1,1); await page.waitForTimeout(100);
 await page.mouse.move(spot.x,spot.y); await page.waitForTimeout(600);
 assert.equal(await hb.locator('.magi-diagram-peek').isVisible(),true,'hovering a participant box away from its name previews its notes');
 await page.mouse.move(1,1);
 // A class box carries its notes by its name (its text also holds the members), and answers its whole box.
 const cl0=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await insert('classDiagram\n class Sale {\n  -total : int\n  +pay() bool\n }\n class Item\n Sale --> Item\n%% magi-descriptions {"Sale":"Class notes."}');
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,cl0+1);
 const cb=page.locator('figure.magi-diagram').last();
 await cb.scrollIntoViewIfNeeded();
 const cs=await cb.evaluate(f=>{const n=[...f.querySelectorAll('svg .node')].find(e=>(e.querySelector('.label-group')||{}).textContent==='Sale');const r=n.getBoundingClientRect();return {described:n.classList.contains('magi-described'),badge:n.querySelectorAll('.magi-note-badge').length,x:r.left+r.width/2,y:r.bottom-8};});
 assert.equal(cs.described,true,'a class box is matched by its name '+JSON.stringify(cs));
 assert.equal(cs.badge,1,'a described class box carries a badge');
 await page.mouse.move(cs.x,cs.y); await page.waitForTimeout(600);
 assert.equal(await cb.locator('.magi-diagram-peek').isVisible(),true,'hovering a class box away from its name previews its notes');
 assert.equal(await cb.locator('.magi-diagram-peek').textContent(),'Class notes.');
 await page.mouse.move(1,1);
 // A stick-figure actor has no filled shape: a clear area around figure and name answers like a box.
 const af0=await page.evaluate(()=>document.querySelectorAll('.magi-diagram svg[role=img]').length);
 await insert('sequenceDiagram\n actor P as Pilot\n participant S as Server\n P->>S: ask\n%% magi-descriptions {"Pilot":"Figure notes."}');
 await page.waitForFunction(k=>document.querySelectorAll('.magi-diagram svg[role=img]').length===k,af0+1);
 const fg=page.locator('figure.magi-diagram').last();
 await fg.scrollIntoViewIfNeeded();
 const head=await fg.evaluate(f=>{const t=[...f.querySelectorAll('svg text.actor-man')].find(e=>e.textContent.trim()==='Pilot');const c=t.parentNode.querySelector(':scope > circle').getBoundingClientRect(),l=t.getBoundingClientRect();const x=c.left+c.width/2,y=c.top+c.height/2,u=document.elementFromPoint(x,y);return {x,y,clear:c.bottom<l.top,under:u?u.tagName+'.'+(u.getAttribute('class')||''):null,hit:!!t.parentNode.querySelector('.magi-figure-hit')};});
 assert.ok(head.clear,'the probe point is off the name '+JSON.stringify(head));
 await page.mouse.move(1,1); await page.waitForTimeout(100);
 await page.mouse.move(head.x,head.y); await page.waitForTimeout(600);
 assert.equal(await fg.locator('.magi-diagram-peek').isVisible(),true,'hovering a stick figure away from its name previews its notes');
 assert.equal(await fg.locator('.magi-diagram-peek').textContent(),'Figure notes.');
 await page.mouse.click(head.x,head.y); await page.waitForTimeout(200);
 assert.equal(await fg.locator('svg .magi-selected').count(),1,'clicking the figure picks the actor');
 assert.equal(await page.evaluate(()=>window.__picked.at(-1)),'Pilot','the figure attaches the actor');
 await page.mouse.move(1,1);
 console.log('Mermaid render, interaction, repeated IDs, malformed input, policy, streaming and offline tests passed');
} finally {await browser.close();}
