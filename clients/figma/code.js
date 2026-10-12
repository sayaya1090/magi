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
  function context() { figma.ui.postMessage({kind:'context',fileKey:file(),pageId:figma.currentPage.id,selection:figma.currentPage.selection.slice(0,LIMIT).map(describe),tools}); }
  let windowMode='right',lastRect='';
  function placeWindow(force=false) {
    if (!figma.viewport || typeof figma.ui.reposition !== 'function') return;
    const {bounds:b,zoom:z}=figma.viewport;
    if (!b || !Number.isFinite(z) || z<=0) return;
    const availableWidth=Math.max(70,Math.floor(b.width*z));
    const availableHeight=Math.max(120,Math.floor(b.height*z)-40);
    const width=Math.min(400,availableWidth),height=windowMode==='right'?availableHeight:Math.min(640,availableHeight);
    const x=windowMode==='right'?b.x+b.width-width/z:b.x+(b.width-width/z)/2;
    const y=windowMode==='right'?b.y:b.y+(b.height-(height+40)/z)/2;
    const rect=JSON.stringify([width,height,x,y]);
    if (!force && rect===lastRect) return;
    lastRect=rect;
    figma.ui.resize(width,height);
    figma.ui.reposition(x,y);
  }
  figma.showUI(__html__, {width:400,height:640,themeColors:true});
  placeWindow(true);
  if (figma.viewport) {
    const timer=setInterval(()=>{if(windowMode==='right')placeWindow();},500);
    figma.on('close',()=>clearInterval(timer));
  }
  figma.ui.onmessage = async m => {
    if (!m || typeof m !== 'object' || Array.isArray(m)) return;
    if (m.kind === 'window-mode' && (m.mode==='right'||m.mode==='window')) {
      windowMode=m.mode;
      placeWindow(true);
      figma.ui.postMessage({kind:'window-state',mode:windowMode});
      return;
    }
    if (m.kind === 'session' && (m.session===null || typeof m.session==='string')) {activeSession=m.session;return;}
    if (m.kind === 'ready') { console.log('[Magi] UI ready; sending file context'); figma.ui.postMessage({kind:'window-state',mode:windowMode}); return context(); }
    if (m.kind === 'bind' && !figma.fileKey && !explicitDocumentId && typeof m.documentId === 'string' && /^[A-Za-z0-9_-]{8,128}$/.test(m.documentId)) { explicitDocumentId='local:'+m.documentId; return; }
    if (m.kind !== 'tool' || typeof m.callId !== 'string' || !m.callId || typeof m.name !== 'string' || !m.args || typeof m.args !== 'object' || Array.isArray(m.args)) return;
    try {
      if (!activeSession || m.session!==activeSession) throw new Error('The tool request belongs to an inactive session.');
      if (!file() || m.fileKey !== file()) throw new Error('The request belongs to another file, or this file has no stable key.');
      if (m.pageId !== figma.currentPage.id) throw new Error('The current page changed. Retry with the new page context.');
      const fields={figma_selection:[],figma_page:[],figma_set_text:['id','text'],figma_create_frame:['name','width','height']}[m.name];
      if (!fields || Object.keys(m.args).some(k=>!fields.includes(k))) throw new Error('Unsupported tool or argument.');
      let result;
      switch (m.name) {
        case 'figma_selection':
          if (!Array.isArray(m.selectionIds) || m.selectionIds.length > LIMIT) throw new Error('Invalid captured selection.');
          result = [];
          for (const id of m.selectionIds) { if (typeof id !== 'string') throw new Error('Invalid node ID.'); const n = await figma.getNodeByIdAsync(id); if (!n || !onPage(n)) throw new Error('A selected node is no longer available.'); result.push(describe(n)); }
          break;
        case 'figma_page': result={nodes:figma.currentPage.children.slice(0,LIMIT).map(describe),truncated:figma.currentPage.children.length>LIMIT}; break;
        case 'figma_set_text': {
          if (typeof m.args.id !== 'string' || typeof m.args.text !== 'string' || m.args.text.length>20000) throw new Error('Text requires a node ID and at most 20000 characters.');
          const n=await figma.getNodeByIdAsync(m.args.id);
          if (!n || n.type !== 'TEXT' || !onPage(n)) throw new Error('The node is not a text node.');
          if (n.hasMissingFont) throw new Error('This text node uses a missing font. Resolve the font in Figma before editing.');
          const fonts=n.fontName===figma.mixed ? n.getRangeAllFontNames(0,n.characters.length) : [n.fontName];
          const unique=new Map(fonts.map(font=>[JSON.stringify(font), font]));
          for (const font of unique.values()) await figma.loadFontAsync(font);
          if (m.pageId !== figma.currentPage.id || m.session!==activeSession) throw new Error('The page or session changed while loading fonts.');
          n.characters=m.args.text; result=describe(n); break;
        }
        case 'figma_create_frame': {
          const a=m.args;
          if (typeof a.name!=='string'||a.name.length>200||!Number.isFinite(a.width)||!Number.isFinite(a.height)||a.width<1||a.height<1||a.width>4096||a.height>4096) throw new Error('Frame name and dimensions (1–4096) are required.');
          const n=figma.createFrame();n.name=a.name;n.resize(a.width,a.height);result=describe(n);break;
        }
        default: throw new Error('Unsupported Figma tool: '+m.name);
      }
      figma.ui.postMessage({kind:'tool-result',callId:m.callId,result});
    } catch(e) { figma.ui.postMessage({kind:'tool-result',callId:m.callId,error:String(e.message||e)}); }
  };
  figma.on('selectionchange',context);
  figma.on('currentpagechange',context);
  console.log('[Magi] sandbox initialized');
  context();
})();
