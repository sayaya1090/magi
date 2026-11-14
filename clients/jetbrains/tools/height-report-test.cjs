// Execute the production height observer without taking desktop focus.
const fs=require('fs'),assert=require('assert'),path=require('path');
const {chromium}=require(path.resolve(__dirname,'../../web/e2e/node_modules/playwright'));
const src=fs.readFileSync(path.resolve(__dirname,'../plugin/intellij/src/main/kotlin/dev/sayaya/magi/ide/ui/RichAnswer.kt'),'utf8');
let js=src.split('val js = """')[1].split('""".trimIndent()')[0]
.replace(/^.*\$\{nodeHook.*\n/gm,'').replace(/^.*\$\{clickDiag.*\n/gm,'')
.replace(/\$\{query.inject\("([^"\n]*)"\)\}/g,(_,expr)=>'window.reports.push('+expr+');')
.replace(/\$\{surface\.(red|green|blue)\}/g,'30')
.replace(/\$\{holder.insets.left \+ holder.insets.right\}/g,'14');
(async()=>{let browser=await chromium.launch();try{let page=await browser.newPage({viewport:{width:400,height:400}});
await page.setContent('<meta name="magi-document" content="doc1"><p style="height:200px">answer</p>');
await page.evaluate(()=>{window.reports=[];window.__magiHeightRequest={generation:1,width:414,sequence:0,document:'doc1',started:performance.now()}});
await page.evaluate(js);
let samples=await page.evaluate(()=>reports.filter(s=>s.startsWith('height:')));assert(samples.some(s=>s.endsWith(':doc1:ready')));
await page.evaluate(()=>{window.reports=[];window.__magiHeightRequest={generation:2,width:999,sequence:0,document:'doc1',started:performance.now()};__magiMeasure()});assert.equal((await page.evaluate(()=>reports.filter(s=>s.startsWith('height:')))).length,0);
await page.evaluate(()=>{window.__magiHeightRequest={generation:3,width:414,sequence:0,document:'doc2',started:performance.now()};__magiMeasure()});assert.equal((await page.evaluate(()=>reports.filter(s=>s.startsWith('height:')))).length,0);
await page.evaluate(()=>{document.querySelector('meta').content='doc2';let f=document.createElement('figure');f.className='magi-diagram';document.body.append(f);__magiMeasure()});
samples=await page.evaluate(()=>reports.filter(s=>s.startsWith('height:')));assert(samples.at(-1).endsWith(':pending'));
await page.evaluate(()=>{document.querySelector('figure').innerHTML='<svg role="img"></svg>';__magiMeasure()});samples=await page.evaluate(()=>reports.filter(s=>s.startsWith('height:')));assert(samples.at(-1).endsWith(':ready'));console.log('Production height script: ready, stale width/document rejection, pending Mermaid and completion passed');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exit(1)});
