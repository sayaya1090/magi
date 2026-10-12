import fs from 'node:fs';
import assert from 'node:assert/strict';
import os from 'node:os';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
export function preparedHTML(){ const prepared=fs.mkdtempSync(path.join(os.tmpdir(),'magi-figma-material-'));
 let html;try{execFileSync(process.execPath,[fileURLToPath(new URL('../prepare-plugin.mjs',import.meta.url)),'123',prepared]);html=fs.readFileSync(path.join(prepared,'ui.html'),'utf8');}finally{fs.rmSync(prepared,{recursive:true,force:true});}
return html;}
export async function verifySettings(browser) {
 const page=await browser.newPage({viewport:{width:320,height:640},colorScheme:'light'});page.setDefaultTimeout(5000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));const calls=[];let releaseResume;let holdResume=false;let deliverOldTool=false;let oldToolDelivered=false;let sid='session-a';let profiles=[{name:'saved',baseUrl:'https://example.com/v1',model:'m1',hasKey:true}];let guide={name:'style',body:'Use concise labels.',enabled:true};
 await page.route('http://localhost:8734/**',async route=>{const r=route.request();if(r.method()==='OPTIONS')return route.fulfill({status:204,headers:{'Access-Control-Allow-Origin':'*','Access-Control-Allow-Headers':'*','Access-Control-Allow-Methods':'GET, POST'}});const u=new URL(r.url());const data=r.postData()?JSON.parse(r.postData()):{};calls.push({path:u.pathname,data,query:u.search,auth:r.headers().authorization});let out={};
 if(u.pathname==='/api/connect')out={binding:'binding-a',session:sid};
 else if(u.pathname==='/api/request'){if(data.method==='sessions')out={sessions:[{id:'session-a',title:'First'},{id:'session-b',title:'Second'}]};else if(data.method==='resume'){if(holdResume)await new Promise(resolve=>releaseResume=resolve);sid=data.name;out={session:sid};}else if(data.method==='profiles')out={profiles:[{name:'saved',tier:'llm'}]};else out={ok:true};}
 else if(u.pathname==='/api/provider-profiles'){if(r.method()==='POST'){profiles=data.op==='delete'?[]:[{name:data.name,baseUrl:data.baseUrl,model:data.model,hasKey:!data.clearKey}];}out={profiles};}
 else if(u.pathname==='/api/instructions')out={text:'Keep document edits scoped.'};
 else if(u.pathname==='/api/guides')out={guides:[{...guide,description:'Style'}]};
 else if(u.pathname==='/api/guide'){if(r.method()==='POST'){guide={...guide,name:data.name,body:data.body??guide.body,enabled:data.op==='disable'?false:true};}out=guide;}
 else if(u.pathname==='/api/council')out={enabled:false};
 else if(u.pathname.endsWith('/events')){await new Promise(r=>setTimeout(r,30));out={events:[]};}else if(u.pathname.endsWith('/poll')){await new Promise(r=>setTimeout(r,30));if(deliverOldTool){deliverOldTool=false;oldToolDelivered=true;out={callId:'old-tool',name:'figma_set_text',session:'session-a',args:{id:'text:1',text:'stale'}};}}
 await route.fulfill({contentType:'application/json',headers:{'Access-Control-Allow-Origin':'*'},body:JSON.stringify(out)});
 });
 const html=preparedHTML();
 await page.setContent(html); await page.waitForFunction(()=>!!customElements.get('md-filled-button'));
 await page.evaluate(()=>window.postMessage({pluginMessage:{kind:'context',fileKey:'file-a',pageId:'page-a',selection:[],tools:[]}},'*'));
 await page.locator('#token').fill('local-test-token');await page.locator('#connect').click();await page.waitForFunction(()=>!document.getElementById('settings').disabled);await page.waitForFunction(()=>document.getElementById('sessions').options.length===3);
 await page.evaluate(()=>{window.sessionMessages=[];window.toolMessages=[];window.addEventListener('message',e=>{if(e.data?.pluginMessage?.kind==='tool')window.toolMessages.push(e.data.pluginMessage);if(e.data?.pluginMessage?.kind==='session')window.sessionMessages.push(e.data.pluginMessage.session);});});holdResume=true;await page.locator('#sessions').selectOption('session-b');await page.waitForFunction(()=>window.sessionMessages.includes(null));assert.ok(releaseResume);deliverOldTool=true;for(let i=0;i<100&&!oldToolDelivered;i++)await new Promise(resolve=>setTimeout(resolve,10));assert.ok(oldToolDelivered);await page.waitForTimeout(50);assert.equal(await page.evaluate(()=>window.toolMessages.some(m=>m.callId==='old-tool')),false);releaseResume();await page.waitForFunction(()=>document.getElementById('status').textContent.includes('session-b'));assert.ok(calls.some(x=>x.data.method==='resume'&&x.data.name==='session-b'));
 await page.locator('#settings').click();await page.waitForFunction(()=>!document.getElementById('profile-save').disabled&& !document.getElementById('rules-save').disabled);
 assert.deepEqual(await page.locator('#provider-select option').evaluateAll(nodes=>nodes.map(n=>n.value)),['','saved']);await page.locator('#profile-list').selectOption('saved');assert.equal(await page.locator('#profile-key').inputValue(),'');assert.equal(await page.locator('#profile-key').getAttribute('type'),'password');await page.locator('#profile-save').click();await page.waitForFunction(()=>document.getElementById('settings-status').textContent.includes('저장했습니다'));
 assert.equal(calls.find(x=>x.path==='/api/provider-profiles'&&x.data.op==='save').data.apiKey,'');
 assert.equal(await page.locator('#settings-panel').isVisible(),false);await page.locator('#settings').click();await page.waitForFunction(()=>!document.getElementById('profile-save').disabled);await page.locator('#profile-list').selectOption('saved');
 await page.locator('#profile-clear-key').check();await page.locator('#profile-save').click();await page.waitForFunction(()=>!document.getElementById('profile-save').disabled);assert.ok(calls.some(x=>x.data.clearKey===true));await page.locator('#settings').click();await page.waitForFunction(()=>!document.getElementById('rules-save').disabled);
 await page.locator('#rules-text').fill('New rule');await page.locator('#rules-save').click();await page.waitForFunction(()=>document.getElementById('settings-status').textContent.includes('다음 요청'));assert.ok(calls.some(x=>x.path==='/api/instructions'&&x.data.text==='New rule'));
 await page.locator('#guide-list').selectOption('style');await page.waitForFunction(()=>document.getElementById('guide-body').value==='Use concise labels.');await page.locator('#guide-enabled').uncheck();await page.waitForFunction(()=>document.getElementById('settings-status').textContent.includes('사용 설정'));assert.ok(calls.some(x=>x.path==='/api/guide'&&x.data.op==='disable'));
 await page.locator('#guide-body').fill('Updated guide');await page.locator('#guide-save').click();await page.waitForFunction(()=>document.getElementById('settings-status').textContent.includes('가이드를 저장'));assert.ok(calls.some(x=>x.data.body==='Updated guide'));
 await page.locator('#council').check();await page.waitForFunction(()=>!document.getElementById('council').disabled);assert.ok(calls.some(x=>x.path==='/api/council'&&x.data.enabled===true));
 await page.locator('#provider-select').selectOption('saved');await page.waitForFunction(()=>document.getElementById('settings-status').textContent.includes('프로바이더를 변경'));assert.ok(calls.some(x=>x.data.method==='use-profile'&&x.data.name==='saved'));
 await page.locator('#model').fill('m2');await page.locator('#set-model').click();await page.waitForFunction(()=>document.getElementById('settings-status').textContent.includes('모델을 변경'));assert.ok(calls.some(x=>x.data.method==='model'&&x.data.name==='m2'));
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));const light=await page.evaluate(()=>getComputedStyle(document.body).backgroundColor);await page.emulateMedia({colorScheme:'dark'});const dark=await page.evaluate(()=>getComputedStyle(document.body).backgroundColor);assert.notEqual(light,dark);assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await page.locator('#profile-key').fill('never-retain');await page.keyboard.press('Escape');assert.equal(await page.locator('#profile-key').inputValue(),'');
 await page.locator('#interrupt').click();await page.locator('#compact').click();await page.waitForFunction(()=>!document.getElementById('compact').disabled);assert.ok(calls.some(x=>x.data.method==='interrupt'));assert.ok(calls.some(x=>x.data.method==='compact'));
 await page.evaluate(()=>window.postMessage({pluginMessage:{kind:'window-state',mode:'window'}},'*'));await page.waitForFunction(()=>document.getElementById('window-mode').textContent==='왼쪽 패널');
 await page.locator('#settings').click();page.once('dialog',d=>d.accept());await page.locator('#restart').click();await page.waitForFunction(()=>document.getElementById('status').textContent.includes('연결됨'));assert.ok(calls.some(x=>x.data.method==='restart'));
 assert.ok(calls.filter(x=>x.path!='/api/connect').every(x=>x.auth==='Bearer local-test-token'));assert.deepEqual(errors,[]);
 await page.close();console.log('PASS Figma Material UI: 320px bounds, sessions, profiles/key preservation, rules, guide enable/save, model/profile switching, council, compact/stop, restart/reconnect and window mode');
}
