import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createRequire} from 'node:module';
const require=createRequire(new URL('../../web/e2e/package.json',import.meta.url));
const {chromium}=require('@playwright/test');
const browser=await chromium.launch({headless:true});
try {
 for(const app of ['powerpoint','word','excel']) {
  const root=new URL(`../../${app}/addin/`,import.meta.url);
  const html=await readFile(new URL('taskpane.html',root),'utf8');
  const start=html.indexOf('<div id="advanced"');
  const end=html.indexOf('<!-- 파괴적',start);
  const source=await readFile(new URL('src/ui/providerProfiles.js',root),'utf8');
  for(const width of [320,420,600]) {
   const page=await browser.newPage({viewport:{width,height:600}});
   await page.setContent('<section id="pane">'+html.slice(start,end)+'</section>');
   await page.addStyleTag({path:new URL('taskpane.css',root).pathname});
   await page.locator('#advanced').evaluate(e=>e.hidden=false);
   await page.addScriptTag({content:source.replace('export function ','function ')});
   await page.evaluate(()=>mountProviderProfiles({api:{providerProfiles:async()=>({profiles:[]})}}));
   const layout=await page.evaluate(()=>{
    const rect=id=>{const r=document.getElementById(id).getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height,right:r.right}};
    return {manage:rect('manage-providers'),provider:rect('provider'),model:rect('model'),advanced:rect('advanced'),icons:['fresh','rules','guides','council','repick'].map(rect),align:getComputedStyle(document.getElementById('advanced')).justifyContent,overflow:document.documentElement.scrollWidth>innerWidth};
   });
   assert.equal(layout.align,'center'); assert.equal(layout.overflow,false);
   assert.equal(layout.manage.height,layout.provider.height); assert.equal(layout.manage.height,40);
   if(width===600) assert.equal(layout.manage.y,layout.provider.y,'management unnecessarily wrapped');
   for(let i=1;i<layout.icons.length;i++) if(layout.icons[i].y===layout.icons[i-1].y) assert.ok(Math.abs(layout.icons[i].x-layout.icons[i-1].right-16)<1,'icon gap expanded');
   await page.locator('#manage-providers').click();
   for(const id of ['profile-name','profile-endpoint','profile-model','profile-key']) {
    assert.equal(await page.locator('#'+id).getAttribute('autocapitalize'),'off');
    assert.equal(await page.locator('#'+id).getAttribute('autocorrect'),'off');
   }
   await page.locator('#profile-name').fill('lowercase'); assert.equal(await page.locator('#profile-name').inputValue(),'lowercase');
   await page.keyboard.press('Escape');
   if(app==='powerpoint' && width===600) await page.screenshot({path:'/tmp/magi-office-toolbar.png'});
   await page.close();
  }
  console.log(`PASS ${app}: centered 16px icons, management fits at 600px, matching 40px height, no overflow at 320/420/600, input capitalization disabled`);
 }
}finally{await browser.close()}
