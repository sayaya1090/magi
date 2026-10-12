/** Group conversation controls and provide immediate pointer/keyboard descriptions. */
export function mountIconControls(root = document) {
  const advanced = root.querySelector('#advanced');
  if (advanced && !advanced.querySelector('.advanced-tools')) {
    const tools = root.createElement('div'); tools.className = 'advanced-tools';
    tools.setAttribute('role', 'group'); tools.setAttribute('aria-label', '대화 작업');
    for (const [label, ids] of [['대화', ['fresh']], ['규칙과 가이드', ['rules', 'guides']], ['설정과 연결', ['council', 'repick']]]) {
      const group = root.createElement('div'); group.className = 'control-group';
      group.setAttribute('role', 'group'); group.setAttribute('aria-label', label);
      for (const id of ids) { const button = advanced.querySelector('#' + id); if (button) group.append(button); }
      if (group.childElementCount) tools.append(group);
    }
    advanced.insertBefore(tools, advanced.querySelector('#pick-model'));
  }
  const hint = root.createElement('div'); hint.id = 'icon-control-tooltip'; hint.className = 'icon-control-tooltip';
  hint.setAttribute('role', 'tooltip'); hint.hidden = true; root.body.append(hint);
  let current = null; let savedTitle = null; let timer = null;
  const observer = new MutationObserver(() => {
    if (!current) return;
    const title = current.getAttribute('title');
    if (title) { savedTitle = title; hint.textContent = title; current.removeAttribute('title'); }
    else if (current.getAttribute('aria-label')) hint.textContent = savedTitle || current.getAttribute('aria-label');
    position();
  });
  const clearTimer = () => { if (timer !== null) clearTimeout(timer); timer = null; };
  function position() {
    if (!current || current.getClientRects().length === 0) { hide(); return; }
    const b = current.getBoundingClientRect(); const r = hint.getBoundingClientRect();
    const w = root.documentElement.clientWidth; const h = root.documentElement.clientHeight;
    const left = Math.max(8, Math.min(b.x + (b.width - r.width) / 2, w - r.width - 8));
    const top = b.bottom + r.height + 8 <= h ? b.bottom + 8 : Math.max(8, b.top - r.height - 8);
    hint.style.left = left + 'px'; hint.style.top = top + 'px';
  }
  function hide() {
    clearTimer(); observer.disconnect(); hint.hidden = true;
    if (current) {
      const ids = (current.getAttribute('aria-describedby') || '').split(/\s+/).filter(id => id && id !== hint.id);
      if (ids.length) current.setAttribute('aria-describedby', ids.join(' ')); else current.removeAttribute('aria-describedby');
      if (savedTitle !== null && !current.hasAttribute('title')) current.setAttribute('title', savedTitle);
    }
    current = null; savedTitle = null;
  }
  function show(button) {
    clearTimer(); if (current === button) return;
    hide(); const text = button.getAttribute('title') || button.getAttribute('aria-label');
    if (!text) return;
    current = button; savedTitle = button.getAttribute('title'); hint.textContent = text;
    button.removeAttribute('title');
    const ids = (button.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean);
    button.setAttribute('aria-describedby', [...ids, hint.id].join(' '));
    hint.hidden = false; position();
    observer.observe(button, {attributes: true, attributeFilter: ['title', 'aria-label', 'hidden']});
  }
  const target = event => event.target?.closest?.('button.icon-btn');
  root.addEventListener('pointerover', event => {
    if (hint.contains(event.target)) { clearTimer(); return; }
    const button = target(event); if (button) show(button);
  });
  root.addEventListener('pointerout', event => {
    if (!current || current.contains(event.relatedTarget) || hint.contains(event.relatedTarget)) return;
    timer = setTimeout(hide, 120);
  });
  root.addEventListener('focusin', event => { const button = target(event); if (button) show(button); });
  root.addEventListener('focusout', event => { if (current?.contains(event.target)) hide(); });
  root.addEventListener('pointerdown', hide);
  root.addEventListener('click', hide);
  root.addEventListener('keydown', event => { if (event.key === 'Escape') hide(); });
  root.addEventListener('scroll', hide, true);
  root.defaultView.addEventListener('resize', hide);
  return {hide};
}
