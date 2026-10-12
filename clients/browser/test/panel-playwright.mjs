import {createRequire} from 'node:module';import fs from 'node:fs';import assert from 'node:assert/strict';
const require=createRequire(new URL('../../web/e2e/package.json',import.meta.url));const {chromium}=require('@playwright/test');
const browser=await chromium.launch({headless:true});
try{
 const targets=[];
 for(const windowId of [1,2]){
 const page=await browser.newPage({viewport:{width:320,height:800}});page.setDefaultTimeout(5000);const errors=[];page.on('pageerror',e=>errors.push(e.message));const calls=[];
 await page.route('http://127.0.0.1:8737/**',async route=>{const r=route.request(),u=new URL(r.url()),d=r.postData()?JSON.parse(r.postData()):{};calls.push({path:u.pathname,...d});let out={ok:true};
 if(r.method()==='OPTIONS')return route.fulfill({status:204,headers:{'Access-Control-Allow-Origin':'*','Access-Control-Allow-Headers':'*'}});
 if(u.pathname==='/api/connect'){targets.push(d.target);out={binding:'b',session:'a'};}
 else if(u.pathname==='/api/request'){if(d.method==='sessions')out={sessions:[{id:'a'},{id:'b'}]};if(d.method==='resume')out={session:d.name};if(d.method==='status')out={model:'m',profile:'saved'};if(d.method==='models')out={models:['m']};}
 else if(u.pathname==='/api/provider-profiles')out={profiles:[{name:'saved',baseUrl:'https://example.com/v1',model:'m',hasKey:true}],note:'저장했습니다.'};
 else if(u.pathname==='/api/instructions')out={text:'rule'};
 else if(u.pathname==='/api/guides')out={guides:[{name:'style',enabled:true}]};
 else if(u.pathname==='/api/guide')out={name:'style',body:'style body',enabled:true};
 else if(u.pathname==='/api/council')out={enabled:false};
 else if(u.pathname.endsWith('/events')){await new Promise(r=>setTimeout(r,40));out={events:[]};}
 else if(u.pathname.endsWith('/poll')){await new Promise(r=>setTimeout(r,40));out={};}
 await route.fulfill({contentType:'application/json',headers:{'Access-Control-Allow-Origin':'*'},body:JSON.stringify(out)});
 });
 await page.route('https://panel.test/**',r=>r.fulfill({body:'<html></html>'}));await page.goto('https://panel.test');await page.setContent(fs.readFileSync(new URL('../extension/panel.html',import.meta.url),'utf8').replace(/<script.*?<\/script>/gs,''));
 await page.evaluate(id=>{const store={};window.chrome={windows:{getCurrent:async()=>({id})},storage:{session:{get:async k=>({[k]:store[k]}),set:async o=>Object.assign(store,o)}},tabs:{query:async()=>[{id:window.testTab||11,url:'https://example.com/a',title:'One'}],get:async id=>({id,url:'https://example.com/a',title:'One'}),onRemoved:{addListener(){}}},permissions:{request:async()=>true}};},windowId);
 await page.addScriptTag({path:new URL('../extension/material.js',import.meta.url).pathname});await page.addScriptTag({path:new URL('../extension/browser.js',import.meta.url).pathname});await page.addScriptTag({path:new URL('../extension/panel.js',import.meta.url).pathname});await page.addStyleTag({path:new URL('../extension/panel.css',import.meta.url).pathname});
 await page.locator('#token').fill('test');await page.locator('#connect').click();await page.waitForFunction(()=>!document.getElementById('settings').disabled);
 await page.locator('#grant').click();await page.evaluate(()=>window.testTab=12);await page.locator('#grant').click();assert.equal((await page.evaluate(()=>browserTools.execute({name:'list_tabs',args:{}}))).tabs.length,2);assert.match(await page.locator('#granted').textContent(),/One/);
 await page.waitForFunction(()=>document.getElementById('sessions').options.length===3);await page.locator('#sessions').selectOption('b');await page.waitForFunction(()=>document.getElementById('status').textContent.includes('세션 b'));
 await page.locator('#settings').click();await page.waitForFunction(()=>!document.getElementById('rules-save').disabled);await page.locator('#rules-text').fill('updated');await page.locator('#rules-save').click();await page.waitForFunction(()=>document.getElementById('settings-status').textContent.includes('저장'));
 await page.locator('#guide-list').selectOption('style');await page.waitForFunction(()=>document.getElementById('guide-body').value==='style body');await page.locator('#guide-enabled').uncheck();const councilBefore=calls.filter(x=>x.path==='/api/council'&&x.enabled===true).length;page.once('dialog',d=>d.dismiss());await page.locator('#council').click();await page.waitForFunction(()=>!document.getElementById('council').checked);assert.equal(calls.filter(x=>x.path==='/api/council'&&x.enabled===true).length,councilBefore);page.once('dialog',d=>d.accept());await page.locator('#council').check();await page.locator('#provider-select').selectOption('saved');await page.locator('#model').fill('new-model');await page.locator('#set-model').click();
 await page.locator('#profile-list').selectOption('saved');await page.locator('#profile-save').click();await page.waitForFunction(()=>document.getElementById('settings-panel').hidden);
 assert.ok(calls.some(x=>x.method==='resume'));assert.ok(calls.some(x=>x.path==='/api/instructions'&&x.text==='updated'));assert.ok(calls.some(x=>x.op==='disable'));assert.ok(calls.some(x=>x.path==='/api/council'&&x.enabled===true));assert.ok(calls.some(x=>x.method==='use-profile'));assert.ok(calls.some(x=>x.method==='model'));assert.equal(await page.locator('#profile-key').inputValue(),'');assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));assert.deepEqual(errors,[]);await page.close();
 }
 assert.equal(new Set(targets).size,2);console.log('PASS actual Chrome panel: separate windows, shared-tab session, Material UI, sessions, providers, model, rules, skills, council, 320px bounds');
}finally{await browser.close()}
