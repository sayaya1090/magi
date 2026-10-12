import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createRequire} from 'node:module';
import {resolve,sep,extname} from 'node:path';
import {fileURLToPath} from 'node:url';
const require=createRequire(new URL('../../web/e2e/package.json',import.meta.url));
const {chromium}=require('@playwright/test');
const browser=await chromium.launch({headless:true});
try {
 for(const app of ['powerpoint','word','excel']) {
  const root=resolve(fileURLToPath(new URL(`../../${app}/addin/`,import.meta.url)));
  const page=await browser.newPage({viewport:{width:420,height:700}});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.route('**/*',async route=>{
   const url=new URL(route.request().url());
   if(url.origin!=='http://magi.test'){await route.fulfill({status:200,contentType:'application/javascript',body:''});return;}
   const file=resolve(root,'.'+decodeURIComponent(url.pathname));
   if(!file.startsWith(root+sep)){await route.fulfill({status:404,body:''});return;}
   try{await route.fulfill({status:200,contentType:({'.html':'text/html','.js':'application/javascript','.css':'text/css','.svg':'image/svg+xml'})[extname(file)]||'application/octet-stream',body:await readFile(file)});}catch{await route.fulfill({status:404,body:''});}
  });
  await page.goto('http://magi.test/taskpane.html');
  await page.locator('.advanced-tools').waitFor({state:'attached'});
  // This is the isolated browser mock entry point; no Office token or live document is attached.
  await page.locator('#advanced').evaluate(e=>e.hidden=false);
  await page.locator('#fresh').hover();await page.getByRole('tooltip').waitFor({state:'visible'});
  assert.ok((await page.getByRole('tooltip').innerText()).includes('새 대화'));
  assert.equal(await page.locator('.control-group').count(),3);
  await page.waitForTimeout(1800);
  assert.deepEqual(errors,[]);
  await page.locator('#advanced').evaluate(e=>e.hidden=false);
  await page.locator('#council').hover();await page.getByRole('tooltip').waitFor({state:'visible'});
  assert.ok((await page.getByRole('tooltip').innerText()).includes('카운슬'));
  await page.screenshot({path:`/tmp/magi-${app}-icon-controls.png`});
  await page.close();console.log(`PASS ${app}: full main.js imports and mounts grouped controls and hover descriptions, zero page errors`);
 }
}finally{await browser.close()}
