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
 assert.ok(await tall.evaluate(e=>e.getBoundingClientRect().height<=Math.min(420,innerHeight*.55)+1),'tall chart fits height');
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
 console.log('Mermaid render, interaction, repeated IDs, malformed input, policy, streaming and offline tests passed');
} finally {await browser.close();}
