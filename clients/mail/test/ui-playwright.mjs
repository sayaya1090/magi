import {createRequire} from 'node:module';
import fs from 'node:fs';
import assert from 'node:assert/strict';
const require=createRequire(new URL('../../web/e2e/package.json',import.meta.url));
const {chromium}=require('@playwright/test');
const browser=await chromium.launch({headless:true});
try {
 const page=await browser.newPage();const errors=[],calls=[];let delayedDraft;let failRequest=false;let activeSession="session";page.on('pageerror',e=>errors.push(e.message));
 await page.route('http://127.0.0.1:8793/**',async route=>{
  const request=route.request(),path=new URL(request.url()).pathname;
  if(path==='/')return route.fulfill({contentType:'text/html',body:fs.readFileSync(new URL('../web/index.html',import.meta.url),'utf8')});
  const body=request.postData()?JSON.parse(request.postData()):undefined;calls.push({path,body,auth:request.headers().authorization});
  let result={};if(path==='/mail/list')result={account:'user@example.com',uidValidity:19,items:[{uid:7,subject:'<script>window.pwned=true</script>',from:'sender@example.com'},{uid:8,subject:'Next',from:'other@example.com'}]};
  if(path==='/mail/read')result={text:'<img src=x onerror=window.pwned=true>',raw:'Content-Type: text/html\n\n<script>window.pwned=true</script>'};
  if(path==='/api/connect')result={binding:'bind-'+body.target,session:'session'};
  if(path==='/api/events')return route.fulfill({contentType:'text/event-stream',body:'data: {"type":"part.appended","sessionId":"foreign","data":{"role":"assistant","part":{"kind":"text","text":"foreign output"}}}\n\ndata: {"type":"part.appended","sessionId":"session","data":{"role":"assistant","part":{"kind":"text","text":"model output"}}}\n\ndata: {"type":"permission.requested","sessionId":"session","data":{"callId":"approve-1","name":"mail_draft","args":{"subject":"Draft"}}}\n\ndata: {"type":"question.requested","sessionId":"session","data":{"callId":"question-1","question":"Send which wording?","options":["Formal"]}}\n\n'.replaceAll('\"sessionId\":\"session\"', '\"sessionId\":\"'+activeSession+'\"')});
  if(path==='/mail/draft'&&delayedDraft){await delayedDraft.promise;}if(path==='/api/request'&&failRequest)return route.fulfill({status:400,contentType:'text/plain',body:'Magi 연결이 끊겼습니다. 다시 선택하세요.'});if(path==='/mail/draft')result={to:'recipient@example.com',subject:'Draft',body:'Reviewed body'};
  if(path==='/api/request'&&body.method==='new'){activeSession='session-new';result={session:activeSession};}if(path==='/mail/send')result={sent:true};
  await route.fulfill({contentType:'application/json',body:JSON.stringify(result)});
 });
 await page.goto('http://127.0.0.1:8793');await page.locator('#token').fill('test-only-token');await page.locator('#load').click();await page.locator('#list button').nth(1).click();await page.waitForFunction(()=>document.getElementById('events').textContent.includes('model output'));
 assert.ok(!(await page.locator('#events').textContent()).includes('foreign output'));assert.equal(await page.evaluate(()=>window.pwned),undefined);assert.equal(await page.locator('#events strong').first().textContent(),'Magi');assert.ok(!(await page.locator('#events').textContent()).includes('data:'));assert.equal(await page.locator('#body').textContent(),'<img src=x onerror=window.pwned=true>');
 await page.locator('#chat').fill('Summarize this mail');await page.locator('#request').click();await page.waitForFunction(()=>document.getElementById('chat').value==='');
 const first=calls.find(c=>c.path==='/api/request');assert.equal(first.body.binding,'bind-user@example.com/INBOX/19/7');assert.equal(first.auth,'Bearer test-only-token');

 await page.getByRole('button',{name:'허용',exact:true}).click();await page.waitForFunction(()=>!document.querySelector('[data-call-id="approve-1"]'));assert.equal(calls.find(c=>c.body?.method==='permission').body.decision,'allow');await page.getByRole('button',{name:'Formal',exact:true}).click();await page.waitForFunction(()=>!document.querySelector('[data-call-id="question-1"]'));assert.equal(calls.find(c=>c.body?.method==='reply').body.answer,'Formal');
 await page.locator('#getDraft').click();await page.waitForFunction(()=>document.getElementById('draft').value==='Reviewed body');
 page.once('dialog',d=>d.dismiss());await page.locator('#send').click();assert.equal(calls.filter(c=>c.path==='/mail/send').length,0);
 page.once('dialog',d=>d.accept());await page.locator('#send').click();await page.waitForFunction(()=>document.getElementById('status').textContent==='발송 완료');const send=calls.find(c=>c.path==='/mail/send');assert.equal(send.body.confirm,true);assert.equal(send.body.draft.body,'Reviewed body');

 delayedDraft={};delayedDraft.promise=new Promise(resolve=>delayedDraft.resolve=resolve);await page.locator('#getDraft').click();
 await page.locator('#list button').nth(0).click();await page.waitForFunction(()=>document.getElementById('subject').textContent==='Next');assert.equal(await page.locator('#to').inputValue(),'');assert.equal(await page.locator('#draft').inputValue(),'');delayedDraft.resolve();await page.waitForTimeout(60);assert.equal(await page.locator('#draft').inputValue(),'');await page.locator('#chat').fill('Next mail');await page.locator('#request').click();await page.waitForFunction(()=>document.getElementById('chat').value==='');assert.equal(calls.filter(c=>c.path==='/api/request').at(-1).body.binding,'bind-user@example.com/INBOX/19/8');failRequest=true;await page.locator('#chat').fill('error test');await page.locator('#request').click();await page.waitForFunction(()=>document.getElementById('status').textContent.includes('다시 선택'));assert.deepEqual(errors,[]);
 failRequest=false;await page.locator('#chat').fill('preserved draft');await page.locator('#model').fill('test-model');await page.locator('#setModel').click();await page.waitForFunction(()=>document.getElementById('status').textContent==='');assert.equal(calls.filter(c=>c.body?.method==='model').at(-1).body.name,'test-model');await page.locator('#interrupt').click();await page.locator('#new').click();await page.waitForFunction(()=>document.getElementById('events').textContent.includes('model output'));assert.equal(await page.locator('#chat').inputValue(),'preserved draft');assert.ok(calls.some(c=>c.body?.method==='interrupt'));assert.ok(calls.some(c=>c.body?.method==='new'));
 console.log('PASS mail UI: untrusted content as text, session binding, new/model/stop controls, preserved input, authenticated requests, canceled and confirmed send');
}finally{await browser.close();}
