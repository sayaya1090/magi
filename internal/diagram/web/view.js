/* Shared local Mermaid presentation. No diagram-provided callbacks or URLs are executed. */
(() => {
  if (window.magiMermaid) return;
  const ko = (document.documentElement.lang || navigator.language || '').startsWith('ko');
  const say = (en, kr) => ko ? kr : en;
  let seq = 0, queue = Promise.resolve();
  const cache = new Map();
  function allowed(source) {
    if (source.length > 24000) throw Error(say('Diagram exceeds 24,000 characters.', '다이어그램은 24,000자를 넘을 수 없습니다.'));
    if (/%%\s*\{|^\s*---|^\s*click\s|<\s*\/?\s*[a-z]|https?:\/\//im.test(source))
      throw Error(say('Use plain labels without HTML, links, click directives or configuration.', 'HTML, 링크, click 지시문, 설정 없이 일반 라벨을 사용해 주세요.'));
  }
  function mount(pre, source) {
    if (pre.dataset.magiDiagram === source) return;
    pre.dataset.magiDiagram = source;
    const box = document.createElement('figure');
    box.className = 'magi-diagram';
    box.style.cssText = 'margin:8px 0;padding:8px;border:1px solid currentColor;border-radius:8px;max-width:100%;overflow:hidden;color:inherit';
    const bar = document.createElement('div'); bar.style.cssText = 'display:flex;gap:6px;flex-wrap:wrap';
    const canvas = document.createElement('div'); canvas.style.cssText = 'overflow:auto;max-height:65vh;min-height:48px';
    const note = document.createElement('div'); note.setAttribute('role','status'); note.style.cssText = 'white-space:pre-wrap;font:inherit';
    const sourceBox = document.createElement('details'), summary = document.createElement('summary'), code = document.createElement('pre');
    summary.textContent = say('Source', '원본'); code.textContent = source; sourceBox.append(summary, code);
    let zoom = 1, svg;
    function button(en, kr, action) {
      const b = document.createElement('button'); b.type='button'; b.textContent=say(en,kr);
      b.style.cssText='font:inherit;color:inherit;background:transparent;border:1px solid currentColor;border-radius:6px;padding:6px 10px;min-height:32px;cursor:pointer';
      b.onclick=action; bar.append(b); return b;
    }
    function scale(n) { zoom=Math.max(.25,Math.min(4,n)); if(svg) { svg.style.width=(zoom*100)+'%'; svg.style.maxWidth='none'; } }
    button('+','+',()=>scale(zoom*1.25)); button('−','−',()=>scale(zoom/1.25)); button('Fit','맞춤',()=>scale(1));
    async function copy(text) {
      try { await navigator.clipboard.writeText(text); note.textContent=say('Copied.','복사했습니다.'); }
      catch { sourceBox.open=true; note.textContent=say('Clipboard unavailable. Select and copy the source below.','클립보드를 사용할 수 없습니다. 아래 원본을 선택해 복사해 주세요.'); }
    }
    button('Copy source','원본 복사',()=>copy(source));
    let selected='';
    const ask=button('Copy question','질문 복사',()=>copy(say('Explain this diagram element: ','이 다이어그램 요소를 설명해 주세요: ')+selected+'\n\n```mermaid\n'+source+'\n```')); ask.disabled=true;
    box.append(bar,canvas,note,sourceBox); pre.replaceWith(box);
    note.textContent=say('Rendering diagram…','다이어그램을 그리는 중…');
    queue=queue.catch(()=>{}).then(async()=>{
      if (!box.isConnected) return;
      try {
        allowed(source);
        let rendered=cache.get(source);
        if (!rendered) {
          window.mermaid.initialize({startOnLoad:false,securityLevel:'strict',htmlLabels:false,theme:'neutral',maxTextSize:24000,maxEdges:200,flowchart:{htmlLabels:false},suppressErrorRendering:true});
          rendered=(await window.mermaid.render('magiDiagram'+(++seq),source)).svg;
          if(cache.size>=32) cache.delete(cache.keys().next().value);
          cache.set(source,rendered);
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
        svg=document.importNode(parsed.documentElement,true); svg.setAttribute('role','img'); svg.setAttribute('aria-label',say('Mermaid diagram','Mermaid 다이어그램'));
        canvas.replaceChildren(svg); scale(1); note.textContent=say('Select a node to prepare a question.','노드를 선택하면 질문을 준비할 수 있습니다.');
        svg.querySelectorAll('.node,.actor,.classGroup,.stateGroup').forEach(n=>{
          n.setAttribute('tabindex','0'); n.setAttribute('role','button');
          const select=()=>{selected=(n.textContent||'').trim(); ask.disabled=!selected; note.textContent=say('Selected: ','선택: ')+selected;};
          n.addEventListener('click',select);n.addEventListener('keydown',e=>{if(e.key==='Enter'||e.key===' '){e.preventDefault();select();}});
        });
      } catch(e) { note.textContent=say('Diagram could not be rendered: ','다이어그램을 표시할 수 없습니다: ')+e.message; sourceBox.open=true; }
    });
  }
  function scan() {
    document.querySelectorAll('pre > code').forEach(code=>{
      const lang=code.getAttribute('data-lang')||code.getAttribute('data-language')||(code.className||'').replace(/^language-/,'');
      if(lang.trim().toLowerCase()==='mermaid' && code.getAttribute('data-streaming')!=='true') mount(code.parentElement,code.textContent||'');
    });
  }
  window.magiMermaid={mount,scan};
  let pending=false;
  const observer=new MutationObserver(()=>{if(!pending){pending=true;setTimeout(()=>{pending=false;scan();},100);}});
  function start(){observer.observe(document.body,{childList:true,subtree:true,characterData:true});scan();}
  if(document.body) start();else document.addEventListener('DOMContentLoaded',start,{once:true});
})();
