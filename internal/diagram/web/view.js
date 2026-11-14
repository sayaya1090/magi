/* Shared local Mermaid presentation. No diagram-provided callbacks or URLs are executed. */
(() => {
  if (window.magiMermaid) return;
  const ko = (document.documentElement.lang || navigator.language || '').startsWith('ko');
  const say = (en, kr) => ko ? kr : en;
  let seq = 0, queue = Promise.resolve();
  const cache = new Map();
  // What a reader chose in a diagram, by its source: the IDE preview re-patches the page on every
  // answer update, which re-mounts the diagram, and the chosen case and node must survive that.
  const memory = new Map();
  // Font Awesome Free 6.7.2 by Fonticons, Inc. (CC BY 4.0 for icons).
  // https://fontawesome.com — see FONT-AWESOME-LICENSE. Bundled for offline IDE previews.
  const icons = {"zoomIn":{"viewBox":"0 0 512 512","path":"M416 208c0 45.9-14.9 88.3-40 122.7L502.6 457.4c12.5 12.5 12.5 32.8 0 45.3s-32.8 12.5-45.3 0L330.7 376c-34.4 25.2-76.8 40-122.7 40C93.1 416 0 322.9 0 208S93.1 0 208 0S416 93.1 416 208zM184 296c0 13.3 10.7 24 24 24s24-10.7 24-24l0-64 64 0c13.3 0 24-10.7 24-24s-10.7-24-24-24l-64 0 0-64c0-13.3-10.7-24-24-24s-24 10.7-24 24l0 64-64 0c-13.3 0-24 10.7-24 24s10.7 24 24 24l64 0 0 64z"},"zoomOut":{"viewBox":"0 0 512 512","path":"M416 208c0 45.9-14.9 88.3-40 122.7L502.6 457.4c12.5 12.5 12.5 32.8 0 45.3s-32.8 12.5-45.3 0L330.7 376c-34.4 25.2-76.8 40-122.7 40C93.1 416 0 322.9 0 208S93.1 0 208 0S416 93.1 416 208zM136 184c-13.3 0-24 10.7-24 24s10.7 24 24 24l144 0c13.3 0 24-10.7 24-24s-10.7-24-24-24l-144 0z"},"fit":{"viewBox":"0 0 448 512","path":"M32 32C14.3 32 0 46.3 0 64l0 96c0 17.7 14.3 32 32 32s32-14.3 32-32l0-64 64 0c17.7 0 32-14.3 32-32s-14.3-32-32-32L32 32zM64 352c0-17.7-14.3-32-32-32s-32 14.3-32 32l0 96c0 17.7 14.3 32 32 32l96 0c17.7 0 32-14.3 32-32s-14.3-32-32-32l-64 0 0-64zM320 32c-17.7 0-32 14.3-32 32s14.3 32 32 32l64 0 0 64c0 17.7 14.3 32 32 32s32-14.3 32-32l0-96c0-17.7-14.3-32-32-32l-96 0zM448 352c0-17.7-14.3-32-32-32s-32 14.3-32 32l0 64-64 0c-17.7 0-32 14.3-32 32s14.3 32 32 32l96 0c17.7 0 32-14.3 32-32l0-96z"},"copy":{"viewBox":"0 0 448 512","path":"M208 0L332.1 0c12.7 0 24.9 5.1 33.9 14.1l67.9 67.9c9 9 14.1 21.2 14.1 33.9L448 336c0 26.5-21.5 48-48 48l-192 0c-26.5 0-48-21.5-48-48l0-288c0-26.5 21.5-48 48-48zM48 128l80 0 0 64-64 0 0 256 192 0 0-32 64 0 0 48c0 26.5-21.5 48-48 48L48 512c-26.5 0-48-21.5-48-48L0 176c0-26.5 21.5-48 48-48z"},"question":{"viewBox":"0 0 512 512","path":"M512 240c0 114.9-114.6 208-256 208c-37.1 0-72.3-6.4-104.1-17.9c-11.9 8.7-31.3 20.6-54.3 30.6C73.6 471.1 44.7 480 16 480c-6.5 0-12.3-3.9-14.8-9.9c-2.5-6-1.1-12.8 3.4-17.4c0 0 0 0 0 0s0 0 0 0s0 0 0 0c0 0 0 0 0 0l.3-.3c.3-.3 .7-.7 1.3-1.4c1.1-1.2 2.8-3.1 4.9-5.7c4.1-5 9.6-12.4 15.2-21.6c10-16.6 19.5-38.4 21.4-62.9C17.7 326.8 0 285.1 0 240C0 125.1 114.6 32 256 32s256 93.1 256 208z"}};
  const style = document.createElement('style');
  style.textContent = `
    /* Material 3 (m3.material.io): outlined icon button small 40dp, icon 24dp, shape full, icon
       on-surface-variant, outline 1dp outline-variant; state layer hover .08 / pressed .12; focus
       ring 3dp at +2dp; plain tooltip 24dp on inverse-surface, body-small 12/16 tracking .4, shape 4;
       outlined card shape 12dp; filled text field (md-filled-text-field tokens): container surface-container-highest,
       shape extra-small top / none bottom, no side borders, 1dp on-surface-variant active indicator,
       3dp primary on focus, hover state layer .08.
       Hosts that define no --md-sys roles (VS Code, JetBrains JCEF) get fallbacks with the same
       meaning: the tooltip inverts Canvas/CanvasText. */
    .magi-diagram { --magi-outline:var(--md-sys-color-outline-variant,color-mix(in srgb,currentColor 20%,transparent));
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
    pre.magi-diagram-pending { background:transparent;border:1px dashed var(--md-sys-color-outline-variant,color-mix(in srgb,currentColor 30%,transparent));border-radius:12px;padding:16px 12px;min-height:48px; }
    pre.magi-diagram-pending > code { display:none !important; }
    pre.magi-diagram-pending::before { content:attr(data-magi-pending);color:var(--md-sys-color-on-surface-variant,currentColor);opacity:.8;font:inherit;white-space:normal; }
    /* Hosts whose composer takes a selected node (JetBrains, web console) draw no question field or copy button of their own. */
    .magi-composer-host .magi-diagram .magi-diagram-ask, .magi-composer-host .magi-diagram-info label { display:none !important; }
    /* Nodes the model linked to the selected one. */
    .magi-diagram svg [role=button].magi-linked { filter:var(--magi-elevation-1);outline:2px dashed var(--magi-primary);outline-offset:2px; }
    .magi-diagram-info textarea { display:block;width:100%;box-sizing:border-box;min-height:90px;margin-top:4px;padding:8px 16px;font:inherit;font-size:16px;line-height:24px;letter-spacing:.5px;color:var(--md-sys-color-on-surface,CanvasText);background:var(--md-sys-color-surface-container-highest,color-mix(in srgb,CanvasText 8%,Canvas));border:0;border-bottom:1px solid var(--md-sys-color-on-surface-variant,color-mix(in srgb,CanvasText 70%,Canvas));border-radius:4px 4px 0 0; }
    .magi-diagram-info textarea:hover:not(:focus) { border-bottom-color:var(--md-sys-color-on-surface,CanvasText);background-image:linear-gradient(color-mix(in srgb,var(--md-sys-color-on-surface,CanvasText) 8%,transparent),color-mix(in srgb,var(--md-sys-color-on-surface,CanvasText) 8%,transparent)); }
    .magi-diagram-info textarea::placeholder { color:var(--md-sys-color-on-surface-variant,GrayText);opacity:1; }
    .magi-diagram-info textarea:focus { outline:none;border-bottom-color:var(--magi-primary);box-shadow:inset 0 -2px 0 var(--magi-primary); }
    .magi-diagram .magi-diagram-toggle { display:inline-flex;align-items:center;min-height:40px;padding-right:12px;color:var(--magi-on-variant);cursor:pointer; }
    .magi-diagram .magi-diagram-toggle input { width:18px;height:18px;margin:11px;accent-color:var(--magi-primary);cursor:pointer; }
    .magi-diagram .magi-diagram-toggle input:focus-visible { outline:3px solid var(--magi-primary);outline-offset:2px; }
    /* Mermaid's own #id .edge-pattern-solid sets stroke-dasharray:0 with an id selector, which hid
       the flow; !important lets the dash through. */
    .magi-diagram .magi-diagram-cases { display:inline-flex;flex-wrap:wrap;align-items:center;gap:4px; }
    .magi-diagram .magi-diagram-cases select { min-height:40px;padding:0 12px;font:inherit;color:inherit;background:transparent;border:1px solid var(--magi-outline);border-radius:4px;cursor:pointer; }
    .magi-diagram .magi-diagram-cases select:focus-visible { outline:3px solid var(--magi-primary);outline-offset:2px; }
    .magi-diagram .magi-flow-active .flowchart-link { stroke-dasharray:8 5 !important;animation:magi-flow 1s linear infinite; }
    /* Theme roles laid over every diagram, including SVG the IDE rendered itself. Nodes are filled
       containers with no border (M3 filled card); clusters keep a 1dp outline-variant. A diagram's
       classDef/style is written inline with !important by Mermaid and still wins. */
    .magi-diagram svg[role=img] .node .label-container,.magi-diagram svg[role=img] .node > :is(rect,polygon,circle,ellipse,path) { fill:var(--magi-node-fill)!important;stroke:none!important; }
    .magi-diagram svg[role=img] .node :is(text,tspan,.nodeLabel,.nodeLabel *,foreignObject *) { fill:var(--magi-node-text)!important;color:var(--magi-node-text)!important; }
    .magi-diagram svg[role=img] .cluster rect { fill:var(--magi-cluster-fill)!important;stroke:var(--magi-cluster-border)!important;stroke-width:1px!important; }
    .magi-diagram svg[role=img] .flowchart-link { stroke:var(--magi-edge)!important; }
    @keyframes magi-flow { to { stroke-dashoffset:-26; } }
    /* A selected node's arrows flow; the others stay still. */
    .magi-diagram .magi-flow-focus .flowchart-link.magi-flow-near { stroke-dasharray:8 5 !important;animation:magi-flow 1s linear infinite; }
    @media (prefers-reduced-motion:reduce) { .magi-diagram :is(.magi-flow-active .flowchart-link,.magi-flow-focus .flowchart-link.magi-flow-near) { animation:none; } }
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
    dark:{'secondary':'#E8B89F','tertiary':'#5CD8E6','error':'#F2B8B5','primary':'#FF7A1A','primary-container':'#4A2E0B','on-primary-container':'#FFD9B8','secondary-container':'#2B251C','on-secondary-container':'#E8E2D8','surface':'#14110D','on-surface':'#E8E2D8','on-surface-variant':'#C9C2B8','surface-container':'#211B14','surface-container-high':'#2B251C','surface-container-highest':'#352E24','outline':'#72675C','outline-variant':'#463E34'},
    light:{'secondary':'#82604F','tertiary':'#0E7490','error':'#B3261E','primary':'#B45309','primary-container':'#F8D9A8','on-primary-container':'#3A1B00','secondary-container':'#ECE5D9','on-secondary-container':'#221D16','surface':'#FBF8F3','on-surface':'#221D16','on-surface-variant':'#4A453C','surface-container':'#F2ECE2','surface-container-high':'#ECE5D9','surface-container-highest':'#E6DED1','outline':'#8A7E6E','outline-variant':'#D8CFC0'}
  };
  const colorValue=v=>/^(#[0-9a-f]{3,8}|rgba?[(][0-9., %]+[)])$/i.test(v)?v:'';
  let hostTheme=null;
  const camel=n=>n.replace(/-([a-z])/g,(_,c)=>c.toUpperCase());
  // A node coloured by meaning gets its role's container and the text that goes on it. Roles whose
  // container the palette does not name are tinted onto the surface, keeping on-surface text.
  function rolePaint(name, role) {
    if(/^#[0-9a-f]{6}$/i.test(name)) {
      const n=parseInt(name.slice(1),16), y=(0.2126*(n>>16)+0.7152*((n>>8)&255)+0.0722*(n&255))/255;
      return {bg:name, fg:y>0.55?'#000000':'#FFFFFF'};
    }
    if(name==='primary') return {bg:role('primary-container'), fg:role('on-primary-container')};
    if(name==='neutral') return {bg:role('surface-container-high'), fg:role('on-surface')};
    if(['secondary','tertiary','error'].includes(name)) {
      const own=colorValue(String(role(name+'-container')||''));
      return {bg:own||'color-mix(in srgb, '+role(name)+' 24%, '+role('surface-container')+')', fg:own?(role('on-'+name+'-container')||role('on-surface')):role('on-surface')};
    }
    return null;
  }
  function themeFor(el) {
    const cs=getComputedStyle(el);
    const rgb=(cs.color.match(/[0-9.]+/g)||['0','0','0']).map(Number);
    const dark=(0.2126*rgb[0]+0.7152*rgb[1]+0.0722*rgb[2])/255>0.5;
    const base=palette[dark?'dark':'light'];
    const given=hostTheme&&hostTheme[dark?'dark':'light'];
    // The person's [theme] (from the daemon) wins, then the host's roles, then the built-in palette.
    // tertiary is called accent in MAGI's palette.
    const fromConfig=n=>given?colorValue(String(given[camel(n)]||(n==='tertiary'?given.accent:'')||'')):'';
    const role=n=>fromConfig(n)||colorValue(cs.getPropertyValue('--md-sys-color-'+n).trim())||base[n];
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
    return {vars,role,key:JSON.stringify(vars)};
  }
  function mount(pre, source, nativeSVG) {
    const input=pre.querySelector('code')||pre;
    if ((!nativeSVG && input.querySelector('svg, img, iframe, object')) || pre.dataset.magiDiagram === source) return;
    pre.dataset.magiDiagram = source;
    const box = document.createElement('figure');
    box.className = 'magi-diagram';
    box.style.cssText = 'margin:8px 0;padding:8px;border:1px solid var(--magi-outline);border-radius:12px;max-width:100%;overflow:hidden;color:inherit';
    const bar = document.createElement('div'); bar.style.cssText = 'display:flex;gap:8px;flex-wrap:wrap;align-items:center';
    const canvas = document.createElement('div'); canvas.style.cssText = 'overflow:auto;min-height:48px;max-width:100%';
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
    const colors=Object.create(null);
    for(const line of source.split('\n')) {
      const match=line.match(/^\s*%% magi-colors (.+)$/);
      if(!match) continue;
      try {
        const parsed=JSON.parse(match[1]);
        if(parsed && !Array.isArray(parsed) && typeof parsed==='object')
          for(const [label,c] of Object.entries(parsed).slice(0,100))
            if(typeof c==='string' && /^(primary|secondary|tertiary|error|neutral|#[0-9a-f]{6})$/i.test(c)) colors[label.trim()]=c;
      } catch { /* Optional malformed colours do not prevent rendering. */ }
    }
    const flows=Object.create(null), triggers=Object.create(null), links=Object.create(null); let flowControl='radio';
    for(const line of source.split('\n')) {
      const lk=line.match(/^\s*%% magi-links (.+)$/); if(lk){ try { const t=JSON.parse(lk[1]); if(t && typeof t==='object' && !Array.isArray(t)) for(const [n,l] of Object.entries(t).slice(0,100)) if(Array.isArray(l)) links[n.trim()]=l.filter(x=>typeof x==='string').map(x=>x.trim()).slice(0,50); } catch { /* optional */ } }
      const c=line.match(/^\s*%% magi-flow-control (radio|checkbox|dropdown)\s*$/); if(c) flowControl=c[1];
      const tm=line.match(/^\s*%% magi-flow-triggers (.+)$/); if(tm){ try { const t=JSON.parse(tm[1]); if(t && typeof t==='object' && !Array.isArray(t)) for(const [n,l] of Object.entries(t).slice(0,20)) if(Array.isArray(l)) triggers[n]=l.filter(x=>typeof x==='string').map(x=>x.trim()).slice(0,50); } catch { /* optional */ } }
      const match=line.match(/^\s*%% magi-flows (.+)$/);
      if(!match) continue;
      try {
        const parsed=JSON.parse(match[1]);
        if(parsed && !Array.isArray(parsed) && typeof parsed==='object')
          for(const [name,path] of Object.entries(parsed).slice(0,20))
            if(Array.isArray(path) && path.length>=2 && path.every(l=>typeof l==='string')) flows[name]=path.slice(0,50);
      } catch { /* Optional malformed cases do not prevent rendering. */ }
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
      // Never enlarge a small diagram merely to fill the transcript column, and fit it to the width
      // only. A height cap squeezed sequence diagrams and long TD flows (measured: a 1050x723
      // sequence drawn 610px wide in an 866px column) until their text could not be read, and a
      // scroll inside the box does not work where the host takes the wheel (JetBrains), so the
      // diagram shows whole and the page scrolls.
      fitWidth=width*Math.min(1,Math.max(1,canvas.clientWidth)/width);
      scale(1);
    }
    button('zoomIn','Zoom in','확대',()=>scale(zoom*1.25)); button('zoomOut','Zoom out','축소',()=>scale(zoom/1.25)); button('fit','Fit','맞춤',fit);
    async function copy(text) {
      try { await navigator.clipboard.writeText(text); note.textContent=say('Copied.','복사했습니다.'); }
      catch { sourceBox.open=true; note.textContent=say('Clipboard unavailable. Select and copy the source below.','클립보드를 사용할 수 없습니다. 아래 원본을 선택해 복사해 주세요.'); }
    }
    button('copy','Copy source','원본 복사',()=>copy(source));
    let selected='', selectedNode=null, flowOn=null, activeCases=[], casesReset=()=>{};
    const kept=memory.get(source)||{}, remember=p=>memory.set(source,Object.assign({},memory.get(source),p));
    // Arrows of the chosen cases: consecutive labels of each path, as from_to node keys.
    // A named animation is on while any trigger connected to it is true: its picker item or a
    // selected node listed in its triggers.
    const caseEdges=(names)=>{ const set=new Set(), keyOf=Object.create(null); if(!svg) return set;
      svg.querySelectorAll('.node').forEach(n=>{ keyOf[(n.textContent||'').trim()]=nodeKey(n); });
      for(const name of names){ const path=flows[name]||[];
        for(let i=0;i+1<path.length;i++){ const a=keyOf[path[i].trim()], b=keyOf[path[i+1].trim()]; if(a&&b) set.add(a+'_'+b); } }
      return set; };
    // Arrows touching the selected node flow (data-id L_<from>_<to>_<n>); with nothing selected the
    // animate toggle, when the diagram has one, moves every arrow.
    const nodeKey=el=>((el.id||'').match(/flowchart-(.+)-\d+$/)||[])[1]||'';
    // An arrow's ends as from_to node keys. Mermaid's own ids say so (L_<from>_<to>_<n>); an arrow the
    // diagram named itself (A route@--> B) carries only that name, so its ends are the nodes nearest
    // the first and last point of its path. Measured once per arrow.
    const edgeKey=e=>{
      if(e.__magiEdge!==undefined) return e.__magiEdge;
      const id=e.getAttribute('data-id')||'', keys=new Set([...svg.querySelectorAll('.node')].map(nodeKey).filter(Boolean));
      const auto=id.match(/^L_(.+)_\d+$/);
      if(auto){ const body=auto[1]; for(const k of keys) if(body.startsWith(k+'_') && keys.has(body.slice(k.length+1))) return e.__magiEdge=body; }
      // A diagram drawn while hidden has every node at size 0, and the nearest node to both ends
      // is the same one: such a guess is not kept, and the next paint measures again.
      let key='', measured=false;
      try {
        measured=[...svg.querySelectorAll('.node')].some(n=>{ const r=n.getBoundingClientRect(); return r.width>0||r.height>0; });
        const m=e.getScreenCTM(), len=e.getTotalLength();
        const end=t=>{ const q=e.getPointAtLength(t); const x=m.a*q.x+m.c*q.y+m.e, y=m.b*q.x+m.d*q.y+m.f; let best='', dist=Infinity;
          svg.querySelectorAll('.node').forEach(n=>{ const r=n.getBoundingClientRect(); const dx=Math.max(r.left-x,0,x-r.right), dy=Math.max(r.top-y,0,y-r.bottom), d=dx*dx+dy*dy; if(d<dist){dist=d;best=nodeKey(n);} });
          return best; };
        const a=end(0), b=end(len); if(a&&b) key=a+'_'+b;
      } catch { /* no geometry: the arrow simply never moves on its own */ }
      if(!key && auto) key=auto[1];
      if(!measured) return key;
      return e.__magiEdge=key;
    };
    const flowPaint=()=>{
      if(!svg) return;
      const key=selectedNode?nodeKey(selectedNode):'';
      const picked=selectedNode?(selectedNode.textContent||'').trim():'';
      const named=[...new Set([...activeCases, ...Object.keys(triggers).filter(n=>flows[n] && picked && triggers[n].includes(picked))])];
      const cases=named.length?caseEdges(named):null;
      // The model may link a node to others that animate with it: they lift, and arrows among them move.
      const keyOfLabel=Object.create(null); svg.querySelectorAll('.node').forEach(n=>{ keyOfLabel[(n.textContent||'').trim()]=nodeKey(n); });
      const group=picked && links[picked] ? [picked,...links[picked]].map(l=>keyOfLabel[l]).filter(Boolean) : [];
      const groupSet=new Set(group), pairs=new Set(); for(const a of group) for(const b of group) if(a!==b) pairs.add(a+'_'+b);
      svg.querySelectorAll('.node').forEach(n=>n.classList.toggle('magi-linked', groupSet.has(nodeKey(n)) && n!==selectedNode));
      const moving=flowOn?flowOn():!matchMedia('(prefers-reduced-motion: reduce)').matches;
      svg.querySelectorAll('.flowchart-link').forEach(e=>{
        const rest=edgeKey(e);
        // An arrow moves when any trigger connected to it is on: a chosen case or the selected node.
        e.classList.toggle('magi-flow-near', pairs.has(rest) || (!!cases && cases.has(rest)) || (!!key && (rest.startsWith(key+'_') || rest.endsWith('_'+key))));
      });
      svg.classList.toggle('magi-flow-active', !!flowOn && flowOn() && !key && !cases);
      svg.classList.toggle('magi-flow-focus', (!!key || !!cases) && moving);
    };
    const ask=button('question','Copy question','질문 복사',async()=>{
      const text=(question.value.trim()||question.placeholder)+'\n\n```mermaid\n'+source+'\n```';
      try { await navigator.clipboard.writeText(text);note.textContent=say('Question copied. Paste it into the conversation to send.','질문을 복사했습니다. 대화 입력창에 붙여넣어 보내세요.'); }
      catch { manualCopy.hidden=false;manualCopy.value=text;manualCopy.focus();manualCopy.select();note.textContent=say('Clipboard unavailable. Copy the selected question manually.','클립보드를 사용할 수 없습니다. 선택된 질문을 직접 복사하세요.'); }
    }); ask.disabled=true; ask.classList.add('magi-diagram-ask');
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
        svg.setAttribute('role','img'); { const paint=()=>{ const th=themeFor(box), look=th.vars; for (const [k,v] of [['--magi-node-fill',look.primaryColor],['--magi-node-text',look.primaryTextColor],['--magi-cluster-fill',look.clusterBkg],['--magi-cluster-border',look.clusterBorder],['--magi-edge',look.lineColor]]) box.style.setProperty(k,v);
          svg.querySelectorAll('.node').forEach(n=>{ const c=colors[(n.textContent||'').trim()]; const p=c&&rolePaint(c.toLowerCase().startsWith('#')?c:c.toLowerCase(),th.role); if(!p) return;
            const shape=n.querySelector('.label-container')||n.querySelector(':scope > :is(rect,polygon,circle,ellipse,path)'); if(shape) shape.style.setProperty('fill',p.bg,'important');
            n.querySelectorAll('text,tspan,.nodeLabel,.nodeLabel *,foreignObject *').forEach(t=>{t.style.setProperty('fill',p.fg,'important');t.style.setProperty('color',p.fg,'important');}); }); };
          box.__magiPaint=paint; paint(); } svg.setAttribute('aria-label',say('Mermaid diagram','Mermaid 다이어그램'));
        canvas.replaceChildren(svg); fit();
        if(/^\s*(?:flowchart|graph)\b/m.test(source) && /^\s*%% magi-animate\s*$/m.test(source)) {
          const label=document.createElement('label'), toggle=document.createElement('input');toggle.type='checkbox';label.className='magi-diagram-toggle';
          toggle.checked=!matchMedia('(prefers-reduced-motion: reduce)').matches;
          flowOn=()=>toggle.checked;
          toggle.addEventListener('change',flowPaint);flowPaint();
          label.append(toggle,document.createTextNode(say('Animate flow','흐름 애니메이션')));bar.append(label);
        } note.textContent=say('Select a node to prepare a question.','노드를 선택하면 질문을 준비할 수 있습니다.');
        if(Object.keys(flows).length && /^\s*(?:flowchart|graph)\b/m.test(source)) {
          // The reader picks which case moves: radio (one), checkbox (several) or dropdown (one of many).
          const names=Object.keys(flows), group=document.createElement('div');
          group.className='magi-diagram-cases'; group.setAttribute('aria-label',say('Flow cases','흐름 케이스'));
          const pick=list=>{ activeCases=list.filter(n=>flows[n]); remember({cases:activeCases});
            flowPaint(); };
          const was=(kept.cases||[]).filter(n=>flows[n]);
          const choices=[['',say('All','전체')],...names.map(n=>[n,n])];
          if(flowControl==='dropdown') {
            const sel=document.createElement('select'); sel.setAttribute('aria-label',say('Flow case','흐름 케이스'));
            for(const [v,t] of choices){ const o=document.createElement('option'); o.value=v; o.textContent=t; sel.append(o); }
            sel.addEventListener('change',()=>pick(sel.value?[sel.value]:[])); casesReset=()=>{ sel.value=''; }; if(was[0]) sel.value=was[0]; group.append(sel);
          } else if(flowControl==='checkbox') {
            const boxes=names.map(n=>{ const l=document.createElement('label'); l.className='magi-diagram-toggle';
              const c=document.createElement('input'); c.type='checkbox'; c.value=n; c.checked=was.includes(n);
              c.addEventListener('change',()=>pick(boxes.filter(b=>b.checked).map(b=>b.value)));
              l.append(c,document.createTextNode(n)); group.append(l); return c; });
            casesReset=()=>boxes.forEach(b=>{ b.checked=false; });
          } else {
            group.setAttribute('role','radiogroup'); const rname='magiCase'+(++seq);
            const radios=choices.map(([v,t])=>{ const l=document.createElement('label'); l.className='magi-diagram-toggle';
              const r=document.createElement('input'); r.type='radio'; r.name=rname; r.value=v; r.checked=was[0]?v===was[0]:v==='';
              r.addEventListener('change',()=>{ if(r.checked) pick(v?[v]:[]); });
              l.append(r,document.createTextNode(t)); group.append(l); return r; });
            casesReset=()=>radios.forEach(r=>{ r.checked=r.value===''; });
          }
          bar.append(group);
          activeCases=flowControl==='checkbox'?was:was.slice(0,1);
        }
        svg.querySelectorAll('.node,.actor,.classGroup,.stateGroup').forEach(n=>{
          n.setAttribute('tabindex','0'); n.setAttribute('role','button'); n.setAttribute('aria-pressed','false');
          const select=()=>{
            if(selectedNode && selectedNode!==n){selectedNode.classList.remove('magi-selected');selectedNode.setAttribute('aria-pressed','false');}
            n.classList.add('magi-selected');n.setAttribute('aria-pressed','true');selectedNode=n;remember({node:(n.textContent||'').trim()});flowPaint();
            const next=(n.textContent||'').trim();
            if(next!==selected){question.value='';question.placeholder=say('Explain this diagram element: ','이 다이어그램 요소를 설명해 주세요: ')+next;}
            manualCopy.hidden=true;selected=next;ask.disabled=!selected;info.hidden=!selected;heading.textContent=selected;
            description.textContent=descriptions[selected]||say('No explanation was supplied for this element. You can ask a follow-up question below.','이 요소의 설명이 제공되지 않았습니다. 아래에서 추가 질문을 작성할 수 있습니다.');
            note.textContent=say('Selected: ','선택: ')+selected;if(window.magiDiagramComposer){questionLabel.hidden=true;ask.hidden=true;manualCopy.hidden=true;note.textContent=say('Added to the message: ','메시지에 넣었습니다: ')+selected;}if(selected) window.dispatchEvent(new CustomEvent('magi-diagram-select',{detail:{label:selected}}));fit();
          };
          const followup=e=>{e.preventDefault();select();question.focus();};
          n.addEventListener('contextmenu',followup);
          n.addEventListener('keydown',e=>{if(e.key==='ContextMenu'||(e.shiftKey&&e.key==='F10')) followup(e);});
          n.addEventListener('click',select);n.addEventListener('keydown',e=>{if(e.key==='Enter'||e.key===' '){e.preventDefault();select();}});
        });
        // Bring back what was chosen before the re-mount, without announcing the node again (its chip
        // is already in the composer).
        if(kept.node){ const back=[...svg.querySelectorAll('.node,.actor,.classGroup,.stateGroup')].find(n=>(n.textContent||'').trim()===kept.node);
          if(back){ back.classList.add('magi-selected'); back.setAttribute('aria-pressed','true'); selectedNode=back; selected=kept.node; } }
        flowPaint();
      } catch(e) { note.textContent=say('Diagram could not be rendered: ','다이어그램을 표시할 수 없습니다: ')+e.message; sourceBox.open=true; }
    });
  }
  function scan() {
    document.querySelectorAll('pre > code').forEach(code=>{
      const lang=code.getAttribute('data-lang')||code.getAttribute('data-language')||(code.className||'').replace(/^language-/,'');
      if(lang.trim().toLowerCase()!=='mermaid') return;
      // A fence still streaming shows a box holding its place, never its source; the finished
      // fence is mounted below and replaces the box.
      if(code.getAttribute('data-streaming')==='true') { const box=code.parentElement; if(box && !box.classList.contains('magi-diagram-pending')) { box.classList.add('magi-diagram-pending'); box.setAttribute('data-magi-pending','\u25C7 '+say('The diagram is drawn when the answer finishes','답이 끝나면 다이어그램을 그립니다')); } return; }
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
  // A client hands over the palette its daemon resolved from the person's [theme] (status.theme:
  // {dark,light}, role name to hex); diagrams already drawn are repainted.
  function setTheme(t) {
    hostTheme=t && typeof t==='object' ? {dark:t.dark||null, light:t.light||null} : null;
    document.querySelectorAll('figure.magi-diagram').forEach(b=>{ if(typeof b.__magiPaint==='function') b.__magiPaint(); });
  }
  // A host sets magiDiagramComposer when its own composer takes a selected node; it may do so
  // before or after this script, so the flag is watched rather than read once.
  let composerHost=!!window.magiDiagramComposer;
  const markComposer=()=>{ if(composerHost) document.documentElement.classList.add('magi-composer-host'); };
  try { Object.defineProperty(window,'magiDiagramComposer',{configurable:true,get:()=>composerHost,set:v=>{ composerHost=!!v; markComposer(); }}); } catch { /* keep the plain flag */ }
  markComposer();
  window.magiMermaid={mount,scan,setTheme};
  // A page that received its palette before this script loaded left it here.
  if(window.magiDiagramTheme) setTheme(window.magiDiagramTheme);
  let pending=false;
  const observer=new MutationObserver(()=>{if(!pending){pending=true;setTimeout(()=>{pending=false;scan();},100);}});
  function start(){observer.observe(document.body,{childList:true,subtree:true,characterData:true,attributes:true,attributeFilter:['data-streaming']});scan();}
  if(document.body) start();else document.addEventListener('DOMContentLoaded',start,{once:true});
})();
