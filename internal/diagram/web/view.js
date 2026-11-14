/* Shared local Mermaid presentation. No diagram-provided callbacks or URLs are executed. */
(() => {
  if (window.magiMermaid) return;
  const ko = (document.documentElement.lang || navigator.language || '').startsWith('ko');
  const say = (en, kr) => ko ? kr : en;
  let seq = 0, queue = Promise.resolve();
  const cache = new Map();
  // Font Awesome Free 6.7.2 by Fonticons, Inc. (CC BY 4.0 for icons).
  // https://fontawesome.com — see FONT-AWESOME-LICENSE. Bundled for offline IDE previews.
  const icons = {"zoomIn":{"viewBox":"0 0 512 512","path":"M416 208c0 45.9-14.9 88.3-40 122.7L502.6 457.4c12.5 12.5 12.5 32.8 0 45.3s-32.8 12.5-45.3 0L330.7 376c-34.4 25.2-76.8 40-122.7 40C93.1 416 0 322.9 0 208S93.1 0 208 0S416 93.1 416 208zM184 296c0 13.3 10.7 24 24 24s24-10.7 24-24l0-64 64 0c13.3 0 24-10.7 24-24s-10.7-24-24-24l-64 0 0-64c0-13.3-10.7-24-24-24s-24 10.7-24 24l0 64-64 0c-13.3 0-24 10.7-24 24s10.7 24 24 24l64 0 0 64z"},"zoomOut":{"viewBox":"0 0 512 512","path":"M416 208c0 45.9-14.9 88.3-40 122.7L502.6 457.4c12.5 12.5 12.5 32.8 0 45.3s-32.8 12.5-45.3 0L330.7 376c-34.4 25.2-76.8 40-122.7 40C93.1 416 0 322.9 0 208S93.1 0 208 0S416 93.1 416 208zM136 184c-13.3 0-24 10.7-24 24s10.7 24 24 24l144 0c13.3 0 24-10.7 24-24s-10.7-24-24-24l-144 0z"},"fit":{"viewBox":"0 0 448 512","path":"M32 32C14.3 32 0 46.3 0 64l0 96c0 17.7 14.3 32 32 32s32-14.3 32-32l0-64 64 0c17.7 0 32-14.3 32-32s-14.3-32-32-32L32 32zM64 352c0-17.7-14.3-32-32-32s-32 14.3-32 32l0 96c0 17.7 14.3 32 32 32l96 0c17.7 0 32-14.3 32-32s-14.3-32-32-32l-64 0 0-64zM320 32c-17.7 0-32 14.3-32 32s14.3 32 32 32l64 0 0 64c0 17.7 14.3 32 32 32s32-14.3 32-32l0-96c0-17.7-14.3-32-32-32l-96 0zM448 352c0-17.7-14.3-32-32-32s-32 14.3-32 32l0 64-64 0c-17.7 0-32 14.3-32 32s14.3 32 32 32l96 0c17.7 0 32-14.3 32-32l0-96z"},"copy":{"viewBox":"0 0 448 512","path":"M208 0L332.1 0c12.7 0 24.9 5.1 33.9 14.1l67.9 67.9c9 9 14.1 21.2 14.1 33.9L448 336c0 26.5-21.5 48-48 48l-192 0c-26.5 0-48-21.5-48-48l0-288c0-26.5 21.5-48 48-48zM48 128l80 0 0 64-64 0 0 256 192 0 0-32 64 0 0 48c0 26.5-21.5 48-48 48L48 512c-26.5 0-48-21.5-48-48L0 176c0-26.5 21.5-48 48-48z"},"question":{"viewBox":"0 0 512 512","path":"M512 240c0 114.9-114.6 208-256 208c-37.1 0-72.3-6.4-104.1-17.9c-11.9 8.7-31.3 20.6-54.3 30.6C73.6 471.1 44.7 480 16 480c-6.5 0-12.3-3.9-14.8-9.9c-2.5-6-1.1-12.8 3.4-17.4c0 0 0 0 0 0s0 0 0 0s0 0 0 0c0 0 0 0 0 0l.3-.3c.3-.3 .7-.7 1.3-1.4c1.1-1.2 2.8-3.1 4.9-5.7c4.1-5 9.6-12.4 15.2-21.6c10-16.6 19.5-38.4 21.4-62.9C17.7 326.8 0 285.1 0 240C0 125.1 114.6 32 256 32s256 93.1 256 208z"}};
  const style = document.createElement('style');
  style.textContent = `
    /* Material 3 (m3.material.io): outlined icon button small 40dp, icon 24dp, shape full, icon
       on-surface-variant, outline 1dp outline-variant; state layer hover .08 / pressed .12; focus
       ring 3dp at +2dp; plain tooltip 24dp on inverse-surface, body-small 12/16 tracking .4, shape 4;
       outlined card shape 12dp; outlined text field shape 4, outline role, focus 3dp primary; its container is filled
       (surface-container-lowest, white on light) so it reads as an input against the panel.
       Hosts that define no --md-sys roles (VS Code, JetBrains JCEF) get fallbacks with the same
       meaning: the tooltip inverts Canvas/CanvasText. */
    .magi-diagram { --magi-outline:var(--md-sys-color-outline-variant,color-mix(in srgb,currentColor 20%,transparent));
      --magi-field-outline:var(--md-sys-color-outline,color-mix(in srgb,currentColor 55%,transparent));
      --magi-on-variant:var(--md-sys-color-on-surface-variant,currentColor);
      --magi-primary:var(--md-sys-color-primary,Highlight); }
    .magi-diagram .magi-diagram-action { position:relative;box-sizing:border-box;font:inherit;color:var(--magi-on-variant);background:transparent;border:1px solid var(--magi-outline);border-radius:9999px;display:inline-flex;align-items:center;justify-content:center;width:40px;height:40px;padding:0;cursor:pointer; }
    .magi-diagram .magi-diagram-action:hover:not(:disabled) { background:color-mix(in srgb,currentColor 8%,transparent); }
    .magi-diagram .magi-diagram-action:active:not(:disabled) { background:color-mix(in srgb,currentColor 12%,transparent); }
    .magi-diagram .magi-diagram-action:focus-visible { outline:3px solid var(--magi-primary);outline-offset:2px; }
    .magi-diagram .magi-diagram-action:disabled { opacity:.38;cursor:default; }
    .magi-diagram .magi-diagram-action svg { width:24px;height:24px;fill:currentColor;flex:none; }
    .magi-diagram .magi-diagram-tooltip { display:none;position:absolute;top:calc(100% + 4px);left:0;z-index:2;box-sizing:border-box;min-height:24px;white-space:nowrap;padding:4px 8px;border-radius:4px;background:var(--md-sys-color-inverse-surface,CanvasText);color:var(--md-sys-color-inverse-on-surface,Canvas);font-size:12px;line-height:16px;letter-spacing:.4px;font-weight:400;pointer-events:none; }
    .magi-diagram-action:hover .magi-diagram-tooltip,.magi-diagram-action:focus-visible .magi-diagram-tooltip { display:block; }
    .magi-diagram-layout { display:flex;flex-wrap:wrap;gap:12px;align-items:start; }
    .magi-diagram-canvas { flex:2 1 280px;min-width:0; }
    .magi-diagram svg [role=button] { cursor:pointer; }
    .magi-diagram svg [role=button]:focus { outline:none; }
    .magi-diagram svg [role=button]:focus-visible { outline:3px solid var(--magi-primary);outline-offset:2px; }
    /* Material 3 elevation for interaction (m3 applying-elevation: shadows lift an element on focus or
       selection and drop again). The two layers are md-elevation's own recipe, key at .3 and ambient
       at .15 of the shadow role; SVG has no box-shadow, so they become drop-shadow, which has no
       spread. Hover level 1, focus or selected level 2, both level 3. */
    .magi-diagram { --magi-shadow:var(--md-sys-color-shadow,#000);
      --magi-key:color-mix(in srgb,var(--magi-shadow) 30%,transparent);
      --magi-ambient:color-mix(in srgb,var(--magi-shadow) 15%,transparent);
      --magi-elevation-1:drop-shadow(0 1px 2px var(--magi-key)) drop-shadow(0 1px 3px var(--magi-ambient));
      --magi-elevation-2:drop-shadow(0 1px 2px var(--magi-key)) drop-shadow(0 2px 6px var(--magi-ambient));
      --magi-elevation-3:drop-shadow(0 1px 3px var(--magi-key)) drop-shadow(0 4px 8px var(--magi-ambient)); }
    .magi-diagram svg [role=button] { transition:filter 200ms cubic-bezier(.2,0,0,1); }
    .magi-diagram svg [role=button]:hover { filter:var(--magi-elevation-1); }
    .magi-diagram svg [role=button]:focus-visible,.magi-diagram svg [role=button].magi-selected { filter:var(--magi-elevation-2); }
    .magi-diagram svg [role=button].magi-selected:focus-visible { filter:var(--magi-elevation-3); }
    @media (prefers-reduced-motion:reduce) { .magi-diagram svg [role=button] { transition:none; } }
    .magi-diagram-info { flex:1 1 220px;min-width:0;white-space:pre-wrap;overflow-wrap:anywhere;padding:12px;box-sizing:border-box;border:1px solid var(--magi-outline);border-radius:12px; }
    .magi-diagram-info[hidden] { display:none; }
    .magi-diagram-info textarea { display:block;width:100%;box-sizing:border-box;min-height:90px;margin-top:4px;padding:8px 16px;font:inherit;font-size:16px;line-height:24px;letter-spacing:.5px;color:var(--md-sys-color-on-surface,CanvasText);background:var(--md-sys-color-surface-container-lowest,Canvas);border:1px solid var(--magi-field-outline);border-radius:4px; }
    .magi-diagram-info textarea:focus { outline:none;border-color:var(--magi-primary);box-shadow:inset 0 0 0 2px var(--magi-primary); }
    .magi-diagram .magi-diagram-toggle { display:inline-flex;align-items:center;min-height:40px;padding-right:12px;color:var(--magi-on-variant);cursor:pointer; }
    .magi-diagram .magi-diagram-toggle input { width:18px;height:18px;margin:11px;accent-color:var(--magi-primary);cursor:pointer; }
    .magi-diagram .magi-diagram-toggle input:focus-visible { outline:3px solid var(--magi-primary);outline-offset:2px; }
    .magi-diagram .magi-flow-active .flowchart-link { stroke-dasharray:8 5;animation:magi-flow 1s linear infinite; }
    @keyframes magi-flow { to { stroke-dashoffset:-26; } }
    @media (prefers-reduced-motion:reduce) { .magi-diagram .magi-flow-active .flowchart-link { animation:none; } }
    @media (pointer:coarse) { .magi-diagram .magi-diagram-action { min-width:48px;min-height:48px; } }
  `;
  document.head.append(style);
  function allowed(source) {
    if (source.length > 24000) throw Error(say('Diagram exceeds 24,000 characters.', '다이어그램은 24,000자를 넘을 수 없습니다.'));
    if (/%%\s*\{|^\s*---|^\s*click\s|<\s*\/?\s*[a-z]|https?:\/\//im.test(source))
      throw Error(say('Use plain labels without HTML, links, click directives or configuration.', 'HTML, 링크, click 지시문, 설정 없이 일반 라벨을 사용해 주세요.'));
  }
  // Material 3 default theme: colors from the host's --md-sys-color roles (m3.material.io color
  // roles), type at body-medium 14/20 in the host typeface. Mermaid derives shades from real color
  // values, so the roles are read as computed values. A host that defines none (VS Code, JetBrains
  // JCEF) gets MAGI's console palette for the light or dark side its text color is on. Node shapes
  // keep Mermaid's meaning. A diagram's own classDef and style lines still win over the theme.
  const palette={
    dark:{'primary':'#FF7A1A','primary-container':'#4A2E0B','on-primary-container':'#FFD9B8','secondary-container':'#2B251C','on-secondary-container':'#E8E2D8','surface':'#14110D','on-surface':'#E8E2D8','on-surface-variant':'#C9C2B8','surface-container':'#211B14','surface-container-high':'#2B251C','surface-container-highest':'#352E24','outline':'#72675C','outline-variant':'#463E34'},
    light:{'primary':'#B45309','primary-container':'#F8D9A8','on-primary-container':'#3A1B00','secondary-container':'#ECE5D9','on-secondary-container':'#221D16','surface':'#FBF8F3','on-surface':'#221D16','on-surface-variant':'#4A453C','surface-container':'#F2ECE2','surface-container-high':'#ECE5D9','surface-container-highest':'#E6DED1','outline':'#8A7E6E','outline-variant':'#D8CFC0'}
  };
  const colorValue=v=>/^(#[0-9a-f]{3,8}|rgba?[(][0-9., %]+[)])$/i.test(v)?v:'';
  function themeFor(el) {
    const cs=getComputedStyle(el);
    const rgb=(cs.color.match(/[0-9.]+/g)||['0','0','0']).map(Number);
    const dark=(0.2126*rgb[0]+0.7152*rgb[1]+0.0722*rgb[2])/255>0.5;
    const base=palette[dark?'dark':'light'];
    const role=n=>colorValue(cs.getPropertyValue('--md-sys-color-'+n).trim())||base[n];
    // Mermaid's sanitizer blanks a theme value holding characters such as a hyphen, so families
    // like ui-monospace are left out rather than losing the whole list.
    const family=(cs.getPropertyValue('--md-ref-typeface-plain').trim()||cs.fontFamily).split(',')
      .map(f=>f.trim()).filter(f=>/^[A-Za-z0-9 ."]+$/.test(f)).join(', ')||'Arial';
    const vars={darkMode:dark,background:role('surface'),fontFamily:family,fontSize:'14px',
      primaryColor:role('primary-container'),primaryTextColor:role('on-primary-container'),primaryBorderColor:role('primary'),
      secondaryColor:role('secondary-container'),secondaryTextColor:role('on-secondary-container'),secondaryBorderColor:role('outline'),
      tertiaryColor:role('surface-container-high'),tertiaryTextColor:role('on-surface'),tertiaryBorderColor:role('outline-variant'),
      mainBkg:role('primary-container'),nodeBorder:role('primary'),nodeTextColor:role('on-primary-container'),
      textColor:role('on-surface'),titleColor:role('on-surface'),lineColor:role('on-surface-variant'),
      clusterBkg:role('surface-container'),clusterBorder:role('outline-variant'),edgeLabelBackground:role('surface-container-highest'),
      actorBkg:role('primary-container'),actorBorder:role('primary'),actorTextColor:role('on-primary-container'),actorLineColor:role('outline'),
      signalColor:role('on-surface'),signalTextColor:role('on-surface'),labelBoxBkgColor:role('surface-container-high'),
      labelBoxBorderColor:role('outline'),labelTextColor:role('on-surface'),
      noteBkgColor:role('secondary-container'),noteTextColor:role('on-secondary-container'),noteBorderColor:role('outline-variant')};
    return {vars,key:JSON.stringify(vars)};
  }
  function mount(pre, source, nativeSVG) {
    const input=pre.querySelector('code')||pre;
    if ((!nativeSVG && input.querySelector('svg, img, iframe, object')) || pre.dataset.magiDiagram === source) return;
    pre.dataset.magiDiagram = source;
    const box = document.createElement('figure');
    box.className = 'magi-diagram';
    box.style.cssText = 'margin:8px 0;padding:8px;border:1px solid var(--magi-outline);border-radius:12px;max-width:100%;overflow:hidden;color:inherit';
    const bar = document.createElement('div'); bar.style.cssText = 'display:flex;gap:8px;flex-wrap:wrap;align-items:center';
    const canvas = document.createElement('div'); canvas.style.cssText = 'overflow:auto;max-height:min(420px,55vh);min-height:48px;max-width:100%';
    canvas.className='magi-diagram-canvas';
    const layout=document.createElement('div');layout.className='magi-diagram-layout';
    const info=document.createElement('aside');info.className='magi-diagram-info';info.hidden=true;
    info.setAttribute('aria-label',say('Element details','요소 설명'));
    const heading=document.createElement('strong'), description=document.createElement('p');
    const questionLabel=document.createElement('label'), question=document.createElement('textarea');
    questionLabel.textContent=say('Follow-up question','추가 질문');questionLabel.append(question);
    const manualCopy=document.createElement('textarea');manualCopy.readOnly=true;manualCopy.hidden=true;manualCopy.setAttribute('aria-label',say('Question to copy','복사할 질문'));
    info.append(heading,description,questionLabel,manualCopy);layout.append(canvas,info);
    const descriptions=Object.create(null);
    for(const line of source.split('\n')) {
      const match=line.match(/^\s*%% magi-descriptions (.+)$/);
      if(!match) continue;
      try {
        const parsed=JSON.parse(match[1]);
        if(parsed && !Array.isArray(parsed) && typeof parsed==='object')
          for(const [label,text] of Object.entries(parsed).slice(0,100))
            if(typeof text==='string' && text.length<=2000) descriptions[label.trim()]=text;
      } catch { /* Optional malformed descriptions do not prevent rendering. */ }
    }
    const note = document.createElement('div'); note.setAttribute('role','status'); note.style.cssText = 'white-space:pre-wrap;font:inherit';
    const sourceBox = document.createElement('details'), summary = document.createElement('summary'), code = document.createElement('pre');
    summary.textContent = say('Source', '원본'); code.textContent = source; sourceBox.append(summary, code);
    let zoom = 1, svg, fitWidth = 0;
    function button(icon, en, kr, action) {
      const b = document.createElement('button'); b.type='button'; b.className='magi-diagram-action';
      b.title=say(en,kr); b.setAttribute('aria-label',b.title);
      const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');
      svg.setAttribute('viewBox',icons[icon].viewBox); svg.setAttribute('aria-hidden','true');
      const path=document.createElementNS(svg.namespaceURI,'path');path.setAttribute('d',icons[icon].path);svg.append(path);b.append(svg);
      const tip=document.createElement('span');tip.className='magi-diagram-tooltip';tip.setAttribute('aria-hidden','true');tip.textContent=b.title;b.append(tip);
      b.onclick=action; bar.append(b); return b;
    }
    function scale(n) {
      zoom=Math.max(.25,Math.min(4,n));
      if(svg) {
        svg.style.width=(fitWidth*zoom)+'px'; svg.style.height='auto'; svg.style.maxWidth='none';
        svg.style.display='block'; svg.style.maxWidth=zoom===1?'100%':'none';
      }
    }
    function fit() {
      if(!svg) return;
      const vb=svg.viewBox.baseVal;
      const width=vb.width || svg.getBoundingClientRect().width || canvas.clientWidth;
      const height=vb.height || svg.getBoundingClientRect().height || 48;
      // Never enlarge a small diagram merely to fill the transcript column.
      fitWidth=width*Math.min(1,Math.max(1,canvas.clientWidth)/width,Math.min(420,innerHeight*.55)/height);
      scale(1);
    }
    button('zoomIn','Zoom in','확대',()=>scale(zoom*1.25)); button('zoomOut','Zoom out','축소',()=>scale(zoom/1.25)); button('fit','Fit','맞춤',fit);
    async function copy(text) {
      try { await navigator.clipboard.writeText(text); note.textContent=say('Copied.','복사했습니다.'); }
      catch { sourceBox.open=true; note.textContent=say('Clipboard unavailable. Select and copy the source below.','클립보드를 사용할 수 없습니다. 아래 원본을 선택해 복사해 주세요.'); }
    }
    button('copy','Copy source','원본 복사',()=>copy(source));
    let selected='', selectedNode=null;
    const ask=button('question','Copy question','질문 복사',async()=>{
      const text=question.value+'\n\n```mermaid\n'+source+'\n```';
      try { await navigator.clipboard.writeText(text);note.textContent=say('Question copied. Paste it into the conversation to send.','질문을 복사했습니다. 대화 입력창에 붙여넣어 보내세요.'); }
      catch { manualCopy.hidden=false;manualCopy.value=text;manualCopy.focus();manualCopy.select();note.textContent=say('Clipboard unavailable. Copy the selected question manually.','클립보드를 사용할 수 없습니다. 선택된 질문을 직접 복사하세요.'); }
    }); ask.disabled=true;
    box.append(bar,layout,note,sourceBox); pre.replaceWith(box);
    note.textContent=say('Rendering diagram…','다이어그램을 그리는 중…');
    queue=queue.catch(()=>{}).then(async()=>{
      if (!box.isConnected) return;
      try {
        allowed(source);
        if (nativeSVG) {
          // Already rendered by the IDE: move that node, retaining native labels
          // and theme. Never parse its CSS/text as Mermaid or render it twice.
          svg=nativeSVG;
        } else {
          const theme=themeFor(box), key=theme.key+source;
          let rendered=cache.get(key);
          if (!rendered) {
            window.mermaid.initialize({startOnLoad:false,securityLevel:'strict',htmlLabels:false,theme:'base',themeVariables:theme.vars,fontFamily:theme.vars.fontFamily,maxTextSize:24000,maxEdges:200,flowchart:{htmlLabels:false},suppressErrorRendering:true});
            rendered=(await window.mermaid.render('magiDiagram'+(++seq),source)).svg;
            if(cache.size>=32) cache.delete(cache.keys().next().value);
            cache.set(key,rendered);
          }
          if(!box.isConnected) return;
          // Only Mermaid's strict-mode output enters this isolated SVG parse boundary.
          const unique=rendered.replace(/magiDiagram[0-9]+/g,'magiDiagram'+(++seq));
          const parsed=new DOMParser().parseFromString(unique,'image/svg+xml');
          if(parsed.querySelector('parsererror')) throw Error('Invalid SVG');
          parsed.querySelectorAll('script,foreignObject,iframe,image,a').forEach(n=>n.remove());
          parsed.querySelectorAll('*').forEach(n=>Array.from(n.attributes).forEach(a=>{
            if(/^on/i.test(a.name)||/href$/i.test(a.name)) n.removeAttribute(a.name);
          }));
          svg=document.importNode(parsed.documentElement,true);
        }
        svg.setAttribute('role','img'); svg.setAttribute('aria-label',say('Mermaid diagram','Mermaid 다이어그램'));
        canvas.replaceChildren(svg); fit();
        if(/^\s*(?:flowchart|graph)\b/m.test(source) && /^\s*%% magi-animate\s*$/m.test(source)) {
          const label=document.createElement('label'), toggle=document.createElement('input');toggle.type='checkbox';label.className='magi-diagram-toggle';
          toggle.checked=!matchMedia('(prefers-reduced-motion: reduce)').matches;
          const update=()=>svg.classList.toggle('magi-flow-active',toggle.checked);
          toggle.addEventListener('change',update);update();
          label.append(toggle,document.createTextNode(say('Animate flow','흐름 애니메이션')));bar.append(label);
        } note.textContent=say('Select a node to prepare a question.','노드를 선택하면 질문을 준비할 수 있습니다.');
        svg.querySelectorAll('.node,.actor,.classGroup,.stateGroup').forEach(n=>{
          n.setAttribute('tabindex','0'); n.setAttribute('role','button'); n.setAttribute('aria-pressed','false');
          const select=()=>{
            if(selectedNode && selectedNode!==n){selectedNode.classList.remove('magi-selected');selectedNode.setAttribute('aria-pressed','false');}
            n.classList.add('magi-selected');n.setAttribute('aria-pressed','true');selectedNode=n;
            const next=(n.textContent||'').trim();
            if(next!==selected) question.value=say('Explain this diagram element: ','이 다이어그램 요소를 설명해 주세요: ')+next;
            manualCopy.hidden=true;selected=next;ask.disabled=!selected;info.hidden=!selected;heading.textContent=selected;
            description.textContent=descriptions[selected]||say('No explanation was supplied for this element. You can ask a follow-up question below.','이 요소의 설명이 제공되지 않았습니다. 아래에서 추가 질문을 작성할 수 있습니다.');
            note.textContent=say('Selected: ','선택: ')+selected;fit();
          };
          const followup=e=>{e.preventDefault();select();question.focus();};
          n.addEventListener('contextmenu',followup);
          n.addEventListener('keydown',e=>{if(e.key==='ContextMenu'||(e.shiftKey&&e.key==='F10')) followup(e);});
          n.addEventListener('click',select);n.addEventListener('keydown',e=>{if(e.key==='Enter'||e.key===' '){e.preventDefault();select();}});
        });
      } catch(e) { note.textContent=say('Diagram could not be rendered: ','다이어그램을 표시할 수 없습니다: ')+e.message; sourceBox.open=true; }
    });
  }
  function scan() {
    document.querySelectorAll('pre > code').forEach(code=>{
      const lang=code.getAttribute('data-lang')||code.getAttribute('data-language')||(code.className||'').replace(/^language-/,'');
      if(lang.trim().toLowerCase()!=='mermaid' || code.getAttribute('data-streaming')==='true') return;
      const pre=code.parentElement, nativeSVG=code.querySelector('svg');
      if (nativeSVG) {
        // JetBrains keeps the UTF-8 source on its copy button even after native
        // rendering. Without that metadata, preserve the SVG without a toolbar.
        const encoded=pre.querySelector('[data-fence-content]')?.getAttribute('data-fence-content');
        if (!encoded) return;
        try {
          const source=new TextDecoder('utf-8',{fatal:true}).decode(Uint8Array.from(atob(encoded),c=>c.charCodeAt(0)));
          allowed(source);
          mount(pre,source,nativeSVG);
        } catch { /* Invalid metadata: leave the IDE's rendered diagram intact. */ }
        return;
      }
      if (code.querySelector('img, iframe, object') || code.dataset.processed === 'true') return;
      mount(pre,code.textContent||'');
    });
  }
  window.magiMermaid={mount,scan};
  let pending=false;
  const observer=new MutationObserver(()=>{if(!pending){pending=true;setTimeout(()=>{pending=false;scan();},100);}});
  function start(){observer.observe(document.body,{childList:true,subtree:true,characterData:true,attributes:true,attributeFilter:['data-streaming']});scan();}
  if(document.body) start();else document.addEventListener('DOMContentLoaded',start,{once:true});
})();
