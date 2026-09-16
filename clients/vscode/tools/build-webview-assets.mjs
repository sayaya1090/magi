import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

let root = path.join(__dirname, '..');
for (const arg of process.argv.slice(2)) {
  if (arg.startsWith('--root=')) {
    root = path.resolve(arg.slice(7));
  } else if (!arg.startsWith('--')) {
    root = path.resolve(arg);
  }
}
if (process.env.MAGI_BUILD_ROOT) {
  root = path.resolve(process.env.MAGI_BUILD_ROOT);
}

const answerStateSrc = path.join(root, 'out', 'core', 'answer_state.js');
const recoveryStateSrc = path.join(root, 'out', 'core', 'recovery_state.js');
const domInteractionSrc = path.join(root, 'out', 'web', 'dom_interaction.js');
const recoveryViewSrc = path.join(root, 'out', 'web', 'recovery_view.js');
const recoveryControllerSrc = path.join(root, 'out', 'web', 'recovery_controller.js');
const adapterSrc = path.join(root, 'out', 'web', 'chat_adapter.js');

const outDir = path.join(root, 'out', 'web');
const answerStateDst = path.join(outDir, 'answer_state.js');
const adapterDst = path.join(outDir, 'chat_adapter.bundle.js');

// 1. Single list of required inputs (§4.7 P2)
const requiredInputs = [
  { id: 'answer_state', name: 'out/core/answer_state.js', path: answerStateSrc },
  { id: 'recovery_state', name: 'out/core/recovery_state.js', path: recoveryStateSrc },
  { id: 'dom_interaction', name: 'out/web/dom_interaction.js', path: domInteractionSrc },
  { id: 'recovery_view', name: 'out/web/recovery_view.js', path: recoveryViewSrc },
  { id: 'recovery_controller', name: 'out/web/recovery_controller.js', path: recoveryControllerSrc },
  { id: 'chat_adapter', name: 'out/web/chat_adapter.js', path: adapterSrc },
];

// 2. Validate existence of all required inputs BEFORE touching/writing any output file
for (const req of requiredInputs) {
  if (!fs.existsSync(req.path)) {
    console.error(`${req.name} not found at ${req.path}. Run 'tsc -p .' first.`);
    process.exit(1);
  }
}

// 3. Read all inputs into memory
const contents = {};
for (const req of requiredInputs) {
  try {
    contents[req.id] = fs.readFileSync(req.path, 'utf8');
  } catch (err) {
    console.error(`Failed to read ${req.name} at ${req.path}: ${err.message}. Run 'tsc -p .' first.`);
    process.exit(1);
  }
}

// 4. Construct answer_state.js bundle
const answerStateWrapped = `// Auto-generated from out/core/answer_state.js for webview. Do not edit directly.
var createAnswerState;
var createRecoveryState;
(function () {
  var exports = {};
  var recoveryExports = {};
  (function (exports) {
${contents['recovery_state']}
  })(recoveryExports);
  createRecoveryState = recoveryExports.createRecoveryState;

  function require(id) {
    if (id === './recovery_state' || id === '../core/recovery_state') {
      return recoveryExports;
    }
    throw new Error('Cannot require ' + id);
  }

${contents['answer_state']}
  createAnswerState = exports.createAnswerState;
  if (typeof window !== 'undefined') {
    window.createAnswerState = createAnswerState;
    window.createRecoveryState = createRecoveryState;
  }
})();
`;

// 5. Construct chat_adapter.bundle.js bundle
const adapterWrapped = `// Auto-generated from out/web/chat_adapter.js and recovery modules for webview. Do not edit directly.
var createWebviewActionAdapter;
var createWebviewInputAdapter;
var createSuggestController;
var createWebviewReceiveHandlers;
var parseHostToWebviewMessage;
var dispatchHostMessage;
var classifyDiffLines;
var renderMarkdown;
var formatChoiceOptions;
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
${contents['dom_interaction']}
  })(domInteractionMod, domInteractionMod.exports);

  // 2. recovery_view
  var recoveryViewMod = { exports: {} };
  modules['recovery_view'] = recoveryViewMod;
  (function (module, exports) {
${contents['recovery_view']}
  })(recoveryViewMod, recoveryViewMod.exports);

  // 3. recovery_controller
  var recoveryControllerMod = { exports: {} };
  modules['recovery_controller'] = recoveryControllerMod;
  (function (module, exports) {
${contents['recovery_controller']}
  })(recoveryControllerMod, recoveryControllerMod.exports);

  // 4. chat_adapter
  var adapterMod = { exports: typeof module !== 'undefined' && module.exports ? module.exports : {} };
  modules['chat_adapter'] = adapterMod;
  var exports = adapterMod.exports;
  (function (module, exports) {
${contents['chat_adapter']}
  })(adapterMod, adapterMod.exports);

  createWebviewActionAdapter = exports.createWebviewActionAdapter;
  createWebviewInputAdapter = exports.createWebviewInputAdapter;
  createSuggestController = exports.createSuggestController;
  createWebviewReceiveHandlers = exports.createWebviewReceiveHandlers;
  parseHostToWebviewMessage = exports.parseHostToWebviewMessage;
  dispatchHostMessage = exports.dispatchHostMessage;
  classifyDiffLines = exports.classifyDiffLines;
  renderMarkdown = exports.renderMarkdown;
  formatChoiceOptions = exports.formatChoiceOptions;
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
    window.formatChoiceOptions = formatChoiceOptions;
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
    module.exports.formatChoiceOptions = formatChoiceOptions;
    module.exports.createWebviewRecoveryController = createWebviewRecoveryController;
    module.exports.createRecoveryView = createRecoveryView;
    module.exports.captureSelection = captureSelection;
    module.exports.restoreSelection = restoreSelection;
    module.exports.restoreFocus = restoreFocus;
    module.exports.moveDomChild = moveDomChild;
  }
})();
`;

// 6. Write both output files together after all inputs are verified and read
fs.mkdirSync(outDir, { recursive: true });
fs.writeFileSync(answerStateDst, answerStateWrapped, 'utf8');
fs.writeFileSync(adapterDst, adapterWrapped, 'utf8');
