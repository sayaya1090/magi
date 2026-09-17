import * as MarkdownIt from 'markdown-it';
import type Token from 'markdown-it/lib/token.mjs';

export interface ClassifiedDiffLine {
  text: string;
  cls: string;
}

export function classifyDiffLines(lines: string[]): ClassifiedDiffLine[] {
  const result: ClassifiedDiffLine[] = [];
  let inHunk = false;
  let oldRemaining = 0;
  let newRemaining = 0;

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i];
    let cls = 'diff-plain';
    const hunkMatch = /^@@\s+-(\d+)(?:,(\d+))?\s+\+(\d+)(?:,(\d+))?\s+@@(?:$|\s)/.exec(rawLine);
    const isGitHeader = rawLine.startsWith('diff --git ') || rawLine.startsWith('Index: ');

    if (isGitHeader) {
      inHunk = false;
      oldRemaining = 0;
      newRemaining = 0;
      cls = 'diff-file-header';
    } else if (hunkMatch) {
      inHunk = true;
      oldRemaining = hunkMatch[2] !== undefined ? parseInt(hunkMatch[2], 10) : 1;
      newRemaining = hunkMatch[4] !== undefined ? parseInt(hunkMatch[4], 10) : 1;
      cls = 'diff-hunk-header';
      if (oldRemaining === 0 && newRemaining === 0) {
        inHunk = false;
      }
    } else if (inHunk) {
      if (rawLine.startsWith('+')) {
        cls = 'diff-added';
        if (newRemaining > 0) newRemaining--;
      } else if (rawLine.startsWith('-')) {
        cls = 'diff-deleted';
        if (oldRemaining > 0) oldRemaining--;
      } else if (rawLine.startsWith(' ') || rawLine === '') {
        cls = 'diff-context';
        if (oldRemaining > 0) oldRemaining--;
        if (newRemaining > 0) newRemaining--;
      } else if (rawLine.startsWith('\\')) {
        cls = 'diff-context';
      } else {
        cls = 'diff-plain';
      }
      if (oldRemaining <= 0 && newRemaining <= 0) {
        inHunk = false;
      }
    } else {
      const isFileMeta = (
        rawLine.startsWith('--- ') ||
        rawLine.startsWith('+++ ') ||
        rawLine.startsWith('index ') ||
        rawLine.startsWith('new file mode ') ||
        rawLine.startsWith('deleted file mode ') ||
        rawLine.startsWith('similarity index ') ||
        rawLine.startsWith('rename from ') ||
        rawLine.startsWith('rename to ') ||
        rawLine.startsWith('old mode ') ||
        rawLine.startsWith('new mode ') ||
        rawLine.startsWith('Binary files ')
      );
      if (isFileMeta) {
        cls = 'diff-file-header';
      } else if (rawLine.startsWith('\\')) {
        cls = 'diff-context';
      } else {
        cls = 'diff-plain';
      }
    }

    result.push({ text: rawLine, cls });
  }

  return result;
}

/**
 * Checks whether a URL is safe to open as an external link or anchor.
 * Allows: http, https, mailto, relative paths (./, ../, / but not //), anchor (#).
 * Strictly rejects: command:, javascript:, data:, vbscript:, and URLs containing control characters.
 */
export function isSafeUrl(rawUrl: string): boolean {
  if (!rawUrl) return false;
  // Strip whitespace and ASCII control characters (0x00-0x1F, 0x7F)
  const cleaned = rawUrl.replace(/[\u0000-\u001F\u007F\s]+/g, '');
  if (!cleaned) return false;

  // Check dangerous schemes
  const lower = cleaned.toLowerCase();
  if (
    lower.startsWith('javascript:') ||
    lower.startsWith('command:') ||
    lower.startsWith('data:') ||
    lower.startsWith('vbscript:')
  ) {
    return false;
  }

  // Anchor
  if (cleaned.startsWith('#')) return true;

  // Relative paths
  if (cleaned.startsWith('./') || cleaned.startsWith('../')) return true;
  if (cleaned.startsWith('/') && !cleaned.startsWith('//')) return true;

  // Check absolute URLs (http, https, mailto)
  return /^(https?|mailto):/i.test(cleaned);
}

// Instantiate single reusable markdown-it parser (§5.8.5)
const MarkdownItConstructor = (MarkdownIt as any).default || MarkdownIt;
const parser = new MarkdownItConstructor({
  html: false,
  breaks: true,
  linkify: false,
  typographer: false,
});
parser.validateLink = isSafeUrl;

function isFenceClosed(token: Token, src: string): boolean {
  if (!token.map) return false;
  const [, end] = token.map;
  const lines = src.split(/\r?\n/);
  const lastLine = lines[end - 1] ?? '';
  const marker = token.markup?.[0] || '`';
  const fenceLen = token.markup?.length || 3;
  const regex = new RegExp('^\\s*\\' + marker + '{' + fenceLen + ',}\\s*$');
  return regex.test(lastLine);
}

/**
 * Renders Markdown into a DOM container using purely safe DOM methods
 * (createElement, createTextNode, appendChild). Never touches innerHTML.
 */
export function renderMarkdown(
  container: HTMLElement,
  markdown: string,
  options?: { document?: Document }
): void {
  const doc = options?.document || container.ownerDocument || (typeof document !== 'undefined' ? document : null);
  if (!doc) return;

  container.textContent = '';
  if (!markdown) return;

  const tokens = parser.parse(markdown, {});
  const stack: HTMLElement[] = [container];

  function curParent(): HTMLElement {
    return stack[stack.length - 1];
  }

  function renderInline(inlineTokens: Token[], parent: HTMLElement): void {
    const inlineStack: HTMLElement[] = [parent];
    function curInline(): HTMLElement {
      return inlineStack[inlineStack.length - 1];
    }

    for (const t of inlineTokens) {
      if (t.type === 'text') {
        curInline().appendChild(doc.createTextNode(t.content));
      } else if (t.type === 'code_inline') {
        const code = doc.createElement('code');
        code.textContent = t.content;
        curInline().appendChild(code);
      } else if (t.type === 'strong_open') {
        const strong = doc.createElement('strong');
        curInline().appendChild(strong);
        inlineStack.push(strong);
      } else if (t.type === 'strong_close') {
        if (inlineStack.length > 1) inlineStack.pop();
      } else if (t.type === 'em_open') {
        const em = doc.createElement('em');
        curInline().appendChild(em);
        inlineStack.push(em);
      } else if (t.type === 'em_close') {
        if (inlineStack.length > 1) inlineStack.pop();
      } else if (t.type === 's_open') {
        const del = doc.createElement('del');
        curInline().appendChild(del);
        inlineStack.push(del);
      } else if (t.type === 's_close') {
        if (inlineStack.length > 1) inlineStack.pop();
      } else if (t.type === 'link_open') {
        const href = t.attrGet('href') || '';
        if (isSafeUrl(href)) {
          const a = doc.createElement('a');
          a.setAttribute('href', href);
          if ('href' in a) (a as any).href = href;
          a.setAttribute('target', '_blank');
          if ('target' in a) (a as any).target = '_blank';
          a.setAttribute('rel', 'noreferrer noopener');
          if ('rel' in a) (a as any).rel = 'noreferrer noopener';
          curInline().appendChild(a);
          inlineStack.push(a);
        } else {
          // Reject actionable link, preserve as inert presentation
          const span = doc.createElement('span');
          curInline().appendChild(span);
          inlineStack.push(span);
        }
      } else if (t.type === 'link_close') {
        if (inlineStack.length > 1) inlineStack.pop();
      } else if (t.type === 'image') {
        // Image syntax: text representation without making external network requests (§5.8.5)
        const alt = t.content || '';
        curInline().appendChild(doc.createTextNode(alt ? `[이미지: ${alt}]` : '[이미지]'));
      } else if (t.type === 'softbreak' || t.type === 'hardbreak') {
        curInline().appendChild(doc.createElement('br'));
      } else {
        // Safe fallback for unknown inline tokens: preserve content without dropping
        if (t.content) {
          curInline().appendChild(doc.createTextNode(t.content));
        }
        if (t.children && t.children.length > 0) {
          renderInline(t.children, curInline());
        }
      }
    }
  }

  for (let i = 0; i < tokens.length; i++) {
    const t = tokens[i];

    if (t.type === 'inline') {
      renderInline(t.children || [], curParent());
    } else if (t.nesting === 1) {
      if (t.hidden) {
        continue;
      }
      const tag = t.tag || 'div';
      const el = doc.createElement(tag);
      if (t.attrs) {
        for (const [k, v] of t.attrs) {
          el.setAttribute(k, String(v));
          if (k === 'start' && tag === 'ol' && 'start' in el) {
            (el as any).start = parseInt(v, 10);
          }
        }
      }
      curParent().appendChild(el);
      stack.push(el);
    } else if (t.nesting === -1) {
      if (t.hidden) {
        continue;
      }
      if (stack.length > 1) {
        stack.pop();
      }
    } else if (t.type === 'fence' || t.type === 'code_block') {
      const pre = doc.createElement('pre');
      const lang = (t.info || '').trim().split(/\s+/)[0];
      if (lang) {
        pre.dataset.lang = lang;
      }
      const code = doc.createElement('code');

      let text = t.content;
      if (t.type === 'code_block') {
        if (text.endsWith('\n')) {
          text = text.slice(0, -1);
        }
      } else {
        const closed = isFenceClosed(t, markdown);
        if (closed && text.endsWith('\n')) {
          text = text.slice(0, -1);
        }
      }

      const isDiff = lang === 'diff' || lang === 'patch';
      if (isDiff) {
        const classified = classifyDiffLines(text.split('\n'));
        for (let j = 0; j < classified.length; j++) {
          const item = classified[j];
          const span = doc.createElement('span');
          span.className = 'diff-line ' + item.cls;
          span.textContent = item.text + (j < classified.length - 1 ? '\n' : '');
          code.appendChild(span);
        }
      } else {
        code.textContent = text;
      }

      pre.appendChild(code);
      curParent().appendChild(pre);
    } else if (t.type === 'hr') {
      curParent().appendChild(doc.createElement('hr'));
    } else {
      // Safe fallback for unknown block tokens
      if (t.content) {
        curParent().appendChild(doc.createTextNode(t.content));
      }
    }
  }
}
