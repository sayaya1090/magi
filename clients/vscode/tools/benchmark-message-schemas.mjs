import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

// Resolve paths based on import.meta.url (cwd-independent)
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const vscodeRoot = path.resolve(__dirname, '..');
const packageJsonPath = path.join(vscodeRoot, 'package.json');
const req = createRequire(packageJsonPath);

const esbuild = req('esbuild');
const v = req('valibot');

function getPackageVersion(packageName) {
  try {
    const entry = req.resolve(packageName);
    let cur = path.dirname(entry);
    while (cur && cur !== path.dirname(cur)) {
      const candidate = path.join(cur, 'package.json');
      if (fs.existsSync(candidate)) {
        const pkg = JSON.parse(fs.readFileSync(candidate, 'utf8'));
        if (pkg.name === packageName) return pkg.version;
      }
      cur = path.dirname(cur);
    }
  } catch {
    // not found
  }
  return null;
}

const valibotVersion = getPackageVersion('valibot') || 'unknown';
const zodVersion = getPackageVersion('zod');

let z = null;
if (zodVersion) {
  try {
    z = req('zod').z;
  } catch {
    z = null;
  }
}

// ── 1. Reusing compiled parsers ──
// Ensure out/ exists (user must run build prior to benchmark)
const legacyParserPath = path.join(vscodeRoot, 'out', 'test', 'support', 'legacy_protocol_parser.js');
const productParserPath = path.join(vscodeRoot, 'out', 'core', 'webview_protocol.js');

if (!fs.existsSync(legacyParserPath) || !fs.existsSync(productParserPath)) {
  console.error('Error: compiled parsers not found. Please run "npm run build --prefix clients/vscode" first.');
  process.exit(1);
}

const { legacyParseWebviewToHostMessage } = req(legacyParserPath);
const { parseWebviewToHostMessage } = req(productParserPath);

console.log('================================================================');
console.log(' Message Schema Benchmark & Parity Verification');
console.log('================================================================');
console.log(` Valibot (installed) : ${valibotVersion}`);
if (zodVersion) {
  console.log(` Zod (installed)     : ${zodVersion}`);
} else {
  console.log(` Zod (installed)     : not installed (skipped in this run)`);
}
console.log(` Target scope        : representative subset (say, reply, open)`);
console.log(` Legacy baseline     : commit 98a733aa (src/test/support/legacy_protocol_parser.ts)`);
console.log(` Product parser      : compiled out/core/webview_protocol.js`);
console.log('================================================================\n');

// ── 2. Test Cases for Representative Messages ──

const testCases = {
  say: [
    { name: 'valid minimal', raw: { kind: 'say', text: 'hello' } },
    { name: 'valid with creationTaskId', raw: { kind: 'say', text: 'hello', creationTaskId: 'task-1' } },
    { name: 'valid empty text', raw: { kind: 'say', text: '' } },
    { name: 'valid preserves text whitespace', raw: { kind: 'say', text: '  padded  ' } },
    { name: 'valid with extra unknown fields', raw: { kind: 'say', text: 'hi', extra: 123, foo: 'bar' } },
    { name: 'invalid creationTaskId empty', raw: { kind: 'say', text: 'hi', creationTaskId: '' } },
    { name: 'invalid creationTaskId whitespace only', raw: { kind: 'say', text: 'hi', creationTaskId: '   ' } },
    { name: 'invalid creationTaskId non-string', raw: { kind: 'say', text: 'hi', creationTaskId: 123 } },
    { name: 'invalid missing text', raw: { kind: 'say' } },
    { name: 'invalid non-string text', raw: { kind: 'say', text: null } },
  ],
  reply: [
    {
      name: 'valid full',
      raw: {
        kind: 'reply',
        callId: 'call-1',
        text: 'answer text',
        attemptId: 1,
        companionKey: 'ckey',
        session: 'sess-1',
        generation: 0,
        webviewId: 'wvid',
      },
    },
    {
      name: 'valid empty text',
      raw: {
        kind: 'reply',
        callId: 'call-1',
        text: '',
        attemptId: 2,
        companionKey: 'ckey',
        session: 'sess-1',
        generation: 3,
        webviewId: 'wvid',
      },
    },
    {
      name: 'valid preserves verbatim identifiers with spaces',
      raw: {
        kind: 'reply',
        callId: '  call 1  ',
        text: 'hello',
        attemptId: 1,
        companionKey: '  key  ',
        session: '  sess  ',
        generation: 0,
        webviewId: '  wv  ',
      },
    },
    {
      name: 'invalid whitespace only callId',
      raw: {
        kind: 'reply',
        callId: '   ',
        text: 't',
        attemptId: 1,
        companionKey: 'k',
        session: 's',
        generation: 0,
        webviewId: 'w',
      },
    },
    {
      name: 'invalid attemptId 0',
      raw: {
        kind: 'reply',
        callId: 'c',
        text: 't',
        attemptId: 0,
        companionKey: 'k',
        session: 's',
        generation: 0,
        webviewId: 'w',
      },
    },
    {
      name: 'invalid attemptId negative',
      raw: {
        kind: 'reply',
        callId: 'c',
        text: 't',
        attemptId: -1,
        companionKey: 'k',
        session: 's',
        generation: 0,
        webviewId: 'w',
      },
    },
    {
      name: 'invalid attemptId float',
      raw: {
        kind: 'reply',
        callId: 'c',
        text: 't',
        attemptId: 1.5,
        companionKey: 'k',
        session: 's',
        generation: 0,
        webviewId: 'w',
      },
    },
    {
      name: 'invalid generation negative',
      raw: {
        kind: 'reply',
        callId: 'c',
        text: 't',
        attemptId: 1,
        companionKey: 'k',
        session: 's',
        generation: -1,
        webviewId: 'w',
      },
    },
    {
      name: 'invalid missing session',
      raw: {
        kind: 'reply',
        callId: 'c',
        text: 't',
        attemptId: 1,
        companionKey: 'k',
        generation: 0,
        webviewId: 'w',
      },
    },
  ],
  open: [
    { name: 'valid without seq', raw: { kind: 'open', session: 's1', callId: 'c1' } },
    { name: 'valid with seq', raw: { kind: 'open', session: 's1', callId: 'c1', seq: 42 } },
    { name: 'valid preserves padded identifiers', raw: { kind: 'open', session: '  s1  ', callId: '  c1  ' } },
    { name: 'invalid empty callId', raw: { kind: 'open', session: 's1', callId: '' } },
    { name: 'invalid whitespace session', raw: { kind: 'open', session: '   ', callId: 'c1' } },
    { name: 'valid non-numeric seq string falls back to undefined', raw: { kind: 'open', session: 's1', callId: 'c1', seq: '12' } },
    { name: 'valid non-numeric seq null falls back to undefined', raw: { kind: 'open', session: 's1', callId: 'c1', seq: null } },
    { name: 'valid non-numeric seq object falls back to undefined', raw: { kind: 'open', session: 's1', callId: 'c1', seq: {} } },
    { name: 'valid numeric NaN preserved as number', raw: { kind: 'open', session: 's1', callId: 'c1', seq: NaN }, isNaN: true },
    { name: 'valid numeric Infinity preserved', raw: { kind: 'open', session: 's1', callId: 'c1', seq: Infinity } },
  ],
};

// ── 3. Run Parity Verification ──

console.log('=== 1. PARITY VERIFICATION (Product vs Legacy Baseline) ===\n');

let valibotMismatches = 0;

for (const [kind, cases] of Object.entries(testCases)) {
  console.log(`Checking [${kind}] (${cases.length} cases)...`);
  for (const tc of cases) {
    const legacyRes = legacyParseWebviewToHostMessage(tc.raw);
    const productRes = parseWebviewToHostMessage(tc.raw);

    let match = false;
    if (tc.isNaN) {
      match = productRes !== undefined &&
        legacyRes !== undefined &&
        productRes.kind === 'open' &&
        legacyRes.kind === 'open' &&
        typeof productRes.seq === 'number' &&
        Number.isNaN(productRes.seq) &&
        typeof legacyRes.seq === 'number' &&
        Number.isNaN(legacyRes.seq);
    } else {
      try {
        assert.deepStrictEqual(productRes, legacyRes);
        match = true;
      } catch {
        match = false;
      }
    }

    if (!match) {
      console.log(`  [Valibot MISMATCH] ${tc.name}`);
      console.log(`    Expected:`, legacyRes);
      console.log(`    Actual:  `, productRes);
      valibotMismatches++;
    }
  }
}

console.log(`\nParity result: ${valibotMismatches} mismatches found against legacy parser.\n`);
if (valibotMismatches > 0) {
  console.error(`Benchmark parity check failed: ${valibotMismatches} mismatches detected.`);
  process.exit(1);
}

// ── 4. Benchmark Parsing Performance (Accurate message counting) ──

console.log('=== 2. PARSING PERFORMANCE ===');
console.log('  Scope                : full product parser (WebviewToHost) vs Zod representative schemas');
const ITER = 10000;
const PARSES_PER_ITER = 2; // sampleSay + sampleReply
const TOTAL_PARSES = ITER * PARSES_PER_ITER;

const sampleSay = { kind: 'say', text: 'Hello, world!', creationTaskId: 'task-123' };
const sampleReply = {
  kind: 'reply',
  callId: 'call-abc',
  text: 'some answer',
  attemptId: 4,
  companionKey: 'ck-1',
  session: 'sess-xyz',
  generation: 2,
  webviewId: 'wv-001',
};

// Hand-written legacy parser
let t0 = performance.now();
for (let i = 0; i < ITER; i++) {
  legacyParseWebviewToHostMessage(sampleSay);
  legacyParseWebviewToHostMessage(sampleReply);
}
const legacyTime = performance.now() - t0;

// Compiled product parser (Valibot)
t0 = performance.now();
for (let i = 0; i < ITER; i++) {
  parseWebviewToHostMessage(sampleSay);
  parseWebviewToHostMessage(sampleReply);
}
const productTime = performance.now() - t0;

console.log(`  Iterations           : ${ITER.toLocaleString()} (${TOTAL_PARSES.toLocaleString()} total message parses)`);
console.log(`  Legacy parser (hand) : ${legacyTime.toFixed(2)} ms (avg ${(legacyTime / TOTAL_PARSES * 1000).toFixed(2)} µs/msg)`);
console.log(`  Product parser (v${valibotVersion}): ${productTime.toFixed(2)} ms (avg ${(productTime / TOTAL_PARSES * 1000).toFixed(2)} µs/msg, ${(productTime / legacyTime).toFixed(1)}x baseline)`);

if (z) {
  const NonEmptyStringZ = z.string().refine((s) => s.trim().length > 0);
  const PositiveIntZ = z.number().int().min(1);
  const NonNegIntZ = z.number().int().min(0);
  const SaySchemaZ = z.object({ kind: z.literal('say'), text: z.string(), creationTaskId: NonEmptyStringZ.optional() });
  const ReplySchemaZ = z.object({
    kind: z.literal('reply'),
    callId: NonEmptyStringZ,
    text: z.string(),
    attemptId: PositiveIntZ,
    companionKey: NonEmptyStringZ,
    session: NonEmptyStringZ,
    generation: NonNegIntZ,
    webviewId: NonEmptyStringZ,
  });

  t0 = performance.now();
  for (let i = 0; i < ITER; i++) {
    SaySchemaZ.safeParse(sampleSay);
    ReplySchemaZ.safeParse(sampleReply);
  }
  const zodTime = performance.now() - t0;
  console.log(`  Zod (v${zodVersion})        : ${zodTime.toFixed(2)} ms (avg ${(zodTime / TOTAL_PARSES * 1000).toFixed(2)} µs/msg, ${(zodTime / legacyTime).toFixed(1)}x baseline)`);
} else {
  console.log(`  Zod                  : not installed (skipped in this run)`);
}

// ── 5. Bundle Size Measurement in Unique Temporary Directory ──

console.log('\n=== 3. BUNDLE SIZE MEASUREMENT (esbuild CJS tree-shaking) ===');

const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'magi_bench_'));

try {
  const valibotEntry = `
import * as v from 'valibot';
const NonEmptyStringV = v.pipe(v.string(), v.check((s) => s.trim().length > 0));
const PositiveIntV = v.pipe(v.number(), v.integer(), v.minValue(1));
const NonNegIntV = v.pipe(v.number(), v.integer(), v.minValue(0));
const SaySchema = v.object({ kind: v.literal('say'), text: v.string(), creationTaskId: v.optional(NonEmptyStringV) });
const ReplySchema = v.object({
  kind: v.literal('reply'),
  callId: NonEmptyStringV,
  text: v.string(),
  attemptId: PositiveIntV,
  companionKey: NonEmptyStringV,
  session: NonEmptyStringV,
  generation: NonNegIntV,
  webviewId: NonEmptyStringV,
});
const OpenSchema = v.object({
  kind: v.literal('open'),
  session: NonEmptyStringV,
  callId: NonEmptyStringV,
  seq: v.optional(v.pipe(v.unknown(), v.transform((val) => typeof val === 'number' ? val : undefined))),
});
const WebviewToHostSchema = v.union([SaySchema, ReplySchema, OpenSchema]);
export function parse(raw) {
  const res = v.safeParse(WebviewToHostSchema, raw);
  return res.success ? res.output : undefined;
}
`;

  const vFile = path.join(tmpDir, 'valibot_entry.js');
  fs.writeFileSync(vFile, valibotEntry);

  const extraNodePaths = (process.env.NODE_PATH || '').split(path.delimiter).filter(Boolean);
  const nodePaths = [path.join(vscodeRoot, 'node_modules'), ...extraNodePaths];

  function measureBundle(entryPath, minify = false) {
    const res = esbuild.buildSync({
      entryPoints: [entryPath],
      bundle: true,
      format: 'cjs',
      minify,
      write: false,
      nodePaths,
    });
    return res.outputFiles[0].contents.length;
  }

  const vRawSize = measureBundle(vFile, false);
  const vMinSize = measureBundle(vFile, true);

  console.log(`  Valibot (v${valibotVersion}):`);
  console.log(`    Unminified bundle : ${vRawSize.toLocaleString()} bytes (${(vRawSize / 1024).toFixed(2)} KB)`);
  console.log(`    Minified bundle   : ${vMinSize.toLocaleString()} bytes (${(vMinSize / 1024).toFixed(2)} KB)`);

  if (zodVersion) {
    const zodEntry = `
import { z } from 'zod';
const NonEmptyStringZ = z.string().refine((s) => s.trim().length > 0);
const PositiveIntZ = z.number().int().min(1);
const NonNegIntZ = z.number().int().min(0);
const SaySchema = z.object({ kind: z.literal('say'), text: z.string(), creationTaskId: NonEmptyStringZ.optional() });
const ReplySchema = z.object({
  kind: z.literal('reply'),
  callId: NonEmptyStringZ,
  text: z.string(),
  attemptId: PositiveIntZ,
  companionKey: NonEmptyStringZ,
  session: NonEmptyStringZ,
  generation: NonNegIntZ,
  webviewId: NonEmptyStringZ,
});
const OpenSchema = z.object({ kind: z.literal('open'), session: NonEmptyStringZ, callId: NonEmptyStringZ, seq: z.any().optional() });
const WebviewToHostSchema = z.discriminatedUnion('kind', [SaySchema, ReplySchema, OpenSchema]);
export function parse(raw) {
  const res = WebviewToHostSchema.safeParse(raw);
  return res.success ? res.data : undefined;
}
`;
    const zFile = path.join(tmpDir, 'zod_entry.js');
    fs.writeFileSync(zFile, zodEntry);
    const zRawSize = measureBundle(zFile, false);
    const zMinSize = measureBundle(zFile, true);
    console.log(`  Zod (v${zodVersion}):`);
    console.log(`    Unminified bundle : ${zRawSize.toLocaleString()} bytes (${(zRawSize / 1024).toFixed(2)} KB)`);
    console.log(`    Minified bundle   : ${zMinSize.toLocaleString()} bytes (${(zMinSize / 1024).toFixed(2)} KB)`);
    console.log(`  Difference: Zod is +${((zMinSize - vMinSize) / 1024).toFixed(2)} KB larger (${(zMinSize / vMinSize).toFixed(1)}x)`);
  } else {
    console.log(`  Zod                  : skipped (not installed in this workspace; reference measurement: minified ~443 KB)`);
  }
} finally {
  fs.rmSync(tmpDir, { recursive: true, force: true });
}

console.log('\n================================================================');
console.log(' Benchmark Completed Successfully');
console.log('================================================================\n');
