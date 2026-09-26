/**
 * Renders the HTML for the chat webview.
 * This module has no dependencies on vscode, fs, or the workspace environment,
 * allowing it to be shared directly between the extension host and test runners.
 */

export interface RenderChatHtmlOptions {
  nonce: string;
  cspSource: string;
  scriptUri: string;
  adapterUri: string;
  /**
   * The view — what used to be a 573-line inline script inside this template literal.
   *
   * It lived here as JavaScript written inside a TypeScript string, so TypeScript never saw it: no
   * types, no checks, and two traps that the build could not catch. A backtick in a comment closed
   * the template and broke the build (TS1443 — hit three times). A piece of TypeScript syntax
   * (as const) was not stripped, shipped as-is, and the browser refused the whole script at parse
   * time — the conversation stopped sending AND receiving, with nothing on screen to say why. It is
   * now src/web/chat_view.ts, type-checked under strict and bundled like the adapter.
   */
  viewUri: string;
}

export function renderChatHtml(options: RenderChatHtmlOptions): string {
  const { nonce, cspSource, scriptUri, adapterUri, viewUri } = options;
  const csp = `default-src 'none'; style-src ${cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}' ${cspSource};`;
  return `<!DOCTYPE html><html lang="ko"><head>
<meta charset="UTF-8">
<title>MAGI Chat</title>
<meta http-equiv="Content-Security-Policy" content="${csp}">
<style>
  /* Every colour is the editor's. Nothing here picks one. */
  .sr-only { position:absolute; width:1px; height:1px; padding:0; margin:-1px;
             overflow:hidden; clip:rect(0,0,0,0); white-space:nowrap; border-width:0; }
  body { margin:0; font-family:var(--vscode-font-family); font-size:var(--vscode-font-size);
         color:var(--vscode-foreground); background:var(--vscode-panel-background);
         display:flex; flex-direction:column; height:100vh; }
  [hidden] { display:none !important; }
  #rows { margin:0; padding:0; }
  #scroll { flex:1; min-height:0; overflow-y:auto; padding:8px 10px; }
  .row { margin:0 0 8px; white-space:pre-wrap; word-break:break-word; }
  .who { font-size:.85em; color:var(--vscode-descriptionForeground); margin-bottom:2px; }
  .user { border-left:2px solid var(--vscode-focusBorder); padding-left:8px; }
  .pending { opacity:.75; }
  /* Parked, not being worked on. Its own mark because "asked and waiting" and "shelved until
     this turn ends" draw the same bar otherwise, and a person cannot tell which they typed. */
  .queued .who::after { content:' ⏸'; }
  .agent { white-space:normal; }
  .agent p { margin:0 0 6px; white-space:pre-wrap; word-break:break-word; }
  .agent p:last-child { margin-bottom:0; }
  .agent pre { margin:6px 0; padding:6px 8px; background:var(--vscode-editor-background);
    border:1px solid var(--vscode-widget-border, rgba(128,128,128,0.25)); border-radius:4px;
    overflow-x:auto; font-family:var(--vscode-editor-font-family, monospace); font-size:.9em;
    white-space:pre; }
  .agent code { font-family:var(--vscode-editor-font-family, monospace); font-size:.9em;
    background:var(--vscode-editor-background); padding:1px 4px; border-radius:3px; }
  .agent pre code { background:transparent; padding:0; }
  .agent ul, .agent ol { margin:4px 0 6px; padding-left:20px; }
  .agent li { margin:2px 0; }
  .agent blockquote { margin:4px 0; padding:2px 8px; border-left:3px solid var(--vscode-focusBorder);
    opacity:.85; }
  .agent hr { border:none; border-top:1px solid var(--vscode-widget-border, rgba(128,128,128,0.3));
    margin:8px 0; }
  .agent a { color:var(--vscode-textLink-foreground); text-decoration:none; }
  .agent a:hover { text-decoration:underline; }
  .agent table { border-collapse:collapse; margin:6px 0; width:100%; font-size:.9em; }
  .agent th, .agent td { border:1px solid var(--vscode-widget-border, rgba(128,128,128,0.25));
    padding:4px 8px; text-align:left; }
  .agent th { background:var(--vscode-editor-background); font-weight:600; }
  .agent .diff-add { color:var(--vscode-gitDecoration-addedResourceForeground, #4ec9b0); }
  .agent .diff-del { color:var(--vscode-gitDecoration-deletedResourceForeground, #f14c4c); }
  .agent .diff-hunk { color:var(--vscode-gitDecoration-modifiedResourceForeground, #3794ff); opacity:.8; }
  .abandoned { opacity:.6; text-decoration:line-through; }
  .thinking, .tool { opacity:.75; font-family:var(--vscode-editor-font-family); font-size:.9em; }
  .error { color:var(--vscode-errorForeground); }
  /* A note about the conversation, not something the companion said — a fold, a recovered error.
     It had no rule at all, so it read as the agent's own words with a small label beside it. The
     JetBrains client draws the same rows small, italic and faint; this is that, in this editor's
     tokens. */
  .system { color:var(--vscode-descriptionForeground); font-style:italic; font-size:.9em; }
  /* What a tool was asked to do, beside its name. Dimmer than the name and clipped to one line:
     it is the answer to "which one", not the argument's full text. */
  .args { color:var(--vscode-descriptionForeground); opacity:.85; }
  /* The companion itself, folded away. A gear rather than "…": the card is what this companion is
     RUNNING ON and what you can change about it, and a gear is the word every editor already uses
     for that — "…" says "more of the same", which this is not. */
  #topbar { display:flex; justify-content:space-between; align-items:center; padding:2px 6px 0; }
  .recovery-btn { background:none; border:1px solid var(--vscode-button-border, var(--vscode-panel-border));
    border-radius:2px; color:var(--vscode-descriptionForeground); cursor:pointer; font-size:.85em; padding:2px 6px; }
  .recovery-btn:hover, .recovery-btn[aria-expanded="true"] { color:var(--vscode-foreground); background:var(--vscode-toolbar-hoverBackground, rgba(128,128,128,0.15)); }
  .recovery-btn:focus-visible { outline:1px solid var(--vscode-focusBorder); }
  .recovery-panel { border-bottom:1px solid var(--vscode-panel-border); padding:8px 0 12px; margin-bottom:8px; }
  .recovery-header { display:flex; justify-content:space-between; align-items:center; flex-wrap:wrap; gap:6px; margin-bottom:8px; font-size:.85em; }
  .recovery-notice { color:var(--vscode-editor-foreground, var(--vscode-foreground)); font-weight:600; border-left:3px solid var(--vscode-editorWarning-foreground, #cca700); padding-left:5px; }
  .recovery-scope-label { color:var(--vscode-descriptionForeground); cursor:pointer; display:flex; align-items:center; gap:4px; font-size:.85em; }
  .recovery-empty { font-size:.85em; color:var(--vscode-descriptionForeground); padding:6px 0; font-style:italic; }
  .recovery-items { display:flex; flex-direction:column; gap:8px; }
  .recovery-item { border:1px solid var(--vscode-widget-border, var(--vscode-panel-border)); border-radius:4px;
    padding:8px 10px; background:var(--vscode-editor-background, rgba(0,0,0,0.02)); }
  .recovery-meta { display:flex; justify-content:space-between; align-items:center; flex-wrap:wrap; gap:4px; font-size:.8em; color:var(--vscode-descriptionForeground); margin-bottom:4px; }
  .recovery-title { font-weight:600; font-size:.9em; color:var(--vscode-foreground); margin-bottom:4px; word-break:break-word; }
  .recovery-reason { font-size:.85em; color:var(--vscode-errorForeground); margin-bottom:4px; word-break:break-word; }
  .recovery-preview { font-size:.85em; color:var(--vscode-descriptionForeground); margin-bottom:6px;
    white-space:pre-wrap; word-break:break-word; max-height:3em; overflow:hidden; text-overflow:ellipsis; }
  .recovery-full-text { font-family:var(--vscode-editor-font-family, monospace); font-size:.85em; margin:6px 0;
    padding:6px 8px; background:var(--vscode-editorWidget-background, rgba(128,128,128,0.1));
    border:1px solid var(--vscode-widget-border, var(--vscode-panel-border)); border-radius:3px;
    white-space:pre-wrap; word-break:break-word; }
  .recovery-actions { display:flex; flex-wrap:wrap; gap:6px; align-items:center; margin-top:6px; }
  .recovery-actions button { font-size:.85em; padding:3px 8px; }
  .recovery-actions .copy-btn { color:var(--vscode-button-foreground); background:var(--vscode-button-background); }
  .recovery-actions .copy-btn:hover:not(:disabled) { background:var(--vscode-button-hoverBackground); }
  .recovery-actions .delete-btn { color:var(--vscode-button-secondaryForeground, var(--vscode-foreground));
    background:var(--vscode-button-secondaryBackground, transparent); border:1px solid var(--vscode-button-border, var(--vscode-panel-border)); }
  .recovery-actions .delete-btn:hover:not(:disabled) { background:var(--vscode-button-secondaryHoverBackground, rgba(128,128,128,0.2)); }
  .recovery-actions .fulltext-btn { background:none; border:none; color:var(--vscode-textLink-foreground); cursor:pointer; padding:0 2px; text-decoration:underline; }
  .recovery-actions button:disabled { opacity:.5; cursor:not-allowed; }
  .recovery-confirm-box { display:flex; flex-wrap:wrap; align-items:center; gap:6px; margin-top:6px;
    padding:6px 8px; background:var(--vscode-editorWarning-background, rgba(204,167,0,0.1));
    border:1px solid var(--vscode-editorWarning-foreground, #cca700); border-radius:3px; font-size:.85em; }
  .recovery-confirm-msg { font-weight:600; color:var(--vscode-editor-foreground, var(--vscode-foreground)); flex:1 1 100%; margin-bottom:4px; }
  .recovery-status { font-size:.8em; color:var(--vscode-descriptionForeground); margin-top:4px; }
  #more { background:none; border:none; cursor:pointer; font-size:1.05em; line-height:1;
    color:var(--vscode-descriptionForeground); padding:2px 4px; }
  #more:hover, #more[aria-expanded="true"] { color:var(--vscode-foreground); }
  #info { border:1px solid var(--vscode-panel-border); border-radius:4px; margin:4px 6px;
    padding:6px 8px; font-size:.9em; }
  #info .line { display:flex; align-items:center; gap:6px; margin:3px 0; }
  #info .k { color:var(--vscode-descriptionForeground); min-width:5.5em; }
  #info .v { flex:1; }
  /* The traffic light. The state IS the class, the way the console does it — the colour is this
     sheet's business and the word travels alone. */
  #info .dot { width:.7em; height:.7em; border-radius:50%; flex:none;
    background:var(--vscode-descriptionForeground); }
  #info .working .dot { background:var(--vscode-testing-iconPassed); }
  #info .waiting .dot { background:var(--vscode-editorWarning-foreground); }
  #info .attached .dot { background:var(--vscode-textLink-foreground); }
  #info .not-running .dot, #info .unknown .dot { background:var(--vscode-editorError-foreground); }
  #info button { font-size:.95em; }
  #info .acts { display:flex; flex-wrap:wrap; gap:4px; margin-top:6px; }
  /* The question/permission body sits at the end of the scroll container (#scroll), after the conversation rows.
     No vertical max-height: reports and permission diffs flow naturally with the transcript scroll.
     Long lines wrap with pre-wrap/break-word to prevent breaking panel width. */
  #ask-body { border-top:1px solid var(--vscode-panel-border); margin-top:12px; padding-top:8px; }
  #ask-body .what { font-weight:600; margin-bottom:6px; white-space:pre-wrap; word-break:break-word; }
  #ask-body .at { color:var(--vscode-descriptionForeground); font-weight:normal; }
  #ask-body pre { font-family:var(--vscode-editor-font-family); font-size:.9em; margin:6px 0;
    white-space:pre-wrap; word-break:break-word; }
  #ask-body pre.diff { border-left:2px solid var(--vscode-textLink-foreground); padding:0; margin:6px 0;
    background:var(--vscode-editor-background, rgba(0,0,0,0.02)); border-radius:2px; }
  .diff-line { padding:1px 6px; min-height:1.25em; white-space:pre-wrap; word-break:break-word; }
  .diff-added { background:var(--vscode-diffEditor-insertedLineBackground, rgba(46, 160, 67, 0.18));
    color:var(--vscode-editor-foreground, inherit); }
  .diff-deleted { background:var(--vscode-diffEditor-removedLineBackground, rgba(248, 81, 73, 0.18));
    color:var(--vscode-editor-foreground, inherit); }
  .diff-context { color:var(--vscode-editor-foreground, inherit); }
  .diff-file-header { font-weight:600;
    background:var(--vscode-sideBarSectionHeader-background, rgba(128, 128, 128, 0.15));
    color:var(--vscode-sideBarSectionHeader-foreground, var(--vscode-editor-foreground, inherit));
    border-top:1px solid var(--vscode-panel-border, rgba(128, 128, 128, 0.2));
    margin-top:6px; padding-top:3px; padding-bottom:3px; }
  .diff-file-header:first-child { border-top:none; margin-top:0; }
  .diff-hunk-header { color:var(--vscode-editor-foreground, inherit); font-weight:600;
    margin:4px 0 2px 0; padding-top:2px; padding-bottom:2px;
    background:var(--vscode-editor-lineHighlightBackground, rgba(128, 128, 128, 0.08)); }
  .diff-plain { color:var(--vscode-editor-foreground, inherit); }
  #ask-body .file-target { font-size:.9em; color:var(--vscode-descriptionForeground); margin:4px 0; word-break:break-all; }
  button.file-nav-btn { background:none; border:none; color:var(--vscode-textLink-foreground);
    cursor:pointer; padding:0 2px; font-family:inherit; font-size:inherit;
    text-decoration:underline; text-underline-offset:2px; display:inline; vertical-align:baseline; }
  button.file-nav-btn:hover { color:var(--vscode-textLink-activeForeground); }
  button.file-nav-btn:focus-visible { outline:1px solid var(--vscode-focusBorder, #007fd4); outline-offset:1px; border-radius:2px; }
  .args-toggle-btn { background:none; border:1px solid var(--vscode-button-border, var(--vscode-panel-border));
    border-radius:2px; color:var(--vscode-descriptionForeground); cursor:pointer;
    padding:0 4px; font-size:.8em; line-height:1.2; vertical-align:baseline; display:inline-block; }
  .args-toggle-btn:hover { color:var(--vscode-foreground); background:var(--vscode-toolbar-hoverBackground, rgba(128, 128, 128, 0.15)); }
  .output-open-btn { background:none; border:1px solid var(--vscode-button-border, var(--vscode-panel-border));
    border-radius:2px; color:var(--vscode-descriptionForeground); cursor:pointer;
    padding:0 4px; font-size:.8em; line-height:1.2; vertical-align:baseline; display:inline-block; margin-left:6px; }
  .output-open-btn:hover:not(:disabled) { color:var(--vscode-foreground); background:var(--vscode-toolbar-hoverBackground, rgba(128, 128, 128, 0.15)); }
  .output-open-btn:disabled { opacity:.5; cursor:not-allowed; }
  pre.raw-args { font-family:var(--vscode-editor-font-family); font-size:.85em; margin:4px 0 6px;
    padding:4px 6px; background:var(--vscode-editor-background, rgba(0, 0, 0, 0.02));
    border-left:2px solid var(--vscode-textLink-foreground); border-radius:2px;
    white-space:pre-wrap; word-break:break-word; }
  .file-open-tag { font-size:.85em; padding:1px 6px; margin-left:6px; border-radius:2px;
    border:1px solid var(--vscode-button-border, var(--vscode-panel-border));
    color:var(--vscode-button-secondaryForeground, var(--vscode-foreground));
    background:var(--vscode-button-secondaryBackground, transparent); cursor:pointer; }
  .file-open-tag:hover { background:var(--vscode-button-secondaryHoverBackground, rgba(128, 128, 128, 0.2)); }
  #ask-body .unstated { color:var(--vscode-editorWarning-foreground); font-size:.9em; margin:4px 0; }
  #ask-body .ground { font-size:.9em; margin:4px 0; white-space:pre-wrap; word-break:break-word; }
  #ask-body .ground b { color:var(--vscode-descriptionForeground); font-weight:600; }
  #ask-body ol.choices { margin:6px 0 6px 20px; padding:0; font-size:.9em; }
  #ask-body ol.choices.hide-marker { list-style:none; margin-left:4px; }
  #ask-body ol.choices li { margin:2px 0; white-space:pre-wrap; word-break:break-word; }
  /* The response controls stay outside the scroll container, fixed directly above the composer.
     The summary row stays permanently pinned while the button group (.acts) is capped at 25vh
     with overflow-y:auto so extensive option lists do not dominate the panel.
     Buttons flex-wrap in narrow sidebars. */
  #ask-controls { border-top:1px solid var(--vscode-panel-border); padding:6px 10px;
    display:flex; flex-direction:column; gap:6px; flex-shrink:0; }
  #ask-controls .summary-row { display:flex; align-items:center; justify-content:space-between; gap:8px; font-size:.85em; flex-shrink:0; }
  #ask-controls .summary-text { color:var(--vscode-descriptionForeground); overflow:hidden;
    text-overflow:ellipsis; white-space:nowrap; flex:1; }
  #ask-controls .ask-status { font-size:.85em; color:var(--vscode-editor-foreground, var(--vscode-foreground));
    flex:none; font-weight:500; margin:0 4px; border-left:2px solid var(--vscode-editorWarning-foreground, #cca700); padding-left:4px; }
  #ask-controls .ask-status:empty { display:none; }
  #ask-controls .jump-btn { background:none; border:none; color:var(--vscode-textLink-foreground);
    cursor:pointer; padding:0; font-size:inherit; flex:none; text-decoration:none; }
  #ask-controls .jump-btn:hover { text-decoration:underline; }
  #ask-controls .jump-btn:focus-visible { outline:1px solid var(--vscode-focusBorder, #007fd4); outline-offset:1px; border-radius:2px; }
  #ask-controls .acts { display:flex; flex-wrap:wrap; gap:6px; max-height:25vh; overflow-y:auto; padding:4px; scroll-padding:4px; }
  #ask-controls .acts button { flex:0 1 auto; max-width:100%; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; scroll-margin:4px; }
  #ask-controls .acts button:disabled { opacity:.5; cursor:not-allowed; }
  #bar button:disabled { opacity:.5; cursor:not-allowed; }
  #ask-controls .acts button.approval-btn {
    color:var(--vscode-button-foreground, #ffffff);
    background:var(--vscode-button-background, #0e639c);
    border:1px solid var(--vscode-contrastBorder, var(--vscode-button-border, transparent));
  }
  #ask-controls .acts button.approval-btn:hover:not(:disabled) {
    background:var(--vscode-button-hoverBackground, #1177bb);
  }
  #ask-controls .acts button.approval-btn:focus-visible {
    outline:1px solid var(--vscode-focusBorder, #007fd4);
    outline-offset:2px;
  }
  #ask-controls .acts button.inspect-btn {
    color:var(--vscode-button-secondaryForeground, var(--vscode-foreground, #ffffff));
    background:var(--vscode-button-secondaryBackground, #3a3d41);
    border:1px solid var(--vscode-contrastBorder, var(--vscode-button-border, transparent));
  }
  #ask-controls .acts button.inspect-btn:hover:not(:disabled) {
    background:var(--vscode-button-secondaryHoverBackground, #45494e);
  }
  #ask-controls .acts button.inspect-btn:focus-visible {
    outline:1px solid var(--vscode-focusBorder, #007fd4);
    outline-offset:2px;
  }
  #ask-controls .acts button.inspect-btn:disabled {
    opacity:.5;
    cursor:not-allowed;
  }
  /* A failure's own words. Its colour is the editor's error colour — the same meaning the glyph
     carries, so the two cannot say different things. */
  .out { color:var(--vscode-errorForeground); font-size:.9em; white-space:pre-wrap; margin-top:2px; }
  /* What a verdict stands on, and what it says to keep — two different kinds of text.
     ⚠ No backticks in this block: it is inside a template literal and one closes it.
     The cite is a FRAGMENT OF THE RECORD: measured against a live run, nine of twelve were diffs,
     leading minus/plus/space and all. This file states the rule for that a few lines up —
     "Monospace and scrollable: it is a command or a patch, and a wrapped one is a different
     command to read" — and it applies here for the same reason: the core keeps this checkable
     (magi looks the fragment up in what the member was shown), and a reader can only check what
     is drawn as it is. Capped, because one member's evidence must not push the round off screen.
     The keep is the member's own prose, so it stays in the reading font. */
  .cite { font-family:var(--vscode-editor-font-family); font-size:.9em; color:var(--vscode-descriptionForeground); margin-top:2px;
    max-height:9em; overflow:auto; }
  .keep { font-size:.9em; color:var(--vscode-descriptionForeground); margin-top:2px; }
  /* Keep reasoning in the outer transcript flow, including its original line breaks. */
  .thought { font-family:var(--vscode-editor-font-family); font-size:.9em; opacity:.6;
    margin-top:2px; white-space:pre-wrap; }
  /* An image row carries a path, not the picture — the same font as a tool row, because that is
     what it is: something a tool produced, with a place to find it. */
  .image { opacity:.75; font-family:var(--vscode-editor-font-family); font-size:.9em; }
  .council { border-left:2px solid var(--vscode-textLink-foreground); padding-left:8px; }
  /* 세로 여백이 4px 인 이유: 둘이 **각각** 서므로 6px 이면 위아래로 24px 이 안내 두 줄에 붙는다.
     실측(1200×200)에서 그 잔여가 바깥 오버플로 2px 로 남았다 — 안내는 줄어들어도 여백은 안
     줄기 때문이다. 하나를 숨겨서 풀지 않는다. */
  /* 행이 설 자리에 선다. ⚠ rows·ask-body 를 감싸거나 지우지 않는다 — 형제로 둔다.
     그 둘을 건드리면 질문 렌더링이 같이 깨진다. */
  #empty-note { margin:0; padding:24px 10px; text-align:center;
    color:var(--vscode-descriptionForeground); font-size:.95em; }
  #note, #state-note { padding:4px 10px; color:var(--vscode-descriptionForeground); font-size:.9em;
    /* ⚠ 낮은 판에서 **입력줄을 밀어내지 않는다.** 이 둘은 flex 자식인데 기본 min-height 가 auto 라
       내용만큼 자리를 붙들고, 그만큼 입력줄이 화면 밖으로 나간다. 실측(1200×200): 지속 안내가
       서자 입력줄 아래끝이 236px — 뷰포트는 200 이었다. 전사는 이미 16px 까지 줄어 더 내줄 것이
       없었다. **한쪽을 숨겨서 풀지 않는다** — 둘은 서로를 부정하지 않으므로 같이 서야 하고,
       대신 줄어들며 스크롤된다. 글자는 남고, 나가는 길(시작 단추)도 닿을 수 있다. */
    min-height:0; overflow-y:auto; }
  /* 비어 있으면 자리를 안 차지한다 — 둘이 각각 서므로, 빈 칸 둘이 입력줄을 밀면 안 된다. */
  #note:empty, #state-note:empty { display:none; }
  #hint { padding:0 10px 4px; font-size:.85em; color:var(--vscode-descriptionForeground); font-family:var(--vscode-editor-font-family); }
  #refs { display:flex; flex-wrap:wrap; gap:4px; padding:0 10px 6px; }
  .chip { font-size:.85em; padding:1px 6px; border-radius:9px;
          color:var(--vscode-badge-foreground); background:var(--vscode-badge-background); }
  #reply-mode { display:flex; justify-content:space-between; align-items:center; padding:4px 10px;
    font-size:.85em; background:var(--vscode-editorWidget-background, #252526);
    border-top:1px solid var(--vscode-panel-border, #333); color:var(--vscode-editorWidget-foreground, var(--vscode-foreground, #ccc)); }
  #reply-mode .reply-tag { font-weight:600; color:var(--vscode-editorWidget-foreground, var(--vscode-foreground)); margin-right:6px; flex:none;
    border-left:3px solid var(--vscode-editorWarning-foreground, #cca700); padding-left:4px; }
  #reply-mode .reply-target { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; flex:1; }
  #reply-mode .cancel-btn { background:transparent; color:var(--vscode-textLink-foreground, #3794ff); border:none; padding:0 4px;
    font-size:inherit; cursor:pointer; flex:none; margin-left:8px; }
  #reply-mode .cancel-btn:hover { text-decoration:underline; }
  #bar { display:flex; gap:6px; align-items:center; padding:8px 10px;
         border-top:1px solid var(--vscode-panel-border); }
  #say { flex:1; resize:none; min-height:2.2em; max-height:8em;
         font-family:inherit; font-size:inherit;
         color:var(--vscode-input-foreground); background:var(--vscode-input-background);
         border:1px solid var(--vscode-input-border, transparent); border-radius:2px; padding:4px 6px; }
  /* ⚠ **고대비 테마에서 채워진 버튼의 경계는 색이 아니라 선이 만든다.** 이 규칙은 border:none 이었고,
     고대비에서 --vscode-button-background 는 패널 배경과 같은 #000000 이다 — 그래서 Send 와 모든
     선택 버튼이 **검정 배경 위의 테두리 없는 검정 사각형**이 됐다(실측 2026-09-19: 배경도 버튼도
     rgb(0, 0, 0), 테두리 0px none). 글자는 흰색이라 대비는 완벽하고, axe-core 감사 42회는 위반 0을
     냈다 — 사각형이 어디서 시작하고 끝나는지를 재는 규칙이 없었기 때문이다.
     같은 파일의 .approval-btn·.inspect-btn 은 이미 이 사슬을 쓴다. 기본 규칙만 빠져 있었다. */
  button { color:var(--vscode-button-foreground); background:var(--vscode-button-background);
           border:1px solid var(--vscode-contrastBorder, var(--vscode-button-border, transparent));
           border-radius:2px; padding:4px 10px; cursor:pointer; }
  button:hover { background:var(--vscode-button-hoverBackground); }
</style></head><body>
<header id="topbar" aria-label="도구 모음"><button id="recovery-btn" class="recovery-btn" type="button" aria-expanded="false" aria-controls="recovery-panel">복구 초안 0</button><button id="more" title="This companion" aria-label="This companion" aria-expanded="false">⚙</button></header>
<aside id="info" aria-label="컴패니언 정보" hidden></aside>
<main id="scroll"><h1 class="sr-only">MAGI Chat</h1><div id="recovery-panel" class="recovery-panel" hidden><div class="recovery-header"><span class="recovery-notice">이 창에서 임시 보관 중</span><label class="recovery-scope-label"><input type="checkbox" id="recovery-scope-all"> 이 컴패니언의 다른 대화</label></div><div id="recovery-items" class="recovery-items"></div><div id="recovery-status" class="recovery-status" aria-live="polite"></div></div><div id="rows"></div><p id="empty-note" hidden></p><div id="ask-body" hidden></div></main>
<section id="ask-controls" aria-label="질문 및 승인 조작" hidden></section><div id="state-note" role="region" aria-label="컴패니언 상태"></div><div id="note" role="region" aria-label="안내 메시지"></div><div id="refs" role="region" aria-label="참조 목록"></div>
<div id="hint" role="region" aria-label="단축키 힌트"></div>
<section id="reply-mode" aria-label="답변 모드" hidden><span class="reply-tag">[답변 모드]</span><span id="reply-target" class="reply-target"></span><button id="reply-cancel" class="cancel-btn" title="일반 입력으로 전환 (Esc)">✕ 취소</button></section>
<footer id="bar" aria-label="메시지 작성"><textarea id="say" rows="1" aria-label="Message the companion"></textarea><button id="send">Send</button></footer>
<script nonce="${nonce}" src="${scriptUri}"></script>
<script nonce="${nonce}" src="${adapterUri}"></script>
<script nonce="${nonce}" src="${viewUri}"></script></body></html>`;
}
