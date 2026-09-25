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
}

export function renderChatHtml(options: RenderChatHtmlOptions): string {
  const { nonce, cspSource, scriptUri, adapterUri } = options;
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
<script nonce="${nonce}">
const vs = acquireVsCodeApi();
const actions = createWebviewActionAdapter(vs);
const scrollEl = document.getElementById('scroll');
const rowsEl = document.getElementById('rows');
const askBodyEl = document.getElementById('ask-body');
const askControlsEl = document.getElementById('ask-controls');
const replyModeEl = document.getElementById('reply-mode');
const replyTargetEl = document.getElementById('reply-target');
const replyCancelEl = document.getElementById('reply-cancel');
const noteEl = document.getElementById('note');
const stateNoteEl = document.getElementById('state-note');
const emptyNoteEl = document.getElementById('empty-note');
const say = document.getElementById('say');
const refsEl = document.getElementById('refs');
const hint = document.getElementById('hint');
const recoveryBtn = document.getElementById('recovery-btn');
const recoveryPanel = document.getElementById('recovery-panel');
const recoveryItemsEl = document.getElementById('recovery-items');
const recoveryScopeAll = document.getElementById('recovery-scope-all');
const recoveryStatus = document.getElementById('recovery-status');
let currentAsk = null;
let currentAskCallId = null;
function askedAt(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const clock = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  const now = new Date();
  const sameDay = d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth()
    && d.getDate() === now.getDate();
  return sameDay ? clock : d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' + clock;
}
function renderDiff(container, text) {
  if (!text || typeof classifyDiffLines !== 'function') return;
  const lines = text.split('\\n');
  if (lines.length > 0 && lines[lines.length - 1] === '' && text.endsWith('\\n')) lines.pop();
  const classified = classifyDiffLines(lines);
  for (let i = 0; i < classified.length; i++) {
    const row = document.createElement('div');
    row.className = 'diff-line ' + classified[i].cls;
    row.textContent = classified[i].text + (i < classified.length - 1 || text.endsWith('\\n') ? '\\n' : '');
    container.append(row);
  }
}
function baseName(p) {
  if (!p) return '';
  const parts = p.split('/');
  const last = parts[parts.length - 1];
  const winParts = last.split('\\\\');
  return winParts[winParts.length - 1] || p;
}
function drawAsk(a) {
  const boundSession = currentSession;
  if (!a) {
    if (answerState.getPendingQuestion()) inputAdapter.exitAnswerMode();
    currentAsk = null;
    currentAskCallId = null;
    askBodyEl.hidden = true;
    askBodyEl.textContent = '';
    askControlsEl.hidden = true;
    askControlsEl.textContent = '';
    askControlsEl.removeAttribute('aria-busy');
    inputAdapter.updateInFlightStatus?.();
    return;
  }
  if (currentAskCallId === a.callId) return;
  if (answerState.getPendingQuestion() && answerState.getPendingQuestion() !== a.callId) inputAdapter.exitAnswerMode();
  currentAsk = a;
  currentAskCallId = a.callId;

  askBodyEl.textContent = '';
  askControlsEl.textContent = '';
  askBodyEl.hidden = false;
  askControlsEl.hidden = false;

  const w = document.createElement('div');
  w.className = 'what';
  askBodyEl.append(w);
  /* Where this sits in the run the call is asking: (3/5). The core says why it travels — a viewer
     "has no other way to know that answering this one leads to another" — and without it somebody
     who answers the first question of five believes they are done. Only when there IS more than
     one: "(1/1)" beside a lone question is noise pretending to be information. */
  if (a.total > 1) {
    const n = document.createElement('span');
    n.className = 'at';
    n.textContent = ' (' + a.index + '/' + a.total + ')';
    w.append(n);
  }
  /* WHEN it was asked. A prompt that went up forty minutes ago while nobody was looking is drawn
     exactly like one you just caused, and those are different situations — the first means a turn
     has been stopped dead since before you stepped away. Drawn as a CLOCK, not as "40m ago":
     nothing redraws this panel while a prompt stands (no events arrive), so an elapsed figure would
     freeze at whatever it said when it was first painted and then quietly lie. The date comes along
     when it is not today, or "14:32" on a prompt from yesterday reads as an hour ago. */
  const when = askedAt(a.since);
  if (when) {
    const t = document.createElement('span');
    t.className = 'at';
    t.textContent = ' asked ' + when;
    w.append(t);
  }

  const sumRow = document.createElement('div');
  sumRow.className = 'summary-row';
  const sumText = document.createElement('span');
  sumText.className = 'summary-text';
  const countTag = a.total > 1 ? ' (' + a.index + '/' + a.total + ')' : '';
  const labelPrefix = a.kind === 'permission' ? '승인 대기: ' : '답변 대기: ';
  const targetPath = (a.filePath || '').trim();
  const fileTag = targetPath ? ' · ' + baseName(targetPath) : '';
  sumText.textContent = labelPrefix + a.what + fileTag + countTag;
  const askStatusEl = document.createElement('span');
  askStatusEl.className = 'ask-status';
  askStatusEl.setAttribute('role', 'status');
  askStatusEl.setAttribute('aria-live', 'polite');
  const jumpBtn = document.createElement('button');
  jumpBtn.className = 'jump-btn';
  jumpBtn.textContent = '질문으로 이동';
  jumpBtn.title = '질문 본문으로 스크롤 이동';
  jumpBtn.addEventListener('click', () => {
    askBodyEl.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  });
  sumRow.append(sumText, askStatusEl, jumpBtn);
  askControlsEl.append(sumRow);

  const acts = document.createElement('div');
  acts.className = 'acts';

  if (a.kind === 'permission') {
    if (answerState.getPendingQuestion()) inputAdapter.exitAnswerMode();
    w.prepend('magi wants to run: ' + a.what);
    if (targetPath) {
      const fileEl = document.createElement('div');
      fileEl.className = 'file-target';
      fileEl.textContent = '파일: ';
      const openBtn = document.createElement('button');
      openBtn.type = 'button';
      openBtn.className = 'file-nav-btn inspect-btn';
      openBtn.textContent = targetPath;
      openBtn.title = '파일 열기 (현재 파일)';
      openBtn.setAttribute('aria-label', '파일 열기 (현재 파일): ' + targetPath);
      if (!boundSession || !a.callId) {
        openBtn.disabled = true;
      } else {
        openBtn.addEventListener('click', () => {
          actions.openFile(boundSession, a.callId);
        });
      }
      fileEl.append(openBtn);
      askBodyEl.append(fileEl);
    }
    /* WHAT is being allowed, not a description of it. Without this a person presses allow knowing
       only the tool's name — the place where the most is riding on the answer was the one drawn
       with the least. The args are the thing itself; the reason is prose about why the policy
       stopped here; the diff is what approving would change. */
    for (const [cls, text] of [['args', a.args], ['reason', a.reason], ['diff', a.diff]]) {
      if (!text) continue;
      const p = document.createElement('pre');
      p.className = cls;
      if (cls === 'diff') {
        renderDiff(p, text);
      } else {
        p.textContent = text;      /* textContent, never innerHTML: this is workspace input */
      }
      askBodyEl.append(p);
    }
    /* Nothing came. Say so — three buttons over a blank space read as "there is nothing to it",
       and that is the reading this must not allow. */
    if (!a.args && !a.reason && !a.diff) {
      const u = document.createElement('div');
      u.className = 'unstated';
      u.textContent = 'the companion did not say what this would do';
      askBodyEl.append(u);
    }
    const canDiff = a.diffKind === 'sides' || a.diffKind === 'patch';
    if (canDiff) {
      const diffBtn = document.createElement('button');
      diffBtn.type = 'button';
      diffBtn.className = 'diff-btn inspect-btn';
      diffBtn.textContent = '변경 보기';
      diffBtn.title = '변경 보기 (승인 당시 비교 자료)';
      diffBtn.setAttribute('aria-label', '변경 보기 (승인 당시 비교 자료)');
      if (!boundSession || !a.callId) {
        diffBtn.disabled = true;
      } else {
        diffBtn.addEventListener('click', () => actions.openDiff(boundSession, a.callId));
      }
      acts.append(diffBtn);
    }
    /* The three words the core spells are the TOKENS — they go on the wire unchanged, so the two
       vocabularies cannot drift. What a person reads is a different thing, and this screen used to
       conflate them: the buttons read allow/deny/always, lowercase English, inside an
       otherwise Korean prompt, with no accessible name of their own — so a screen reader announced
       the wire token. Display and token are separated here; the label is also the accessible name,
       because a control that is read differently from how it looks is its own defect.

       The "always" label says only what the core does (permission.go records the tool for THIS
       session; persisting to project rules is a further step), so it does not promise forever. */
    /* ⚠ This block is INSIDE a template literal — it ships as webview JS text, so TypeScript never
       looks at it and never strips anything. TS syntax written here survives into the bundle and
       kills the whole script at parse time. An "as const" here did exactly that: the conversation
       stopped sending AND receiving, with nothing on screen to say why. Plain JS only. */
    const LABEL = { allow: '허용', deny: '거절', always: '항상 허용' };
    for (const d of ['allow', 'deny', 'always']) {
      const label = LABEL[d];
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'approval-btn decision-' + d;
      b.textContent = label;
      b.setAttribute('aria-label', label);
      b.addEventListener('click', () => actions.answer(a.callId, d));
      acts.append(b);
    }
    askControlsEl.append(acts);
    return;
  }
  /* A question wants a sentence, not a verdict. Options are shortcuts to one. */
  w.prepend(a.what);
  /* The grounds it was asked on. The decision-report skill gathers these and asks in their order,
     and a prompt whose grounds stayed behind is exactly what carrying them exists to stop — the
     person would be deciding from a bare sentence while the reasons sat in another process. */
  for (const g of a.report || []) {
    const row = document.createElement('div');
    row.className = 'ground';
    const k = document.createElement('b');
    k.textContent = g.key + ': ';
    row.append(k, g.text);   /* text as a node, never innerHTML: the model wrote it */
    askBodyEl.append(row);
  }
  const formattedChoices = typeof formatChoiceOptions === 'function'
    ? formatChoiceOptions(a.options)
    : { hideListMarker: false, items: (a.options || []).map((opt, i) => ({ raw: opt, buttonLabel: (i + 1) + '. ' + opt })) };
  if (formattedChoices.items.length) {
    const ol = document.createElement('ol');
    ol.className = formattedChoices.hideListMarker ? 'choices hide-marker' : 'choices';
    for (const item of formattedChoices.items) {
      const li = document.createElement('li');
      li.textContent = item.raw;
      ol.append(li);
    }
    askBodyEl.append(ol);
  }
  for (let i = 0; i < formattedChoices.items.length; i++) {
    const item = formattedChoices.items[i];
    const b = document.createElement('button');
    b.className = 'choice-btn';
    b.textContent = item.buttonLabel;
    b.title = item.raw;
    b.addEventListener('click', () => {
      inputAdapter.submitChoice(a.callId, item.raw);
    });
    acts.append(b);
  }
  const free = document.createElement('button');
  free.className = 'direct-btn';
  free.textContent = '직접 입력';
  free.title = '입력창에서 직접 답변 작성';
  free.addEventListener('click', () => { inputAdapter.enterAnswerMode(a.callId, a.what); });
  acts.append(free);
  askControlsEl.append(acts);
  if (formattedChoices.items.length === 0) {
    inputAdapter.enterAnswerMode(a.callId, a.what);
  }
  inputAdapter.updateInFlightStatus?.();
}
const moreEl = document.getElementById('more');
const infoEl = document.getElementById('info');
let info = null;
moreEl.addEventListener('click', () => {
  const open = infoEl.hidden;
  infoEl.hidden = !open;
  moreEl.setAttribute('aria-expanded', open ? 'true' : 'false');
  if (open) drawInfo();
});
/* Run one of the editor's own commands. The NAME is chosen here and checked on the other side —
   a webview is a page and a page must not be able to name any command it likes. */
function act(command) { actions.act(command); }
function line(k, v, cls) {
  const d = document.createElement('div');
  d.className = 'line' + (cls ? ' ' + cls : '');
  if (cls) { const dot = document.createElement('span'); dot.className = 'dot'; d.append(dot); }
  const kk = document.createElement('span'); kk.className = 'k'; kk.textContent = k;
  const vv = document.createElement('span'); vv.className = 'v'; vv.textContent = v || 'not said';
  d.append(kk, vv);
  return d;
}
function drawInfo() {
  if (infoEl.hidden) return;
  infoEl.textContent = '';
  if (!info) { infoEl.append(line('state', 'asking…')); return; }
  /* The state word IS the class — the stylesheet paints the light, this only says which. */
  infoEl.append(line('now', info.label, info.state));
  infoEl.append(line('build', info.version));
  for (const [k, v, cmd] of [['model', info.model, 'magi.chooseModel'],
                             ['provider', info.backend, 'magi.chooseBackend'],
                             ['approval', info.permission, 'magi.choosePermission']]) {
    const row = line(k, v);
    const b = document.createElement('button');
    b.textContent = 'change';
    b.addEventListener('click', () => act(cmd));
    row.append(b);
    infoEl.append(row);
  }
  if (info.council) infoEl.append(line('council', info.council));
  const acts = document.createElement('div');
  acts.className = 'acts';
  for (const [text, cmd] of [['fold context', 'magi.compact'],
                             ['restart', 'magi.restartDaemon'],
                             ['update', 'magi.updateCore']]) {
    const b = document.createElement('button');
    b.textContent = text;
    b.addEventListener('click', () => act(cmd));
    acts.append(b);
  }
  infoEl.append(acts);
}
const answerState = createAnswerState();
let currentCompanionKey = '';
let currentSession = '';
let currentGeneration = undefined;
let currentWebviewId = '';
const expandedCallIds = new Set();
const inputAdapter = createWebviewInputAdapter({
  say,
  sendBtn: document.getElementById('send'),
  replyModeEl,
  replyTargetEl,
  replyCancelEl,
  noteEl,
  hintEl: hint,
  askControlsEl,
  getCurrentAsk: () => currentAsk,
}, actions, answerState);
const recoveryController = createWebviewRecoveryController({
  elements: {
    recoveryBtn,
    recoveryPanel,
    recoveryItemsEl,
    recoveryScopeAll,
    recoveryStatus,
    say,
  },
  answerState,
  inputAdapter,
  getCurrentCompanionKey: () => currentCompanionKey,
  getCurrentSession: () => currentSession,
});
/* 빈 전사가 말하는 것은 **전사에 대한 사실**뿐이다. 단추는 여기 안 붙는다 — 나가는 길은
   state-note 가 혼자 맡는다(시작 단추가 두 곳에 생기면 같은 일을 두 번 그리는 것이다).
   무엇을 말할지는 코어가 정한다(emptyTranscriptNote). 이 함수는 그리기만 한다.
   (이 스크립트는 템플릿 문자열 안이다 — 주석에 백틱을 쓰면 안 된다.) */
function drawEmptyNote(text) {
  emptyNoteEl.textContent = text || '';
  emptyNoteEl.hidden = !text;
}

function drawState(note) {
  /* ⚠ **지속 상태는 제 자리에 산다.** 이 함수는 오래 note 칸에 썼고, 같은 칸에 "sending…" 같은
     일시 알림도 살았다. 둘이 한 싱크라 서로를 지웠다: 컴패니언이 죽어 「없습니다」와 시작 단추가
     떠 있을 때 무언가 보내면 그것이 덮이고, 4초 뒤 타이머가 지우고, **다음 state 사건이 올 때까지
     안 돌아왔다.** 보고(사건)와 상태(수준)가 한 자리면 뒤가 앞을 지운다. 이제 갈라 둔다 —
     여기는 상태가 바뀔 때만 바뀌고, 일시 알림의 만료는 이 자리를 건드리지 않는다.
     (이 스크립트는 템플릿 문자열 안에 산다 — 주석에도 백틱을 쓰면 안 된다.) */
  stateNoteEl.textContent = '';
  if (!note || !note.text) return;
  /* The words and whether to offer a way out are decided in core (panelNote), so this draws and
     decides nothing. It used to decide: not-running got a button and unknown got a bare sentence,
     which left somebody whose companion could not be reached with nothing to press.

     The parameter is the NOTE, not the state. It used to be handed the state and reach for
     st.note - and the note is a SIBLING of state in the message, not a child of it, so that
     reach was always undefined. Nothing failed: the panel simply never drew the sentence and
     never drew the button, which is the same screen as "everything is fine" and is exactly the
     screen somebody with no companion running was left looking at.
     (No backticks in here: this script lives in a template literal and one would close it.) */
  stateNoteEl.append(note.text + ' ');
  if (note.offerStart) {
    const b = document.createElement('button');
    b.textContent = 'Start one';
    b.addEventListener('click', () => actions.start());
    stateNoteEl.append(b);
  }
  /* idle / working / waiting say nothing here: the status bar already says them, and repeating a
     line above the composer is a line in the way. */
}
function drawRefs(rs) {
  refsEl.textContent = '';
  for (const r of rs || []) {
    const c = document.createElement('span');
    c.className = 'chip';
    c.textContent = r;
    refsEl.append(c);
  }
  if ((rs || []).length) {
    const b = document.createElement('button');
    b.textContent = 'clear';
    b.addEventListener('click', () => actions.drop());
    refsEl.append(b);
  }
}
function draw(rs) {
  const boundSession = currentSession;
  const activeEl = document.activeElement;
  const focusedCallId = (activeEl && activeEl.classList && activeEl.classList.contains('args-toggle-btn')) ? activeEl.dataset.callId : null;
  rowsEl.textContent = '';
  for (const r of rs) {
    const d = document.createElement('div');
    d.className = 'row ' + r.who + (r.pending ? ' pending' : '')
      + (r.queued ? ' queued' : '') + (r.abandoned ? ' abandoned' : '');
    const w = document.createElement('div');
    w.className = 'who';
    w.textContent = r.label;
    if (r.outputId) {
      const boundOutputId = r.outputId;
      const openOutputBtn = document.createElement('button');
      openOutputBtn.type = 'button';
      openOutputBtn.className = 'output-open-btn';
      openOutputBtn.textContent = '편집창에서 열기';
      openOutputBtn.title = '편집창에서 열기 (읽기 전용)';
      openOutputBtn.setAttribute('aria-label', '편집창에서 열기 (읽기 전용)');
      openOutputBtn.dataset.outputId = boundOutputId;
      if (!boundSession) {
        openOutputBtn.disabled = true;
      } else {
        openOutputBtn.addEventListener('click', (ev) => {
          ev.stopPropagation();
          actions.openOutput(boundSession, boundOutputId);
        });
      }
      w.append(' ', openOutputBtn);
    }
    const b = document.createElement('div');
    b.className = 'body';
    if (r.who === 'agent' && typeof renderMarkdown === 'function') {
      renderMarkdown(b, r.text);
    } else {
      b.textContent = r.text;           /* textContent, never innerHTML: the model wrote this */
    }
    /* A tool row names the call AND what it was asked to do. Without the second half a turn that
       runs thirty commands is thirty rows reading the same word, and the transcript cannot answer
       the one question it exists for. Its own element so it can be dimmed and clipped without
       touching the name. */
    /* Why it failed, under the row. The glyph says the shape of the trouble; this says what it
       was, which is what a person opened the transcript for. */
    if (r.who === 'tool' && r.out) {
      const o = document.createElement('div');
      o.className = 'out';
      o.textContent = r.out;
      d.append(o);
    }
    /* What the verdict rests on, and what it says a revision must keep.
       ⚠ No backticks in here: this whole script is a template literal, and one closes it.
       The cite is the fragment magi can look up in the material the member was shown, and the core
       says the case that matters — an empty one on an approval is itself worth seeing, so a done
       standing on nothing must not draw the same as one standing on the record. The keep arrives on
       approvals too, and that is exactly when it is worth reading: it is what a rewrite forced by
       somebody else's objection would otherwise drop. */
    if (r.who === 'council') {
      /* The thought is last and dimmest, because it is the one line here that is NOT a vote: it
         never went through the parser, and reading it with the same weight as the grounds above
         would be reading a model's musing as a finding. It is drawn at all for the silent
         members — a reply that came as reasoning alone left "no answer came back" and nothing
         else, over thousands of characters of work. */
      for (const [cls, label, text] of [['cite', 'on', r.cite], ['keep', 'keep', r.keep],
        ['thought', 'thought (not a vote)', r.thought]]) {
        if (!text) continue;
        const el = document.createElement('div');
        el.className = String(cls);
        el.textContent = label + ': ' + text;
        d.append(el);
      }
    }
    if (r.who === 'tool' && r.fileNav) {
      const a = document.createElement('button');
      a.type = 'button';
      a.className = 'file-nav-btn';
      const loc = r.fileNav.line ? r.fileNav.path + ':' + r.fileNav.line : r.fileNav.path;
      a.textContent = loc;
      a.title = r.fileNav.line ? '파일 열기: ' + loc : '파일 열기: ' + r.fileNav.path;
      a.setAttribute('aria-label', a.title);
      if (!boundSession || !r.callId) {
        a.disabled = true;
      } else {
        a.addEventListener('click', (ev) => {
          ev.stopPropagation();
          actions.openFile(boundSession, r.callId, r.seq);
        });
      }
      b.append(' ', a);
    }
    if (r.who === 'tool' && r.args) {
      const loc = r.fileNav ? (r.fileNav.line ? r.fileNav.path + ':' + r.fileNav.line : r.fileNav.path) : '';
      if (!r.fileNav || (r.args !== r.fileNav.path && r.args !== loc)) {
        const a = document.createElement('span');
        a.className = 'args';
        a.textContent = r.args;
        b.append(' ', a);
      }
    }
    if (r.who === 'tool' && r.rawArgs && r.rawArgs !== r.args) {
      const toggle = document.createElement('button');
      toggle.type = 'button';
      toggle.className = 'args-toggle-btn';
      if (r.callId) toggle.dataset.callId = r.callId;
      const isExpanded = !!(boundSession && r.callId && expandedCallIds.has(r.callId));
      toggle.textContent = isExpanded ? '접기' : '…';
      toggle.title = isExpanded ? '인자 접기' : '인자 전체 펼치기';
      toggle.setAttribute('aria-label', toggle.title);
      toggle.setAttribute('aria-expanded', isExpanded ? 'true' : 'false');
      const rawBox = document.createElement('pre');
      rawBox.className = 'raw-args';
      rawBox.textContent = r.rawArgs;
      rawBox.hidden = !isExpanded;
      toggle.addEventListener('click', (ev) => {
        ev.stopPropagation();
        const open = rawBox.hidden;
        rawBox.hidden = !open;
        toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
        toggle.textContent = open ? '접기' : '…';
        toggle.title = open ? '인자 접기' : '인자 전체 펼치기';
        toggle.setAttribute('aria-label', toggle.title);
        if (boundSession && r.callId) {
          if (open) expandedCallIds.add(r.callId);
          else expandedCallIds.delete(r.callId);
        }
      });
      b.append(' ', toggle);
      d.append(rawBox);
    }
    d.append(w, b);
    rowsEl.append(d);
  }
  if (focusedCallId) {
    const btns = rowsEl.querySelectorAll('.args-toggle-btn');
    for (let i = 0; i < btns.length; i++) {
      if (btns[i].dataset.callId === focusedCallId) {
        btns[i].focus();
        break;
      }
    }
  }
}
const receiveHandlers = createWebviewReceiveHandlers({
  inputAdapter,
  answerState,
  recoveryController,
  getCurrentAsk: () => currentAsk,
  getCurrentSession: () => currentSession,
  setCurrentSession: (s) => { currentSession = s; },
  getCurrentCompanionKey: () => currentCompanionKey,
  setCurrentCompanionKey: (k) => { currentCompanionKey = k; },
  getCurrentGeneration: () => currentGeneration,
  setCurrentGeneration: (g) => { currentGeneration = g; },
  getCurrentWebviewId: () => currentWebviewId,
  setCurrentWebviewId: (w) => { currentWebviewId = w; },
  clearExpandedCallIds: () => expandedCallIds.clear(),
  resetCurrentAsk: () => { currentAsk = null; currentAskCallId = null; },
  drawRows: draw,
  drawAsk,
  drawRefs,
  drawState,
  drawInfo: (m) => { info = m; drawInfo(); },
  drawEmptyNote,
  setNoteText: (t) => { noteEl.textContent = t; },
  getNoteText: () => noteEl.textContent,
  scrollContainer: scrollEl,
});
window.addEventListener('message', (e) => {
  dispatchHostMessage(e.data, receiveHandlers);
});
actions.ready();
</script></body></html>`;
}
