import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, '..');
const src = path.join(root, 'out', 'core', 'answer_state.js');
const outDir = path.join(root, 'out', 'web');
const dst = path.join(outDir, 'answer_state.js');

if (!fs.existsSync(src)) {
  console.error('out/core/answer_state.js not found. Run tsc -p . first.');
  process.exit(1);
}

fs.mkdirSync(outDir, { recursive: true });
const compiled = fs.readFileSync(src, 'utf8');
const wrapped = `// Auto-generated from out/core/answer_state.js for webview. Do not edit directly.
var createAnswerState;
(function () {
  var exports = {};
${compiled}
  createAnswerState = exports.createAnswerState;
  if (typeof window !== 'undefined') {
    window.createAnswerState = createAnswerState;
  }
})();
`;

fs.writeFileSync(dst, wrapped, 'utf8');

// Bundle chat_adapter.js
const adapterSrc = path.join(root, 'out', 'core', 'chat_adapter.js');
const adapterDst = path.join(outDir, 'chat_adapter.js');
if (fs.existsSync(adapterSrc)) {
  const adapterCompiled = fs.readFileSync(adapterSrc, 'utf8');
  const adapterWrapped = `// Auto-generated from out/core/chat_adapter.js for webview. Do not edit directly.
var createWebviewActionAdapter;
var createWebviewInputAdapter;
var createWebviewReceiveHandlers;
var parseHostToWebviewMessage;
var dispatchHostMessage;
(function () {
  var exports = typeof module !== 'undefined' && module.exports ? module.exports : {};
${adapterCompiled}
  createWebviewActionAdapter = exports.createWebviewActionAdapter;
  createWebviewInputAdapter = exports.createWebviewInputAdapter;
  createWebviewReceiveHandlers = exports.createWebviewReceiveHandlers;
  parseHostToWebviewMessage = exports.parseHostToWebviewMessage;
  dispatchHostMessage = exports.dispatchHostMessage;
  if (typeof window !== 'undefined') {
    window.createWebviewActionAdapter = createWebviewActionAdapter;
    window.createWebviewInputAdapter = createWebviewInputAdapter;
    window.createWebviewReceiveHandlers = createWebviewReceiveHandlers;
    window.parseHostToWebviewMessage = parseHostToWebviewMessage;
    window.dispatchHostMessage = dispatchHostMessage;
  }
  if (typeof module !== 'undefined' && module.exports) {
    module.exports.createWebviewActionAdapter = createWebviewActionAdapter;
    module.exports.createWebviewInputAdapter = createWebviewInputAdapter;
    module.exports.createWebviewReceiveHandlers = createWebviewReceiveHandlers;
    module.exports.parseHostToWebviewMessage = parseHostToWebviewMessage;
    module.exports.dispatchHostMessage = dispatchHostMessage;
  }
})();
`;
  fs.writeFileSync(adapterDst, adapterWrapped, 'utf8');
}

