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

// Bundle chat_adapter.js and recovery modules (§4.7)
const domInteractionSrc = path.join(root, 'out', 'web', 'dom_interaction.js');
const recoveryViewSrc = path.join(root, 'out', 'web', 'recovery_view.js');
const recoveryControllerSrc = path.join(root, 'out', 'web', 'recovery_controller.js');
const adapterSrc = path.join(root, 'out', 'web', 'chat_adapter.js');
const adapterDst = path.join(outDir, 'chat_adapter.bundle.js');

const requiredFiles = [
  { name: 'out/web/dom_interaction.js', path: domInteractionSrc },
  { name: 'out/web/recovery_view.js', path: recoveryViewSrc },
  { name: 'out/web/recovery_controller.js', path: recoveryControllerSrc },
  { name: 'out/web/chat_adapter.js', path: adapterSrc },
];

for (const req of requiredFiles) {
  if (!fs.existsSync(req.path)) {
    console.error(`${req.name} not found at ${req.path}. Run 'tsc -p .' first.`);
    process.exit(1);
  }
}

const domInteractionCompiled = fs.readFileSync(domInteractionSrc, 'utf8');
const recoveryViewCompiled = fs.readFileSync(recoveryViewSrc, 'utf8');
const recoveryControllerCompiled = fs.readFileSync(recoveryControllerSrc, 'utf8');
const adapterCompiled = fs.readFileSync(adapterSrc, 'utf8');

const adapterWrapped = `// Auto-generated from out/web/chat_adapter.js and recovery modules for webview. Do not edit directly.
var createWebviewActionAdapter;
var createWebviewInputAdapter;
var createSuggestController;
var createWebviewReceiveHandlers;
var parseHostToWebviewMessage;
var dispatchHostMessage;
var classifyDiffLines;
var renderMarkdown;
var createWebviewRecoveryController;
var createRecoveryView;
var captureSelection;
var restoreSelection;
var restoreFocus;
var moveDomChild;
(function () {
  var modules = {};
  function require(id) {
    var key = id.replace(/^\\.\\//, '').replace(/\\.js$/, '');
    if (modules[key]) {
      return modules[key].exports;
    }
    throw new Error('Cannot require ' + id + ' in webview bundle');
  }

  // 1. dom_interaction
  var domInteractionMod = { exports: {} };
  modules['dom_interaction'] = domInteractionMod;
  (function (module, exports) {
${domInteractionCompiled}
  })(domInteractionMod, domInteractionMod.exports);

  // 2. recovery_view
  var recoveryViewMod = { exports: {} };
  modules['recovery_view'] = recoveryViewMod;
  (function (module, exports) {
${recoveryViewCompiled}
  })(recoveryViewMod, recoveryViewMod.exports);

  // 3. recovery_controller
  var recoveryControllerMod = { exports: {} };
  modules['recovery_controller'] = recoveryControllerMod;
  (function (module, exports) {
${recoveryControllerCompiled}
  })(recoveryControllerMod, recoveryControllerMod.exports);

  // 4. chat_adapter
  var adapterMod = { exports: typeof module !== 'undefined' && module.exports ? module.exports : {} };
  modules['chat_adapter'] = adapterMod;
  var exports = adapterMod.exports;
  (function (module, exports) {
${adapterCompiled}
  })(adapterMod, adapterMod.exports);

  createWebviewActionAdapter = exports.createWebviewActionAdapter;
  createWebviewInputAdapter = exports.createWebviewInputAdapter;
  createSuggestController = exports.createSuggestController;
  createWebviewReceiveHandlers = exports.createWebviewReceiveHandlers;
  parseHostToWebviewMessage = exports.parseHostToWebviewMessage;
  dispatchHostMessage = exports.dispatchHostMessage;
  classifyDiffLines = exports.classifyDiffLines;
  renderMarkdown = exports.renderMarkdown;
  createWebviewRecoveryController = exports.createWebviewRecoveryController;
  createRecoveryView = exports.createRecoveryView;
  captureSelection = exports.captureSelection;
  restoreSelection = exports.restoreSelection;
  restoreFocus = exports.restoreFocus;
  moveDomChild = exports.moveDomChild;

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
    window.createRecoveryView = createRecoveryView;
    window.captureSelection = captureSelection;
    window.restoreSelection = restoreSelection;
    window.restoreFocus = restoreFocus;
    window.moveDomChild = moveDomChild;
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
    module.exports.createRecoveryView = createRecoveryView;
    module.exports.captureSelection = captureSelection;
    module.exports.restoreSelection = restoreSelection;
    module.exports.restoreFocus = restoreFocus;
    module.exports.moveDomChild = moveDomChild;
  }
})();
`;
fs.writeFileSync(adapterDst, adapterWrapped, 'utf8');

