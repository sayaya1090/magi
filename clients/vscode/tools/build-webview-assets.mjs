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
const recoverySrc = path.join(root, 'out', 'core', 'recovery_state.js');
let recoveryCompiled = '';
if (fs.existsSync(recoverySrc)) {
  recoveryCompiled = fs.readFileSync(recoverySrc, 'utf8');
}
const wrapped = `// Auto-generated from out/core/answer_state.js for webview. Do not edit directly.
var createAnswerState;
var createRecoveryState;
(function () {
  var exports = {};
  var recoveryExports = {};
  (function (exports) {
    ${recoveryCompiled}
  })(recoveryExports);
  createRecoveryState = recoveryExports.createRecoveryState;

  function require(id) {
    if (id === './recovery_state' || id === '../core/recovery_state') {
      return recoveryExports;
    }
    throw new Error('Cannot require ' + id);
  }

${compiled}
  createAnswerState = exports.createAnswerState;
  if (typeof window !== 'undefined') {
    window.createAnswerState = createAnswerState;
    window.createRecoveryState = createRecoveryState;
  }
})();
`;

fs.writeFileSync(dst, wrapped, 'utf8');

// Bundle chat_adapter.js
const adapterSrc = path.join(root, 'out', 'web', 'chat_adapter.js');
const adapterDst = path.join(outDir, 'chat_adapter.bundle.js');
if (!fs.existsSync(adapterSrc)) {
  console.error(`out/web/chat_adapter.js not found at ${adapterSrc}. Run 'tsc -p .' first.`);
  process.exit(1);
}
const adapterCompiled = fs.readFileSync(adapterSrc, 'utf8');
  const adapterWrapped = `// Auto-generated from out/web/chat_adapter.js for webview. Do not edit directly.
var createWebviewActionAdapter;
var createWebviewInputAdapter;
var createSuggestController;
var createWebviewReceiveHandlers;
var parseHostToWebviewMessage;
var dispatchHostMessage;
var classifyDiffLines;
var renderMarkdown;
var createWebviewRecoveryController;
(function () {
  var exports = typeof module !== 'undefined' && module.exports ? module.exports : {};
${adapterCompiled}
  createWebviewActionAdapter = exports.createWebviewActionAdapter;
  createWebviewInputAdapter = exports.createWebviewInputAdapter;
  createSuggestController = exports.createSuggestController;
  createWebviewReceiveHandlers = exports.createWebviewReceiveHandlers;
  parseHostToWebviewMessage = exports.parseHostToWebviewMessage;
  dispatchHostMessage = exports.dispatchHostMessage;
  classifyDiffLines = exports.classifyDiffLines;
  renderMarkdown = exports.renderMarkdown;
  createWebviewRecoveryController = exports.createWebviewRecoveryController;
  if (typeof window !== 'undefined') {
    window.createWebviewActionAdapter = createWebviewActionAdapter;
    window.createWebviewInputAdapter = createWebviewInputAdapter;
    window.createSuggestController = createSuggestController;
    window.createWebviewReceiveHandlers = createWebviewReceiveHandlers;
    window.parseHostToWebviewMessage = parseHostToWebviewMessage;
    window.dispatchHostMessage = dispatchHostMessage;
    window.classifyDiffLines = classifyDiffLines;
    window.renderMarkdown = renderMarkdown;
    window.createWebviewRecoveryController = createWebviewRecoveryController;
  }
  if (typeof module !== 'undefined' && module.exports) {
    module.exports.createWebviewActionAdapter = createWebviewActionAdapter;
    module.exports.createWebviewInputAdapter = createWebviewInputAdapter;
    module.exports.createSuggestController = createSuggestController;
    module.exports.createWebviewReceiveHandlers = createWebviewReceiveHandlers;
    module.exports.parseHostToWebviewMessage = parseHostToWebviewMessage;
    module.exports.dispatchHostMessage = dispatchHostMessage;
    module.exports.classifyDiffLines = classifyDiffLines;
    module.exports.renderMarkdown = renderMarkdown;
    module.exports.createWebviewRecoveryController = createWebviewRecoveryController;
  }
})();
`;
  fs.writeFileSync(adapterDst, adapterWrapped, 'utf8');

