import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import esbuild from 'esbuild';

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
const domInteractionSrc = path.join(root, 'src', 'web', 'dom_interaction.ts');
const recoveryViewSrc = path.join(root, 'src', 'web', 'recovery_view.ts');
const recoveryControllerSrc = path.join(root, 'src', 'web', 'recovery_controller.ts');
const adapterSrc = path.join(root, 'src', 'web', 'chat_adapter.ts');
const markdownRenderSrc = path.join(root, 'src', 'web', 'markdown_render.ts');
const protocolSrc = path.join(root, 'src', 'core', 'webview_protocol.ts');
/* 웹뷰 번들이 **부르는** 코어 판정. 빈 전사 안내를 무엇으로 할지는 여기서 정해지고
   (emptyTranscriptNote), 화면은 그리기만 한다 — 그래서 번들의 진짜 입력이다. 타입만 쓰던 동안은
   esbuild 가 지워서 없어도 됐지만, 이제는 없으면 번들이 안 선다. */
const activitySrc = path.join(root, 'src', 'core', 'activity.ts');

const outDir = path.join(root, 'out', 'web');
const coreDir = path.join(root, 'out', 'core');
const answerStateDst = path.join(outDir, 'answer_state.js');
const adapterDst = path.join(outDir, 'chat_adapter.bundle.js');
const markdownRenderDst = path.join(outDir, 'markdown_render.js');
const protocolDst = path.join(coreDir, 'webview_protocol.js');

// 1. Single list of required inputs (§4.7, §5.7, §5.8)
const requiredInputs = [
  { id: 'answer_state', name: 'out/core/answer_state.js', path: answerStateSrc },
  { id: 'recovery_state', name: 'out/core/recovery_state.js', path: recoveryStateSrc },
  { id: 'dom_interaction', name: 'src/web/dom_interaction.ts', path: domInteractionSrc },
  { id: 'recovery_view', name: 'src/web/recovery_view.ts', path: recoveryViewSrc },
  { id: 'recovery_controller', name: 'src/web/recovery_controller.ts', path: recoveryControllerSrc },
  { id: 'markdown_render', name: 'src/web/markdown_render.ts', path: markdownRenderSrc },
  { id: 'chat_adapter', name: 'src/web/chat_adapter.ts', path: adapterSrc },
  { id: 'webview_protocol', name: 'src/core/webview_protocol.ts', path: protocolSrc },
  { id: 'activity', name: 'src/core/activity.ts', path: activitySrc },
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

// 5. Construct chat_adapter.bundle.js bundle using esbuild (§5.7)
let bundledCode = '';
try {
  const esbuildRes = esbuild.buildSync({
    entryPoints: [adapterSrc],
    bundle: true,
    format: 'iife',
    globalName: 'MagiAdapter',
    write: false,
    nodePaths: [
      path.join(root, 'node_modules'),
      path.join(__dirname, '..', 'node_modules'),
    ],
    metafile: true,
    logLevel: 'silent',
  });
  bundledCode = esbuildRes.outputFiles[0].text;
} catch (err) {
  console.error(`esbuild failed to bundle ${adapterSrc}: ${err.message}`);
  process.exit(1);
}

const adapterWrapped = `// Auto-generated from src/web/chat_adapter.ts and dependencies for webview. Do not edit directly.
var createWebviewActionAdapter;
var createWebviewInputAdapter;
var createSuggestController;
var createWebviewReceiveHandlers;
var parseHostToWebviewMessage;
var dispatchHostMessage;
var classifyDiffLines;
var renderMarkdown;
var formatChoiceOptions;
var updateInFlightUI;
var createWebviewRecoveryController;
var createRecoveryView;
var captureSelection;
var restoreSelection;
var restoreFocus;
var moveDomChild;

(function () {
${bundledCode}

  var exp = typeof MagiAdapter !== 'undefined' ? MagiAdapter : {};
  createWebviewActionAdapter = exp.createWebviewActionAdapter;
  createWebviewInputAdapter = exp.createWebviewInputAdapter;
  createSuggestController = exp.createSuggestController;
  createWebviewReceiveHandlers = exp.createWebviewReceiveHandlers;
  parseHostToWebviewMessage = exp.parseHostToWebviewMessage;
  dispatchHostMessage = exp.dispatchHostMessage;
  classifyDiffLines = exp.classifyDiffLines;
  renderMarkdown = exp.renderMarkdown;
  formatChoiceOptions = exp.formatChoiceOptions;
  updateInFlightUI = exp.updateInFlightUI;
  createWebviewRecoveryController = exp.createWebviewRecoveryController;
  createRecoveryView = exp.createRecoveryView;
  captureSelection = exp.captureSelection;
  restoreSelection = exp.restoreSelection;
  restoreFocus = exp.restoreFocus;
  moveDomChild = exp.moveDomChild;

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
    window.updateInFlightUI = updateInFlightUI;
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
    module.exports.updateInFlightUI = updateInFlightUI;
    module.exports.createWebviewRecoveryController = createWebviewRecoveryController;
    module.exports.createRecoveryView = createRecoveryView;
    module.exports.captureSelection = captureSelection;
    module.exports.restoreSelection = restoreSelection;
    module.exports.restoreFocus = restoreFocus;
    module.exports.moveDomChild = moveDomChild;
  }
})();
`;


// 6. Construct webview_protocol.js bundle for host with inlined Valibot runtime (§5.8.3)
let protocolBundledCode = '';
try {
  const esbuildProtocol = esbuild.buildSync({
    entryPoints: [protocolSrc],
    bundle: true,
    format: 'cjs',
    platform: 'node',
    write: false,
    nodePaths: [
      path.join(root, 'node_modules'),
      path.join(__dirname, '..', 'node_modules'),
    ],
    logLevel: 'silent',
  });
  protocolBundledCode = esbuildProtocol.outputFiles[0].text;
} catch (err) {
  console.error(`esbuild failed to bundle ${protocolSrc}: ${err.message}`);
  process.exit(1);
}

// 6.5. Construct markdown_render.js bundle with inlined markdown-it (§5.8.5)
let markdownRenderBundledCode = '';
try {
  const esbuildMd = esbuild.buildSync({
    entryPoints: [markdownRenderSrc],
    bundle: true,
    format: 'cjs',
    platform: 'node',
    write: false,
    nodePaths: [
      path.join(root, 'node_modules'),
      path.join(__dirname, '..', 'node_modules'),
    ],
    logLevel: 'silent',
  });
  markdownRenderBundledCode = esbuildMd.outputFiles[0].text;
} catch (err) {
  console.error(`esbuild failed to bundle ${markdownRenderSrc}: ${err.message}`);
  process.exit(1);
}

// 7. Write all output files together after all inputs are verified and compiled
fs.mkdirSync(outDir, { recursive: true });
fs.mkdirSync(coreDir, { recursive: true });
fs.writeFileSync(answerStateDst, answerStateWrapped, 'utf8');
fs.writeFileSync(adapterDst, adapterWrapped, 'utf8');
fs.writeFileSync(markdownRenderDst, markdownRenderBundledCode, 'utf8');
fs.writeFileSync(protocolDst, protocolBundledCode, 'utf8');
