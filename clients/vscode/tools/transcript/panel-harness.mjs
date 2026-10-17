import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const Module = require('node:module');
const env = { language: 'ko' };

function planClass() {
  const original = Module._load;
  Module._load = function(id, ...args) {
    if (id === 'vscode') return { env };
    return original.call(this, id, ...args);
  };
  try { return require('../../out/ide/plan.js').Plan; }
  finally { Module._load = original; }
}

export async function checkPanel(context) {
 const Plan = planClass();
 const plan = new Plan({onChanged:()=>({dispose(){}})});
 try {
 for(const locale of ['ko','en']) {
 env.language=locale;
 const page = await context.newPage({viewport:{width:320,height:700}});
 const errors=[]; page.on('pageerror',e=>errors.push(String(e)));
 const html=plan.html({cspSource:"'self'"});
 await page.setContent(html.replace('const vs = acquireVsCodeApi();', 'window.__posted=[]; const vs = {postMessage:m=>window.__posted.push(m)};'));
 const state={kind:'plan',connection:'attached',state:'attached',session:'s1',settings:{model:'m1',profile:'p1',permission:'ask',council:false},gauge:null,
 choices:{models:['m1','m2'],profiles:['p1','p2'],sessions:[{id:'s1',title:'First'},{id:'s2',title:'Second'}]},plan:'',jobs:'',handed:'',fleet:[],cron:'',hand:''};
 await page.evaluate(m=>window.postMessage(m,'*'),state);
 await page.locator('#select-model').waitFor();
 assert.equal(await page.getByRole('combobox').count(),4);
 assert.equal(await page.locator('#select-model').inputValue(),'m1');
 await page.locator('#select-model').selectOption('m2');
 await page.locator('#select-provider').selectOption('p2');
 const toggle=page.getByRole('switch',{name:locale==='ko'?'카운슬':'Council'});
 await toggle.focus(); await page.keyboard.press('Space');
 assert.deepEqual(await page.evaluate(()=>window.__posted.slice(-3)),[
 {kind:'select',field:'model',value:'m2',session:'s1'},
 {kind:'select',field:'provider',value:'p2',session:'s1'},
 {kind:'council',session:'s1'}]);
 await page.locator('#select-model').focus();
 await page.evaluate(m=>{window.__focused=document.activeElement;window.postMessage(m,'*');},state);
 await page.evaluate(()=>new Promise(requestAnimationFrame));
 assert.equal(await page.evaluate(()=>document.activeElement===window.__focused),true);
 await page.locator('#select-model').blur();
 await page.evaluate(m=>window.postMessage(m,'*'),{...state,changing:true});
 await page.locator('#select-model:disabled').waitFor();
 for(const width of [320,840]) { await page.setViewportSize({width,height:700}); assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true); }
 assert.equal(await page.getByRole('button',{name:locale==='ko'?'새 대화':'New chat'}).getAttribute('title'),locale==='ko'?'새 대화':'New chat');
 assert.deepEqual(errors,[]);
 await page.close();
 }
 } finally { plan.dispose(); }
}
