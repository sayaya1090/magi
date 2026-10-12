import {chromium} from '../../web/e2e/node_modules/playwright/index.mjs';
import {readFile,mkdtemp,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createInterface} from 'node:readline';
const config=JSON.parse(process.argv[2]);
const allowed=new Set(config.allowedOrigins);
const permitted=url=>{try{const u=new URL(url);return ['http:','https:'].includes(u.protocol)&&!u.username&&!u.password&&allowed.has(u.origin)}catch{return false}};
const profile=await mkdtemp(join(tmpdir(),'magi-headless-'));
let context;
process.on('SIGTERM',async()=>{await context?.close().catch(()=>{});await rm(profile,{recursive:true,force:true});process.exit(1)});
const send=value=>process.stdout.write(JSON.stringify(value)+'\n');
try {
 context=await chromium.launchPersistentContext(profile,{channel:config.channel||undefined,headless:true,serviceWorkers:'block',acceptDownloads:false});
 await context.routeWebSocket('**/*',ws=>ws.close());
 await context.route('**/*',route=>permitted(route.request().url())?route.continue():route.abort('blockedbyclient'));
 const script=await readFile(new URL('../extension/page.js',import.meta.url),'utf8');
 await context.addInitScript({content:script});
 const page=context.pages()[0]||await context.newPage();
 page.setDefaultTimeout(10000);
 context.on('page',p=>{if(p!==page)p.close().catch(()=>{})});
 if(config.initialURL){if(!permitted(config.initialURL))throw Error('Initial URL origin is not allowed');await page.goto(config.initialURL,{waitUntil:'domcontentloaded'})}
 send({ready:true});
 for await(const line of createInterface({input:process.stdin,crlfDelay:Infinity})){
  let request;
  try{
   request=JSON.parse(line);const {name,args={}}=request;
   if(name==='close')break;
   if(name==='list_tabs'){send({result:{tabs:permitted(page.url())?[{tabId:1,url:page.url(),title:await page.title()}]:[]}});continue}
   if(args.tabId!==1)throw Error('Only headless tab 1 is available');
   if(!['read_page','navigate','click','fill'].includes(name))throw Error('Unknown tool');
   if(name!=='read_page'&&!config.allowActions)throw Error('Headless actions were not enabled at startup');
   if(name==='navigate'){
    if(!permitted(args.url))throw Error('Navigation origin is not allowed');
    await page.goto(args.url,{waitUntil:'domcontentloaded'});send({result:{navigated:true,url:page.url()}});continue;
   }
   if(!permitted(page.url()))throw Error('Page origin is not allowed');
   const result=await page.evaluate(({name,args,expectedOrigin})=>window.__magiBrowserPage({name,args,expectedOrigin}),{name,args,expectedOrigin:new URL(page.url()).origin});
   send({result});
  }catch(error){send({error:String(error.message).slice(0,1000)})}
 }
}catch(error){send({error:String(error.message).slice(0,1000)});process.exitCode=1}
finally{await context?.close();await rm(profile,{recursive:true,force:true})}
