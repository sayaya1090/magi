import {createRequire} from 'node:module';
import fs from 'node:fs';
import assert from 'node:assert/strict';
const require=createRequire(new URL('../../web/e2e/package.json',import.meta.url));
const {chromium}=require('@playwright/test');const browser=await chromium.launch();
try {
 const page=await browser.newPage(),calls=[],errors=[];let pendingRestart=true;page.on('pageerror',e=>errors.push(e.message));
 await page.route('http://127.0.0.1:8738/**',async route=>{
  const r=route.request(),url=new URL(r.url());if(url.pathname==='/')return route.fulfill({contentType:'text/html',body:fs.readFileSync(new URL('../web/index.html',import.meta.url),'utf8')});
  assert.equal(r.headers().authorization,'Bearer test');const body=r.postData()?JSON.parse(r.postData()):null;calls.push({path:url.pathname,body});
  if(url.pathname==='/api/events')return route.fulfill({contentType:'text/event-stream',body:'data: {"sessionId":"foreign","type":"part.appended","data":{"part":{"text":"foreign"}}}\n\ndata: {"sessionId":"s","type":"part.appended","data":{"part":{"text":"safe <script>"}}}\n\ndata: {"sessionId":"s","type":"permission.requested","data":{"callId":"p","name":"restart","args":{"resource":"allowed"}}}\n\n'});
  if(url.pathname==='/api/operations/pending')return route.fulfill({contentType:'application/json',body:JSON.stringify(pendingRestart?[{id:'one-use',environment:'docker:test:',resource:'allowed',impact:'interruption',recovery:'inspect health'}]:[])});if(url.pathname==='/api/operations/decision'){pendingRestart=false;return route.fulfill({contentType:'application/json',body:JSON.stringify({executed:true,verified:true})})}
  const result=url.pathname==='/api/operations/config'?{target:'docker:test:',resources:['allowed']}:url.pathname==='/api/connect'?{binding:'b',session:'s'}:{ok:true};return route.fulfill({contentType:'application/json',body:JSON.stringify(result)});
 });
 await page.goto('http://127.0.0.1:8738');await page.locator('#token').fill('test');await page.locator('#connect').click();await page.locator('#send').waitFor({state:'visible'});await page.waitForFunction(()=>!document.getElementById('send').disabled);
 await page.waitForFunction(()=>document.getElementById('events').textContent.includes('safe <script>'));assert.ok(!(await page.locator('#events').textContent()).includes('foreign'));assert.equal(calls.find(x=>x.path==='/api/connect').body.target,'docker:test:');
 await page.getByRole('button',{name:'deny',exact:true}).click();assert.ok(calls.some(x=>x.body?.decision==='deny'));
 await page.locator('#prompt').fill('Inspect status');await page.locator('#send').click();await page.waitForFunction(()=>document.getElementById('prompt').value==='');await page.locator('#stop').click();await page.waitForTimeout(50);assert.ok(calls.some(x=>x.body?.method==='interrupt'));await page.getByRole('button',{name:'재시작 승인',exact:true}).waitFor();page.once('dialog',d=>d.dismiss());await page.getByRole('button',{name:'재시작 승인',exact:true}).click();assert.equal(calls.filter(c=>c.path==='/api/operations/decision').length,0);page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'재시작 승인',exact:true}).click();await page.waitForFunction(()=>document.getElementById('status').textContent.includes('상태 확인이 완료'));assert.equal(calls.find(c=>c.path==='/api/operations/decision').body.id,'one-use');assert.deepEqual(errors,[]);console.log('PASS operations UI: fixed target, auth, text rendering, session filter, denial, chat and stop');
} finally {await browser.close()}
