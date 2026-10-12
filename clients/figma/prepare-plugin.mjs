// Copy the plugin into a local development directory with a Figma-assigned ID.
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const [id,out,...extra]=process.argv.slice(2);
if(!id||!/^\d+$/.test(id)||!out||extra.length){console.error('Usage: node clients/figma/prepare-plugin.mjs FIGMA_ASSIGNED_ID OUTPUT_DIRECTORY');process.exitCode=1;}else{
 const source=path.dirname(fileURLToPath(import.meta.url));
 const target=path.resolve(out);
 if(target===source){throw Error('Choose a separate output directory; keep the source manifest as a template.');}
 fs.mkdirSync(target,{recursive:true});
 const manifest=JSON.parse(fs.readFileSync(path.join(source,'manifest.json'),'utf8'));
 manifest.id=id;
 fs.copyFileSync(path.join(source,'code.js'),path.join(target,'code.js'));
 const material=fs.readFileSync(path.join(source,'../web/server/vendor/material.js'),'utf8').replace(/<\/script/gi,'<\\/script');
 const html=fs.readFileSync(path.join(source,'ui.html'),'utf8').replace('<!-- MAGI_MATERIAL_BUNDLE -->',()=>'<script type="module">'+material+'</script>');
 fs.writeFileSync(path.join(target,'ui.html'),html);
 fs.writeFileSync(path.join(target,'manifest.json'),JSON.stringify(manifest,null,2)+'\n');
 console.log('Prepared development plugin:',path.join(target,'manifest.json'));
}
