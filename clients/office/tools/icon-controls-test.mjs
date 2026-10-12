import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createRequire} from 'node:module';
const require=createRequire(new URL('../../web/e2e/package.json',import.meta.url));
const {chromium}=require('@playwright/test');
const browser=await chromium.launch({headless:true});
try {
 for(const app of ['powerpoint','word','excel']) {
  const page=await browser.newPage({viewport:{width:320,height:600}});
  const root=new URL(`../../${app}/addin/`,import.meta.url);
  const html=await readFile(new URL('taskpane.html',root),'utf8');
  const start=html.indexOf('<div id="advanced"');const end=html.indexOf('<!-- 파괴적',start);
  await page.setContent('<section id="pane">'+html.slice(start,end)+'</section>');
  await page.addStyleTag({path:new URL('taskpane.css',root).pathname});
  await page.locator('#advanced').evaluate(e=>e.hidden=false);
  const source=await readFile(new URL('src/ui/iconControls.js',root),'utf8');
  await page.addScriptTag({content:source.replace('export function ','function ')});
  await page.evaluate(()=>mountIconControls());
  assert.deepEqual(await page.locator('.control-group').evaluateAll(groups=>groups.map(g=>[...g.children].map(b=>b.id))),[['fresh'],['rules','guides'],['council','repick']]);
  const button=page.locator('#fresh'); const tip=page.getByRole('tooltip');
  const title=await button.getAttribute('title');
  await button.hover();await tip.waitFor({state:'visible'});
  assert.equal(await tip.innerText(),title);
  assert.equal(await button.getAttribute('title'),null);
  assert.ok((await button.getAttribute('aria-describedby')).includes('icon-control-tooltip'));
  const box=await tip.boundingBox();assert.ok(box.x>=0 && box.x+box.width<=320 && box.y>=0 && box.y+box.height<=600);
  await tip.hover();assert.equal(await tip.isVisible(),true);
  await page.keyboard.press('Escape');assert.equal(await tip.isVisible(),false);assert.equal(await button.getAttribute('title'),title);
  await page.mouse.move(310,590);await button.focus();await tip.waitFor({state:'visible'});
  await button.evaluate(e=>{e.title='바뀐 상태 설명';e.setAttribute('aria-label','바뀐 상태 설명')});
  await page.waitForFunction(()=>document.getElementById('icon-control-tooltip').textContent==='바뀐 상태 설명');
  await page.keyboard.press('Tab');await tip.waitFor({state:'visible'});
  assert.equal(await tip.innerText(),await page.evaluate(()=>document.activeElement.getAttribute('aria-label')));
  await page.evaluate(()=>{document.activeElement.blur();const b=document.createElement('button');b.id='late-icon';b.className='icon-btn';b.title='나중에 생긴 기능';b.style.position='fixed';b.style.bottom='2px';b.style.right='2px';document.body.append(b)});
  await page.locator('#late-icon').hover();await tip.waitFor({state:'visible'});assert.equal(await tip.innerText(),'나중에 생긴 기능');
  const edge=await tip.boundingBox();assert.ok(edge.x>=0 && edge.x+edge.width<=320 && edge.y+edge.height<=600);
  await page.locator('#late-icon').click();assert.equal(await tip.isVisible(),false);
  await page.mouse.move(0,0);await page.locator('#late-icon').hover();await tip.waitFor({state:'visible'});
  await page.setViewportSize({width:420,height:700});await tip.waitFor({state:'hidden'});
  const profiles=await readFile(new URL('src/ui/providerProfiles.js',root),'utf8');
  await page.addScriptTag({content:profiles.replace('export function ','function ')});
  await page.evaluate(()=>mountProviderProfiles({api:{providerProfiles:async()=>({profiles:[]})}}));
  await page.mouse.move(0,0);await page.locator('#manage-providers').hover();await tip.waitFor({state:'visible'});assert.equal(await tip.innerText(),'프로바이더 관리');
  await page.locator('#manage-providers').click();await page.locator('#provider-profiles-dialog').waitFor({state:'visible'});assert.equal(await tip.isVisible(),false);
  await page.close();console.log(`PASS ${app}: grouped controls, immediate hover/focus tooltip, dynamic labels/buttons, hoverable and dismissible, viewport edges`);
 }
}finally{await browser.close()}
