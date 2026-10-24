/* Figma main sandbox. No credentials or HTTP requests belong here. */
(function () {
  'use strict';
  const LIMIT = 100;
  let explicitDocumentId=null,activeSession=null;
  const file = () => figma.fileKey || explicitDocumentId;
  const describe = n => ({ id: n.id, name: n.name, type: n.type,
    ...(typeof n.characters === 'string' ? { text: n.characters.slice(0, 4000), truncated: n.characters.length > 4000 } : {}),
    ...(typeof n.width === 'number' ? { width: n.width, height: n.height } : {}) });
  const onPage = n => { let p=n; while (p && p.type !== 'PAGE') p=p.parent; return p && p.id === figma.currentPage.id; };
  const tools = [
    {name:'figma_selection',description:'Read selected nodes on the current page; at most 100 nodes, text capped at 4000 characters per node.',parameters:{type:'object',properties:{},additionalProperties:false}},
    {name:'figma_page',description:'Read current page immediate children (at most 100). No recursive full-file scan.',parameters:{type:'object',properties:{},additionalProperties:false}},
    {name:'figma_set_text',description:'Replace text of one explicit text node in this bound Figma file. Loads fonts first.',parameters:{type:'object',properties:{id:{type:'string'},text:{type:'string',maxLength:20000}},required:['id','text'],additionalProperties:false}},
    {name:'figma_create_frame',description:'Create a frame on the current page. Dimensions 1–4096.',parameters:{type:'object',properties:{name:{type:'string'},width:{type:'number'},height:{type:'number'}},required:['name','width','height'],additionalProperties:false}}
  ];
  for(const t of tools)t.parameters.properties.branch={type:'string',description:'Host-issued checkpoint branch ID; when supplied, reads and writes stay in that page copy.'};
  tools.push({name:'figma_checkpoint',description:'Create isolated page checkpoints. begin, fork, adopt a branch, status, reopen, or finish to request publication after verified completion. Does not edit shared components or libraries.',parameters:{type:'object',properties:{action:{type:'string',enum:['begin','fork','adopt','status','reopen','finish']},branch:{type:'string'}},required:['action'],additionalProperties:false}});
  function context() { figma.ui.postMessage({kind:'context',fileKey:file(),pageId:figma.currentPage.id,selection:figma.currentPage.selection.slice(0,LIMIT).map(describe),tools}); }
  // Checkpoint pages are host-managed copies. Only these two mutation tools are
  // supported; no component/library/file-wide writes are offered in a branch.
  const checkpointTrees=new Map();
  const branchProps=['name','x','y','width','height','visible','locked','opacity','rotation','characters','fontName','fontSize','fills','strokes','strokeWeight','effects','layoutMode','primaryAxisSizingMode','counterAxisSizingMode','itemSpacing','paddingTop','paddingRight','paddingBottom','paddingLeft','constraints','textAlignHorizontal','textAlignVertical','textAutoResize','lineHeight','letterSpacing','fillStyleId','textStyleId','componentPropertyReferences'];
  function scan(page) {
    const nodes=[];
    function walk(n){if(nodes.length>=2000)throw Error('Checkpoint pages are limited to 2000 nodes.');nodes.push(n);for(const c of n.children||[])walk(c);}
    walk(page);return nodes;
  }
  function fingerprint(page) {
    return JSON.stringify(scan(page).map(n=>{const props={id:n.id,type:n.type,parent:n.parent?.id};for(const k of branchProps){try{const v=n[k];if(v!==undefined)props[k]=typeof v==='symbol'?'mixed':v;}catch(e){throw Error('Cannot checkpoint '+k+': '+String(e));}}if(n.type==='TEXT'&&typeof n.getStyledTextSegments==='function')props.styledText=n.getStyledTextSegments(['fontName','fontSize','fills','textStyleId','textCase','textDecoration','letterSpacing','lineHeight','listOptions','indentation']);return props;}));
  }
  function clonePage(page,label,keys) {
    scan(page);const copy=page.clone();copy.name=label;
    const from=scan(page),to=scan(copy),mapped=new Map();
    if(from.length!==to.length){copy.remove();throw Error('Page copy did not preserve the checkpoint structure.');}
    for(let i=0;i<from.length;i++)mapped.set(to[i].id,keys?.get(from[i].id)||from[i].id);
    return {page:copy,keys:mapped,stamp:fingerprint(copy)};
  }
  function treeFor(){const t=checkpointTrees.get(activeSession);if(!t||t.file!==file())throw Error('Begin a checkpoint in this file and session first.');return t;}
  function resolveBranch(id,write=true){const t=treeFor(),b=t.branches.get(id);if(!b||b.page.removed||t.published||(write&&(b.sealed||b.base!==t.current.page.id||t.ready)))throw Error('This branch is missing, sealed, stale or belongs to another session.');if(fingerprint(b.page)!==b.stamp)throw Error('The branch changed outside its tracked tools. Fork again.');return {t,b};}
  async function fontsFor(n){if(n.hasMissingFont)throw Error('Resolve missing fonts before editing.');const fonts=n.fontName===figma.mixed?n.getRangeAllFontNames(0,n.characters.length):[n.fontName];for(const font of new Map(fonts.map(f=>[JSON.stringify(f),f])).values())await figma.loadFontAsync(font);}
  async function cleanupTree(t){
    for(const item of [...t.branches.values(),...t.snapshots])if(!item.page.removed)item.page.remove();
    t.branches.clear();t.snapshots=[];t.phase='cleaned';
  }
  function checkpointStatus(t){return {phase:t.phase,checkpoint:t.current.page.id,ready:t.ready,branches:[...t.branches.values()].map(b=>({branch:b.id,page:b.page.id,checkpoint:b.base,sealed:b.sealed})),history:t.history,limitations:'Page-local text/frame operations only; shared fonts and libraries are not copied.'};}
  async function checkpointAction(m){
    const a=m.args;
    if(a.action==='begin'){
      let t=checkpointTrees.get(activeSession);
      if(!t||t.phase==='cleaned'){
        const original=figma.currentPage,baseline=fingerprint(original),current=clonePage(original,'[Magi checkpoint] '+original.name);
        t={session:activeSession,file:file(),original,baseline,current,branches:new Map(),snapshots:[current],history:[],ready:false,published:false,phase:'open',originalKeys:new Map(scan(original).map(n=>[n.id,n.id]))};checkpointTrees.set(activeSession,t);
      }
      return checkpointStatus(t);
    }
    const t=treeFor();
    if(a.action==='status')return checkpointStatus(t);
    if(a.action==='fork'){
      if(t.ready||t.published||t.branches.size>=10)throw Error('The checkpoint is not open, or already has ten branches.');
      if(fingerprint(t.current.page)!==t.current.stamp)throw Error('The checkpoint snapshot changed outside its tracked tools.');
      const copy=clonePage(t.current.page,'[Magi branch] '+m.callId,t.current.keys),b={...copy,id:m.callId,base:t.current.page.id,ops:JSON.parse(JSON.stringify(t.current.ops||[])),sealed:false};t.branches.set(b.id,b);return {branch:b.id,page:b.page.id,checkpoint:b.base};
    }
    if(a.action==='adopt'){
      const {b}=resolveBranch(a.branch),previous=t.current.page.id;
      const next=clonePage(b.page,'[Magi checkpoint] '+m.callId,b.keys);next.ops=JSON.parse(JSON.stringify(b.ops));t.current=next;t.snapshots.push(next);b.sealed=true;t.history.push({branch:b.id,from:previous,to:next.page.id});return checkpointStatus(t);
    }
    if(a.action==='reopen'){if(t.published)throw Error('Publication already happened.');t.ready=false;t.token=null;return checkpointStatus(t);}
    if(a.action==='finish'){
      if(!t.history.length)throw Error('Adopt an artifact before finishing.');
      t.ready=true;t.token=m.callId;return {...checkpointStatus(t),checkpoint_ready:true,checkpoint_token:m.callId};
    }
    throw Error('Unsupported checkpoint action.');
  }
  // This entry is absent from the model tool catalogue. The authenticated bridge
  // invokes it only after a new, verified completion beyond the finish request.
  async function finalizeCheckpoint(m){
    const t=treeFor();if(!t.ready||m.args.token!==t.token)throw Error('No matching completion reservation.');
    if(t.published){await cleanupTree(t);await persistTree(t);return checkpointStatus(t);}
    if(t.original.removed||fingerprint(t.original)!==t.baseline)throw Error('The original page changed; checkpoint pages were retained.');
    if(fingerprint(t.current.page)!==t.current.stamp)throw Error('The selected checkpoint changed; nothing was published.');
    const ops=t.current.ops||[],nodes=new Map(scan(t.original).map(n=>[n.id,n]));
    for(const op of ops)if(op.kind==='text'&&!op.key.startsWith('new:')){const n=nodes.get(op.key);if(!n||n.type!=='TEXT')throw Error('An original text node is missing.');await fontsFor(n);}
    if(activeSession!==t.session||file()!==t.file||fingerprint(t.original)!==t.baseline)throw Error('The session or original changed while preparing publication.');
    const previous=figma.currentPage,undo=[],created=[];
    try {
      await figma.setCurrentPageAsync(t.original);
      if(activeSession!==t.session||fingerprint(t.original)!==t.baseline)throw Error('The original changed before publication.');
      for(const op of ops){
        if(op.kind==='text'){const n=nodes.get(op.key);undo.push([n,n.characters]);n.characters=op.text;if(n.characters!==op.text)throw Error('Text publication did not match the selected result.');}
        else if(op.kind==='frame'){const n=figma.createFrame();created.push(n);n.name=op.name;n.resize(op.width,op.height);nodes.set(op.key,n);if(n.name!==op.name||n.width!==op.width||n.height!==op.height)throw Error('Frame publication did not match the selected result.');}
        else throw Error('Unrecognized checkpoint operation.');
      }
      t.published=true;t.phase='cleanup_pending';try{await persistTree(t);}catch(e){t.published=false;t.phase='open';throw e;}
      await cleanupTree(t);await persistTree(t);return checkpointStatus(t);
    } catch(e){
      if(!t.published){const failures=[];for(const n of created)try{n.remove();}catch(x){failures.push(String(x));}for(const [n,text] of undo.reverse())try{n.characters=text;}catch(x){failures.push(String(x));}if(failures.length)throw Error(String(e)+'; rollback requires review: '+failures.join('; '));}
      throw e;
    } finally {if(previous!==t.original&&!previous.removed)await figma.setCurrentPageAsync(previous);}
  }
  const storageKey=session=>'magi-checkpoint:'+file()+':'+session;
  let checkpointLoaded=Promise.resolve(),checkpointLoadError=null;
  const loadedSessions=new Set();
  async function persistTree(t){
    if(!figma.clientStorage)throw Error('Persistent checkpoint storage is unavailable.');
    const pack=item=>({page:item.page.id,keys:[...item.keys],stamp:item.stamp,ops:item.ops||[],id:item.id,base:item.base,sealed:item.sealed});
    const data={session:t.session,file:t.file,original:t.original.id,baseline:t.baseline,current:t.current.page.id,snapshots:t.snapshots.map(pack),branches:[...t.branches.values()].map(pack),history:t.history,ready:t.ready,token:t.token,published:t.published,phase:t.phase};
    const key='magi-checkpoint:'+t.file+':'+t.session;
    if(t.phase==='cleaned'){await figma.clientStorage.setAsync(key+':last',{history:t.history,phase:'cleaned'});await figma.clientStorage.deleteAsync(key);}
    else await figma.clientStorage.setAsync(key,data);
  }
  async function restoreTree(session){
    if(!session||!figma.clientStorage||checkpointTrees.has(session)||loadedSessions.has(session))return;
    const data=await figma.clientStorage.getAsync(storageKey(session));
    if(data){
      if(data.session!==session||data.file!==file()||!Array.isArray(data.snapshots)||!Array.isArray(data.branches))throw Error('Checkpoint recovery metadata is invalid. Copies were retained.');
      const original=await figma.getNodeByIdAsync(data.original);
      if(!original||original.type!=='PAGE')throw Error('The original checkpoint page is missing. Review retained page copies.');
      const unpack=async item=>{const page=await figma.getNodeByIdAsync(item.page);if((!page||page.type!=='PAGE')&&!data.published)throw Error('A checkpoint page is missing. Review retained page copies.');return {...item,page:page||{id:item.page,removed:true},keys:new Map(item.keys)};};
      const snapshots=[];for(const item of data.snapshots)snapshots.push(await unpack(item));
      const branches=new Map();for(const item of data.branches){const b=await unpack(item);branches.set(b.id,b);}
      const current=snapshots.find(s=>s.page.id===data.current);if(!current)throw Error('The selected checkpoint is missing.');
      checkpointTrees.set(session,{...data,original,current,snapshots,branches});
    }
    loadedSessions.add(session);
  }
  let toolBusy=false;
  let windowMode='left',lastRect='';
  function placeWindow(force=false) {
    if (!figma.viewport || typeof figma.ui.reposition !== 'function') return;
    const {bounds:b,zoom:z}=figma.viewport;
    if (!b || !Number.isFinite(z) || z<=0) return;
    const availableWidth=Math.max(70,Math.floor(b.width*z));
    const availableHeight=Math.max(120,Math.floor(b.height*z)-40);
    const width=Math.min(400,availableWidth),height=windowMode==='left'?availableHeight:Math.min(640,availableHeight);
    const x=windowMode==='left'?b.x:b.x+(b.width-width/z)/2;
    const y=windowMode==='left'?b.y:b.y+(b.height-(height+40)/z)/2;
    const rect=JSON.stringify([width,height,x,y]);
    if (!force && rect===lastRect) return;
    lastRect=rect;
    figma.ui.resize(width,height);
    figma.ui.reposition(x,y);
  }
  figma.showUI(__html__, {width:400,height:640,themeColors:true});
  placeWindow(true);
  if (figma.viewport) {
    const timer=setInterval(()=>{if(windowMode==='left')placeWindow();},500);
    figma.on('close',()=>clearInterval(timer));
  }
  figma.ui.onmessage = async m => {
    if (!m || typeof m !== 'object' || Array.isArray(m)) return;
    if (m.kind === 'window-mode' && (m.mode==='left'||m.mode==='window')) {
      windowMode=m.mode;
      placeWindow(true);
      figma.ui.postMessage({kind:'window-state',mode:windowMode});
      return;
    }
    if (m.kind === 'session' && (m.session===null || typeof m.session==='string')) {activeSession=m.session;checkpointLoadError=null;const loadingSession=activeSession;checkpointLoaded=restoreTree(loadingSession).catch(e=>{if(activeSession===loadingSession)checkpointLoadError=e;});return;}
    if (m.kind === 'ready') { console.log('[Magi] UI ready; sending file context'); figma.ui.postMessage({kind:'window-state',mode:windowMode}); return context(); }
    if (m.kind === 'bind' && !figma.fileKey && !explicitDocumentId && typeof m.documentId === 'string' && /^[A-Za-z0-9_-]{8,128}$/.test(m.documentId)) { explicitDocumentId='local:'+m.documentId; return; }
    if (m.kind !== 'tool' || typeof m.callId !== 'string' || !m.callId || typeof m.name !== 'string' || !m.args || typeof m.args !== 'object' || Array.isArray(m.args)) return;
    let ownsTool=false;
    try {
      if (!activeSession || m.session!==activeSession) throw new Error('The tool request belongs to an inactive session.');
      if (!file() || m.fileKey !== file()) throw new Error('The request belongs to another file, or this file has no stable key.');
      if (m.pageId !== figma.currentPage.id) throw new Error('The current page changed. Retry with the new page context.');
      await checkpointLoaded;
      if(activeSession!==m.session)throw Error('The session changed while restoring checkpoint state.');
      if(checkpointLoadError)throw Error('Checkpoint recovery requires review: '+String(checkpointLoadError));
      if(toolBusy)throw new Error('Another application operation is still executing.');
      toolBusy=true;ownsTool=true;
      const fields={figma_selection:['branch'],figma_page:['branch'],figma_set_text:['id','text','branch'],figma_create_frame:['name','width','height','branch'],figma_checkpoint:['action','branch'],__magi_checkpoint_complete:['token']}[m.name];
      if (!fields || Object.keys(m.args).some(k=>!fields.includes(k))) throw new Error('Unsupported tool or argument.');
      let result;
      if(m.name==='figma_checkpoint'){result=await checkpointAction(m);await persistTree(treeFor());figma.ui.postMessage({kind:'tool-result',callId:m.callId,result});return;}
      if(m.name==='__magi_checkpoint_complete'){result=await finalizeCheckpoint(m);if(typeof figma.notify==='function')figma.notify('Checkpoint result applied; temporary pages cleaned.');figma.ui.postMessage({kind:'tool-result',callId:m.callId,result});return;}
      const isWrite=m.name==='figma_set_text'||m.name==='figma_create_frame';
      const activeTree=checkpointTrees.get(activeSession);
      if(isWrite&&activeTree&&activeTree.phase!=='cleaned'&&m.args.branch===undefined)throw Error('An open checkpoint requires an explicit branch for every write.');
      const scoped=m.args.branch!==undefined?resolveBranch(m.args.branch,isWrite):null;
      const targetPage=scoped?scoped.b.page:figma.currentPage;
      const inTarget=n=>{let p=n;while(p&&p.type!=='PAGE')p=p.parent;return p?.id===targetPage.id;};
      const unchanged=()=>activeSession===m.session&&file()===m.fileKey&&(!scoped||resolveBranch(m.args.branch).b===scoped.b);
      switch (m.name) {
        case 'figma_selection':
          if (!Array.isArray(m.selectionIds) || m.selectionIds.length > LIMIT) throw new Error('Invalid captured selection.');
          result = [];
          for (const id of m.selectionIds) { if (typeof id !== 'string') throw new Error('Invalid node ID.'); const mapped=scoped?[...scoped.b.keys.entries()].find(([,key])=>key===id)?.[0]||id:id;const n = await figma.getNodeByIdAsync(mapped); if (!n || !inTarget(n)) throw new Error('A selected node is no longer available.'); result.push(describe(n)); }
          break;
        case 'figma_page': result={nodes:targetPage.children.slice(0,LIMIT).map(describe),truncated:targetPage.children.length>LIMIT}; break;
        case 'figma_set_text': {
          if (typeof m.args.id !== 'string' || typeof m.args.text !== 'string' || m.args.text.length>20000) throw new Error('Text requires a node ID and at most 20000 characters.');
          const n=await figma.getNodeByIdAsync(m.args.id);
          if (!n || n.type !== 'TEXT' || !inTarget(n)) throw new Error('The node is not a text node in the bound resource.');
          if (n.hasMissingFont) throw new Error('This text node uses a missing font. Resolve the font in Figma before editing.');
          const fonts=n.fontName===figma.mixed ? n.getRangeAllFontNames(0,n.characters.length) : [n.fontName];
          const unique=new Map(fonts.map(font=>[JSON.stringify(font), font]));
          for (const font of unique.values()) await figma.loadFontAsync(font);
          if (m.pageId !== figma.currentPage.id || !unchanged()) throw new Error('The page or session changed while loading fonts.');
          n.characters=m.args.text;if(scoped){scoped.b.ops.push({kind:'text',key:scoped.b.keys.get(n.id),text:m.args.text});scoped.b.stamp=fingerprint(scoped.b.page);} result=describe(n); break;
        }
        case 'figma_create_frame': {
          const a=m.args;
          if (typeof a.name!=='string'||a.name.length>200||!Number.isFinite(a.width)||!Number.isFinite(a.height)||a.width<1||a.height<1||a.width>4096||a.height>4096) throw new Error('Frame name and dimensions (1–4096) are required.');
          const previous=figma.currentPage;
          if(scoped){await figma.setCurrentPageAsync(targetPage);if(!unchanged())throw Error('The branch changed before frame creation.');}
          try{const n=figma.createFrame();n.name=a.name;n.resize(a.width,a.height);if(scoped){const key='new:'+m.callId;scoped.b.keys.set(n.id,key);scoped.b.ops.push({kind:'frame',key,name:a.name,width:a.width,height:a.height});scoped.b.stamp=fingerprint(scoped.b.page);}result=describe(n);}finally{if(scoped&&!previous.removed)await figma.setCurrentPageAsync(previous);}break;
        }
        default: throw new Error('Unsupported Figma tool: '+m.name);
      }
      if(scoped)await persistTree(scoped.t);
      figma.ui.postMessage({kind:'tool-result',callId:m.callId,result});
    } catch(e) { if(m.name==='__magi_checkpoint_complete'&&typeof figma.notify==='function')figma.notify('Checkpoint publication failed; copies retained: '+String(e.message||e),{error:true});figma.ui.postMessage({kind:'tool-result',callId:m.callId,error:String(e.message||e)}); } finally {if(ownsTool)toolBusy=false;}
  };
  figma.on('selectionchange',context);
  figma.on('currentpagechange',context);
  console.log('[Magi] sandbox initialized');
  context();
})();
