import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(path.resolve('clients/vscode/package.json'));
const esbuild = require('esbuild');
const v = require('valibot');

let z;
try {
  z = require('zod').z;
} catch {
  // Zod is an optional benchmark comparison target.
}

// ── 1. Reference hand-written parsers (commit 98a733aa) ──

function legacyParseSay(m) {
  if (typeof m.text !== 'string') return undefined;
  let creationTaskId;
  if (m.creationTaskId !== undefined) {
    if (typeof m.creationTaskId !== 'string' || m.creationTaskId.trim().length === 0) {
      return undefined;
    }
    creationTaskId = m.creationTaskId;
  }
  return creationTaskId !== undefined
    ? { kind: 'say', text: m.text, creationTaskId }
    : { kind: 'say', text: m.text };
}

function legacyParseReply(m) {
  if (
    typeof m.callId !== 'string' ||
    m.callId.trim().length === 0 ||
    typeof m.text !== 'string' ||
    typeof m.attemptId !== 'number' ||
    !Number.isInteger(m.attemptId) ||
    m.attemptId <= 0 ||
    typeof m.companionKey !== 'string' ||
    m.companionKey.trim().length === 0 ||
    typeof m.session !== 'string' ||
    m.session.trim().length === 0 ||
    typeof m.generation !== 'number' ||
    !Number.isInteger(m.generation) ||
    m.generation < 0 ||
    typeof m.webviewId !== 'string' ||
    m.webviewId.trim().length === 0
  ) {
    return undefined;
  }
  return {
    kind: 'reply',
    callId: m.callId,
    text: m.text,
    attemptId: m.attemptId,
    companionKey: m.companionKey,
    session: m.session,
    generation: m.generation,
    webviewId: m.webviewId,
  };
}

function legacyParseOpen(m) {
  if (
    typeof m.session !== 'string' ||
    m.session.trim().length === 0 ||
    typeof m.callId !== 'string' ||
    m.callId.trim().length === 0
  ) {
    return undefined;
  }
  return {
    kind: 'open',
    session: m.session,
    callId: m.callId,
    seq: typeof m.seq === 'number' ? m.seq : undefined,
  };
}

function legacyParseRows(m) {
  if (!Array.isArray(m.rows)) return undefined;
  if (typeof m.session !== 'string') return undefined;
  if (!Array.isArray(m.refs) || !m.refs.every((r) => typeof r === 'string')) return undefined;

  const rows = [];
  for (const r of m.rows) {
    if (!r || typeof r !== 'object') return undefined;
    const rowObj = r;
    if (
      typeof rowObj.who !== 'string' ||
      typeof rowObj.label !== 'string' ||
      typeof rowObj.text !== 'string' ||
      (rowObj.outputId !== undefined && typeof rowObj.outputId !== 'string')
    ) {
      return undefined;
    }
    rows.push(r);
  }

  let ask = null;
  if (m.ask !== undefined && m.ask !== null) {
    if (typeof m.ask !== 'object' || Array.isArray(m.ask)) return undefined;
    const askObj = m.ask;
    if (typeof askObj.callId !== 'string' || !askObj.callId) return undefined;
    if (typeof askObj.what !== 'string') return undefined;
    if (askObj.kind !== 'permission' && askObj.kind !== 'question') return undefined;
    if (
      askObj.options !== undefined &&
      (!Array.isArray(askObj.options) || !askObj.options.every((o) => typeof o === 'string'))
    ) {
      return undefined;
    }
    if (
      askObj.report !== undefined &&
      (!Array.isArray(askObj.report) ||
        !askObj.report.every(
          (item) =>
            item &&
            typeof item === 'object' &&
            typeof item.key === 'string' &&
            typeof item.text === 'string'
        ))
    ) {
      return undefined;
    }
    if (askObj.args !== undefined && typeof askObj.args !== 'string') return undefined;
    if (askObj.reason !== undefined && typeof askObj.reason !== 'string') return undefined;
    if (askObj.diff !== undefined && typeof askObj.diff !== 'string') return undefined;
    if (
      askObj.diffKind !== undefined &&
      askObj.diffKind !== 'sides' &&
      askObj.diffKind !== 'patch' &&
      askObj.diffKind !== 'none'
    ) {
      return undefined;
    }
    if (askObj.filePath !== undefined && typeof askObj.filePath !== 'string') return undefined;
    if (askObj.index !== undefined && typeof askObj.index !== 'number') return undefined;
    if (askObj.total !== undefined && typeof askObj.total !== 'number') return undefined;
    if (askObj.since !== undefined && typeof askObj.since !== 'string') return undefined;
    ask = m.ask;
  }

  const rowsMsg = {
    kind: 'rows',
    session: m.session,
    rows,
    ask,
    refs: m.refs,
  };
  if (typeof m.companionKey === 'string') rowsMsg.companionKey = m.companionKey;
  if (typeof m.generation === 'number') rowsMsg.generation = m.generation;
  if (typeof m.webviewId === 'string') rowsMsg.webviewId = m.webviewId;
  return rowsMsg;
}

function legacyParseReplyResult(m) {
  if (
    typeof m.callId !== 'string' ||
    m.callId.trim().length === 0 ||
    typeof m.attemptId !== 'number' ||
    !Number.isInteger(m.attemptId) ||
    m.attemptId <= 0 ||
    typeof m.ok !== 'boolean' ||
    typeof m.companionKey !== 'string' ||
    m.companionKey.trim().length === 0 ||
    typeof m.session !== 'string' ||
    m.session.trim().length === 0 ||
    typeof m.generation !== 'number' ||
    !Number.isInteger(m.generation) ||
    m.generation < 0 ||
    typeof m.webviewId !== 'string' ||
    m.webviewId.trim().length === 0
  ) {
    return undefined;
  }
  return {
    kind: 'replyResult',
    callId: m.callId,
    attemptId: m.attemptId,
    ok: m.ok,
    companionKey: m.companionKey,
    session: m.session,
    generation: m.generation,
    webviewId: m.webviewId,
    error: typeof m.error === 'string' ? m.error : undefined,
    text: typeof m.text === 'string' ? m.text : undefined,
  };
}

// ── 2. Valibot Schemas (v1.5.0) ──

const NonEmptyStringV = v.pipe(
  v.string(),
  v.check((s) => s.trim().length > 0)
);

const PositiveIntegerV = v.pipe(
  v.number(),
  v.integer(),
  v.minValue(1)
);

const NonNegativeIntegerV = v.pipe(
  v.number(),
  v.integer(),
  v.minValue(0)
);

const SaySchemaV = v.object({
  kind: v.literal('say'),
  text: v.string(),
  creationTaskId: v.optional(NonEmptyStringV),
});

const ReplySchemaV = v.object({
  kind: v.literal('reply'),
  callId: NonEmptyStringV,
  text: v.string(),
  attemptId: PositiveIntegerV,
  companionKey: NonEmptyStringV,
  session: NonEmptyStringV,
  generation: NonNegativeIntegerV,
  webviewId: NonEmptyStringV,
});

const OpenSchemaV = v.object({
  kind: v.literal('open'),
  session: NonEmptyStringV,
  callId: NonEmptyStringV,
  seq: v.optional(v.custom((_val) => true)),
});

const PaintedRowSchemaV = v.object({
  who: v.string(),
  label: v.string(),
  text: v.string(),
  outputId: v.optional(v.string()),
});

const AskReportItemSchemaV = v.object({
  key: v.string(),
  text: v.string(),
});

const AskSchemaV = v.object({
  callId: v.pipe(v.string(), v.check((s) => s.length > 0)),
  what: v.string(),
  kind: v.union([v.literal('permission'), v.literal('question')]),
  options: v.optional(v.array(v.string())),
  report: v.optional(v.array(AskReportItemSchemaV)),
  args: v.optional(v.string()),
  reason: v.optional(v.string()),
  diff: v.optional(v.string()),
  diffKind: v.optional(v.union([v.literal('sides'), v.literal('patch'), v.literal('none')])),
  filePath: v.optional(v.string()),
  index: v.optional(v.number()),
  total: v.optional(v.number()),
  since: v.optional(v.string()),
});

const RowsSchemaV = v.object({
  kind: v.literal('rows'),
  session: v.string(),
  rows: v.array(PaintedRowSchemaV),
  ask: v.nullable(AskSchemaV),
  refs: v.array(v.string()),
  companionKey: v.optional(v.string()),
  generation: v.optional(v.number()),
  webviewId: v.optional(v.string()),
});

const ReplyResultSchemaV = v.object({
  kind: v.literal('replyResult'),
  callId: NonEmptyStringV,
  attemptId: PositiveIntegerV,
  ok: v.boolean(),
  companionKey: NonEmptyStringV,
  session: NonEmptyStringV,
  generation: NonNegativeIntegerV,
  webviewId: NonEmptyStringV,
  error: v.optional(v.string()),
  text: v.optional(v.string()),
});

function valibotParse(schema, raw) {
  const res = v.safeParse(schema, raw);
  if (!res.success) return undefined;
  const out = res.output;
  if (out.kind === 'open') {
    return {
      kind: 'open',
      session: out.session,
      callId: out.callId,
      seq: typeof out.seq === 'number' ? out.seq : undefined,
    };
  }
  return out;
}

// ── 3. Zod Schemas (Optional Comparison) ──

let zodParse = null;
let SaySchemaZ, ReplySchemaZ, OpenSchemaZ, RowsSchemaZ, ReplyResultSchemaZ;

if (z) {
  const NonEmptyStringZ = z.string().refine((s) => s.trim().length > 0);
  const PositiveIntegerZ = z.number().int().min(1);
  const NonNegativeIntegerZ = z.number().int().min(0);

  SaySchemaZ = z.object({
    kind: z.literal('say'),
    text: z.string(),
    creationTaskId: NonEmptyStringZ.optional(),
  });

  ReplySchemaZ = z.object({
    kind: z.literal('reply'),
    callId: NonEmptyStringZ,
    text: z.string(),
    attemptId: PositiveIntegerZ,
    companionKey: NonEmptyStringZ,
    session: NonEmptyStringZ,
    generation: NonNegativeIntegerZ,
    webviewId: NonEmptyStringZ,
  });

  OpenSchemaZ = z.object({
    kind: z.literal('open'),
    session: NonEmptyStringZ,
    callId: NonEmptyStringZ,
    seq: z.any().optional(),
  });

  const PaintedRowSchemaZ = z.object({
    who: z.string(),
    label: z.string(),
    text: z.string(),
    outputId: z.string().optional(),
  });

  const AskReportItemSchemaZ = z.object({
    key: z.string(),
    text: z.string(),
  });

  const AskSchemaZ = z.object({
    callId: z.string().min(1),
    what: z.string(),
    kind: z.union([z.literal('permission'), z.literal('question')]),
    options: z.array(z.string()).optional(),
    report: z.array(AskReportItemSchemaZ).optional(),
    args: z.string().optional(),
    reason: z.string().optional(),
    diff: z.string().optional(),
    diffKind: z.union([z.literal('sides'), z.literal('patch'), z.literal('none')]).optional(),
    filePath: z.string().optional(),
    index: z.number().optional(),
    total: z.number().optional(),
    since: z.string().optional(),
  });

  RowsSchemaZ = z.object({
    kind: z.literal('rows'),
    session: z.string(),
    rows: z.array(PaintedRowSchemaZ),
    ask: AskSchemaZ.nullable(),
    refs: z.array(z.string()),
    companionKey: z.string().optional(),
    generation: z.number().optional(),
    webviewId: z.string().optional(),
  });

  ReplyResultSchemaZ = z.object({
    kind: z.literal('replyResult'),
    callId: NonEmptyStringZ,
    attemptId: PositiveIntegerZ,
    ok: z.boolean(),
    companionKey: NonEmptyStringZ,
    session: NonEmptyStringZ,
    generation: NonNegativeIntegerZ,
    webviewId: NonEmptyStringZ,
    error: z.string().optional(),
    text: z.string().optional(),
  });

  zodParse = function (schema, raw) {
    const res = schema.safeParse(raw);
    if (!res.success) return undefined;
    const out = res.data;
    if (out.kind === 'open') {
      return {
        kind: 'open',
        session: out.session,
        callId: out.callId,
        seq: typeof out.seq === 'number' ? out.seq : undefined,
      };
    }
    return out;
  };
}

// ── 4. Test Cases for Representative Messages ──

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
      name: 'invalid generation float',
      raw: {
        kind: 'reply',
        callId: 'c',
        text: 't',
        attemptId: 1,
        companionKey: 'k',
        session: 's',
        generation: 0.5,
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
    { name: 'valid non-numeric seq falls back to undefined', raw: { kind: 'open', session: 's1', callId: 'c1', seq: '12' } },
    { name: 'valid null seq falls back to undefined', raw: { kind: 'open', session: 's1', callId: 'c1', seq: null } },
  ],
  rows: [
    {
      name: 'valid minimal rows',
      raw: {
        kind: 'rows',
        session: 's1',
        rows: [{ who: 'user', label: 'You', text: 'hi' }],
        ask: null,
        refs: ['file.ts'],
      },
    },
    {
      name: 'valid with ask and options',
      raw: {
        kind: 'rows',
        session: 's1',
        rows: [],
        ask: {
          callId: 'c1',
          what: 'Choose',
          kind: 'question',
          options: ['a', 'b'],
        },
        refs: [],
        companionKey: 'k1',
        generation: 1,
        webviewId: 'w1',
      },
    },
    {
      name: 'invalid rows non-array',
      raw: { kind: 'rows', session: 's1', rows: 'not-an-array', ask: null, refs: [] },
    },
    {
      name: 'invalid refs element non-string',
      raw: { kind: 'rows', session: 's1', rows: [], ask: null, refs: [123] },
    },
    {
      name: 'invalid ask missing callId',
      raw: { kind: 'rows', session: 's1', rows: [], ask: { what: 'test', kind: 'question' }, refs: [] },
    },
  ],
  replyResult: [
    {
      name: 'valid success',
      raw: {
        kind: 'replyResult',
        callId: 'c1',
        attemptId: 1,
        ok: true,
        companionKey: 'k1',
        session: 's1',
        generation: 0,
        webviewId: 'w1',
      },
    },
    {
      name: 'valid with error and text',
      raw: {
        kind: 'replyResult',
        callId: 'c1',
        attemptId: 2,
        ok: false,
        companionKey: 'k1',
        session: 's1',
        generation: 1,
        webviewId: 'w1',
        error: 'Timeout',
        text: 'Draft text',
      },
    },
    {
      name: 'invalid ok non-boolean',
      raw: {
        kind: 'replyResult',
        callId: 'c1',
        attemptId: 1,
        ok: 'true',
        companionKey: 'k1',
        session: 's1',
        generation: 0,
        webviewId: 'w1',
      },
    },
  ],
};

// ── 5. Run Parity Verification ──

console.log('=== 1. PARITY VERIFICATION (Legacy vs Valibot' + (z ? ' vs Zod' : '') + ') ===\n');

const currentParsers = {
  say: legacyParseSay,
  reply: legacyParseReply,
  open: legacyParseOpen,
  rows: legacyParseRows,
  replyResult: legacyParseReplyResult,
};

const valibotSchemas = {
  say: SaySchemaV,
  reply: ReplySchemaV,
  open: OpenSchemaV,
  rows: RowsSchemaV,
  replyResult: ReplyResultSchemaV,
};

let valibotMismatches = 0;
let zodMismatches = 0;

for (const [kind, cases] of Object.entries(testCases)) {
  console.log(`Checking [${kind}] (${cases.length} cases)...`);
  const curP = currentParsers[kind];
  const vSchema = valibotSchemas[kind];

  for (const tc of cases) {
    const curRes = curP(tc.raw);
    const vRes = valibotParse(vSchema, tc.raw);

    const vMatch = (curRes === undefined && vRes === undefined) ||
      (JSON.stringify(curRes) === JSON.stringify(vRes));
    if (!vMatch) {
      console.log(`  [Valibot MISMATCH] ${tc.name}`);
      console.log(`    Expected:`, curRes);
      console.log(`    Actual:  `, vRes);
      valibotMismatches++;
    }

    if (z && zodParse) {
      const zSchema = {
        say: SaySchemaZ,
        reply: ReplySchemaZ,
        open: OpenSchemaZ,
        rows: RowsSchemaZ,
        replyResult: ReplyResultSchemaZ,
      }[kind];
      const zRes = zodParse(zSchema, tc.raw);
      const zMatch = (curRes === undefined && zRes === undefined) ||
        (JSON.stringify(curRes) === JSON.stringify(zRes));
      if (!zMatch) {
        console.log(`  [Zod MISMATCH] ${tc.name}`);
        console.log(`    Expected:`, curRes);
        console.log(`    Actual:  `, zRes);
        zodMismatches++;
      }
    }
  }
}

console.log(`\nParity result:`);
console.log(`  Valibot mismatches: ${valibotMismatches}`);
if (z) {
  console.log(`  Zod mismatches:     ${zodMismatches}`);
} else {
  console.log(`  Zod:                not installed (skipped)`);
}

// ── 6. Benchmark Parsing Performance ──

console.log('\n=== 2. PARSING PERFORMANCE (10,000 runs) ===');
const ITER = 10000;
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

// Hand-written legacy
let t0 = performance.now();
for (let i = 0; i < ITER; i++) {
  legacyParseSay(sampleSay);
  legacyParseReply(sampleReply);
}
const curTime = performance.now() - t0;

// Valibot
t0 = performance.now();
for (let i = 0; i < ITER; i++) {
  valibotParse(SaySchemaV, sampleSay);
  valibotParse(ReplySchemaV, sampleReply);
}
const vTime = performance.now() - t0;

console.log(`  Hand-written parser  : ${curTime.toFixed(2)} ms`);
console.log(`  Valibot (v1.5.0)     : ${vTime.toFixed(2)} ms (${(vTime / curTime).toFixed(1)}x hand-written)`);

if (z && zodParse) {
  t0 = performance.now();
  for (let i = 0; i < ITER; i++) {
    zodParse(SaySchemaZ, sampleSay);
    zodParse(ReplySchemaZ, sampleReply);
  }
  const zTime = performance.now() - t0;
  console.log(`  Zod                  : ${zTime.toFixed(2)} ms (${(zTime / curTime).toFixed(1)}x hand-written)`);
}

// ── 7. Bundle Size Measurement ──

console.log('\n=== 3. BUNDLE SIZE MEASUREMENT (esbuild tree-shaking) ===');

const tmpDir = path.join(process.cwd(), 'clients', 'vscode', 'out', 'benchmark_tmp');
fs.mkdirSync(tmpDir, { recursive: true });

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
const OpenSchema = v.object({ kind: v.literal('open'), session: NonEmptyStringV, callId: NonEmptyStringV, seq: v.optional(v.custom(() => true)) });
const WebviewToHostSchema = v.union([SaySchema, ReplySchema, OpenSchema]);
export function parse(raw) {
  const res = v.safeParse(WebviewToHostSchema, raw);
  return res.success ? res.output : undefined;
}
`;

const vFile = path.join(tmpDir, 'valibot_entry.js');
fs.writeFileSync(vFile, valibotEntry);

function measureBundle(entryPath, minify = false) {
  const res = esbuild.buildSync({
    entryPoints: [entryPath],
    bundle: true,
    format: 'cjs',
    minify,
    write: false,
    nodePaths: [path.join(process.cwd(), 'clients', 'vscode', 'node_modules')],
  });
  return res.outputFiles[0].contents.length;
}

const vRawSize = measureBundle(vFile, false);
const vMinSize = measureBundle(vFile, true);

console.log(`  Valibot (v1.5.0):`);
console.log(`    Unminified bundle : ${vRawSize.toLocaleString()} bytes (${(vRawSize / 1024).toFixed(2)} KB)`);
console.log(`    Minified bundle   : ${vMinSize.toLocaleString()} bytes (${(vMinSize / 1024).toFixed(2)} KB)`);

if (z) {
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

  console.log(`  Zod (v4.6.5):`);
  console.log(`    Unminified bundle : ${zRawSize.toLocaleString()} bytes (${(zRawSize / 1024).toFixed(2)} KB)`);
  console.log(`    Minified bundle   : ${zMinSize.toLocaleString()} bytes (${(zMinSize / 1024).toFixed(2)} KB)`);
  console.log(`  Difference: Zod is +${((zMinSize - vMinSize) / 1024).toFixed(2)} KB larger (${(zMinSize / vMinSize).toFixed(1)}x)`);
}

// Clean up temp
try {
  fs.rmSync(tmpDir, { recursive: true, force: true });
} catch {
  // ignore
}
console.log('\n=== DONE ===\n');
