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
 const page = await browser.newPage();
 await page.goto(`http://127.0.0.1:${server.address().port}/`);
 const results = await page.evaluate(async () => {
  const { ExcelHand } = await import('/clients/excel/addin/src/adapter/ExcelHand.js');
  const results = [];
  for (const [width, height, requested, expectedW, expectedH] of [
    [2400, 1200, undefined, 800, 400],
    [1200, 2400, undefined, 400, 800],
    [2400, 1200, 640, 640, 320],
    [2400, 1200, 1200, 1200, 600],
    [320, 160, undefined, 320, 160],
  ]) {
    const canvas = document.createElement('canvas');
    canvas.width = width; canvas.height = height;
    const ctx = canvas.getContext('2d');
    ctx.fillStyle = 'white'; ctx.fillRect(0, 0, width, height);
    ctx.fillStyle = 'black'; ctx.font = '32px sans-serif'; ctx.fillText('매출 2026: 123,456', 30, 60);
    const source = canvas.toDataURL('image/png').split(',')[1];
    const range = { load() {}, address: 'Sheet1!A1:D5', getImage: () => ({ value: source }) };
    const sheet = { name: 'Sheet1', load() {}, getRange: () => range };
    const context = { sync: async () => {}, workbook: { worksheets: { getActiveWorksheet: () => sheet } } };
    const hand = new ExcelHand({ run: fn => fn(context), supports: () => true });
    const out = await hand.run('render_range', { address: 'A1:D5', ...(requested ? { max_width: requested } : {}) });
    const image = new Image(); image.src = 'data:image/png;base64,' + out.result.image_base64;
    await image.decode();
    if (image.naturalWidth !== expectedW || image.naturalHeight !== expectedH) {
      throw new Error(`wrong transmitted dimensions ${image.naturalWidth}x${image.naturalHeight}`);
    }
    if (out.result.width !== expectedW || out.result.height !== expectedH) throw new Error('metadata differs');
    results.push(`${width}x${height} -> ${expectedW}x${expectedH}`);
  }
  return results;
 });
 for (const result of results) console.log('PASS actual ExcelHand render_range: ' + result);
 await page.close();
} finally { await browser.close(); await new Promise(r => server.close(r)); }
