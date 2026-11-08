import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createRequire} from 'node:module';
import {resolve,sep,extname} from 'node:path';
import {fileURLToPath} from 'node:url';
const require=createRequire(new URL('../../web/e2e/package.json',import.meta.url));
const {chromium}=require('@playwright/test');
const types={'.html':'text/html','.js':'application/javascript','.css':'text/css'};
const slots=['one','two'].map(id=>({plugin:'probe',id,target:'knowledge',title:id,actions:[{id:'ping',label:`ping ${id}`}]}));
const browser=await chromium.launch({headless:true});
try {
 for(const app of ['powerpoint','word','excel']) {
  const root=resolve(fileURLToPath(new URL(`../../${app}/addin/`,import.meta.url)));
  const page=await browser.newPage({viewport:{width:420,height:700}});
  const posted=[];
  // A token on the page is what makes main.js take the real helper path; no Office or helper is attached.
  await page.addInitScript(()=>{window.MAGI={token:'t',base:''};});
  await page.route('**/*',async route=>{
   const url=new URL(route.request().url());
   if(url.origin!=='http://magi.test'){await route.fulfill({status:200,contentType:'application/javascript',body:''});return;}
   if(url.pathname==='/api/slots'){await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify({slots})});return;}
   if(url.pathname==='/api/slots/action'){posted.push(JSON.parse(route.request().postData()));await route.fulfill({status:200,contentType:'application/json',body:'{"ok":true,"doing":"done"}'});return;}
   if(url.pathname.startsWith('/api/')||url.pathname.startsWith('/hand/')){await route.fulfill({status:200,contentType:'application/json',body:'{}'});return;}
   const file=resolve(root,'.'+decodeURIComponent(url.pathname));
   if(!file.startsWith(root+sep)){await route.fulfill({status:404,body:''});return;}
   try{await route.fulfill({status:200,contentType:types[extname(file)]??'application/octet-stream',body:await readFile(file)});}catch{await route.fulfill({status:404,body:''});}
  });
  await page.goto('http://magi.test/taskpane.html');
  await page.locator('#slots').waitFor({state:'attached'});
  await page.locator('#advanced').evaluate(e=>e.hidden=false);
  await page.locator('#slots').click();
  await page.locator('.slot-card').nth(1).waitFor({state:'visible',timeout:5000});
  await page.locator('.slot-card[data-slot-id="two"] .slot-act-btn').click();
  await page.waitForFunction(()=>document.querySelector('#slots-note')?.textContent==='done'||document.querySelectorAll('.slot-card').length===2);
  await page.waitForTimeout(200);
  assert.deepEqual(posted,[{plugin:'probe',slot:'two',action:'ping',target:'knowledge'}]);
  await page.close();console.log(`PASS ${app}: a slot's button posts that slot's id`);
 }
}finally{await browser.close()}
