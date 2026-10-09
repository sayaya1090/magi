import http from 'node:http';
import fs from 'node:fs/promises';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
const require=createRequire(new URL('../../web/e2e/package.json', import.meta.url));
const {chromium}=require('@playwright/test');
const root = fileURLToPath(new URL('../../../', import.meta.url));
// Serve real View modules and their imports; no copied scroll implementation.
const server = http.createServer(async (req, res) => {
  const pathname = new URL(req.url, 'http://localhost').pathname;
  if (pathname === '/') {
    res.setHeader('Content-Type', 'text/html');
    res.end('<!doctype html><html><body></body></html>');
    return;
  }
  try {
    res.setHeader('Content-Type', 'text/javascript');
    res.end(await fs.readFile(root + pathname));
  } catch {
    res.statusCode = 404;
    res.end();
  }
});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const browser=await chromium.launch({headless:true});
try {
 for(const app of ['powerpoint','word','excel']) {
  const page=await browser.newPage();
  await page.goto(`http://127.0.0.1:${server.address().port}/`);
  page.setDefaultTimeout(5000);
  await page.setContent('<style>#scroll{height:300px;overflow:auto;overflow-anchor:none}#turns>div{height:100px}</style><div id="scroll"><div id="turns"></div></div>');
  await page.evaluate(async(app)=>{
   const {View}=await import(`/clients/${app}/addin/src/ui/view.js`);
   window.v=Object.create(View.prototype);
   v.keepingEnd(()=>document.querySelector('#turns').innerHTML='<div></div>'.repeat(30));
  },app);
  await page.waitForFunction(()=>v.atEnd());
  await page.evaluate(()=>{document.querySelector('#turns').lastElementChild.style.height='6000px';});
  await page.waitForFunction(()=>v.atEnd());
  await page.evaluate(()=>{document.querySelector('#scroll').scrollTop=400;});
  await page.waitForFunction(()=>v._followingEnd===false);
  await page.evaluate(()=>v.keepingEnd(()=>{document.querySelector('#turns').lastElementChild.style.height='9000px';}));
  await page.waitForTimeout(100);
  const top=await page.evaluate(()=>document.querySelector('#scroll').scrollTop);
  if(top!==400)throw Error(`${app}: reading position lost ${top}`);
  // Scroll and redraw in one event turn: the native scroll event has not fired yet.
  await page.evaluate(() => {
    const scroll = document.querySelector('#scroll');
    scroll.scrollTop = scroll.scrollHeight;
    v.keepingEnd(() => {
      document.querySelector('#turns').replaceChildren();
      document.querySelector('#turns').innerHTML = '<div></div>'.repeat(100);
    });
  });
  await page.waitForFunction(() => v.atEnd());
  await page.waitForFunction(()=>v._followingEnd===true);
  await page.evaluate(()=>document.querySelector('#scroll').style.height='150px');
  await page.waitForFunction(()=>v.atEnd());
  console.log(`PASS ${app}: tall output, delayed growth, reading position, viewport resize`);
  await page.close();
 }
} finally {await browser.close();await new Promise(r=>server.close(r));}
