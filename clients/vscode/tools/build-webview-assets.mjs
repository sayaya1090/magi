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
