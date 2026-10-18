import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vm from 'node:vm';

async function fixture(action?: string, stale = false, unavailable = false) {
  const exports: any = {}; let command: any; let tick: any;
  const calls: any[] = []; const notices: string[] = [];
  const prompt = { session: 'delegated', requester: 'handbook', prompt: { id: 'call', kind: 'permission', what: 'bash', args: { command: 'git status' } } };
  const api = { l10n: { t: (s: string) => s }, StatusBarAlignment: { Left: 1 }, window: {
    createStatusBarItem: () => ({ show() {}, hide() {}, dispose() {} }),
    showWarningMessage: async (s: string, options: any) => { notices.push(s); return options?.modal ? action : undefined; },
    showErrorMessage: async (s: string) => { notices.push(s); },
    showQuickPick: async (rows: any[]) => rows[0],
  }, commands: { registerCommand: (_: string, handler: any) => { command = handler; return { dispose() {} }; } } };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../ide/approvals.js'), 'utf8'), {
    exports, require: () => api, setInterval: (f: any) => { tick = f; return 1; }, clearInterval() {},
  });
  let reads = 0;
  const disposable = exports.approvalSupport({ session: 'current', ask: async (method: string, extra: any) => {
    calls.push({ method, extra });
    if (method !== 'jobs') return { ok: true };
    reads++;
    if (unavailable && reads >= 4) return { ok: false, error: 'connection failed' };
    return { ok: true, jobs: { approvals: stale && reads >= 4 ? [] : [prompt] } };
  } });
  await new Promise(resolve => setImmediate(resolve));
  tick();
  await new Promise(resolve => setImmediate(resolve));
  await command();
  disposable.dispose();
  return { calls, notices };
}
test('closing delegated approval review leaves request pending and only notifies once', async () => {
  const { calls, notices } = await fixture();
  assert.ok(calls.every(c => c.method === 'jobs'));
  assert.equal(notices.filter(s => s.startsWith('Magi:')).length, 1);
});
test('delegated approval uses original session and call id, never selected chat', async () => {
  const { calls } = await fixture('Allow once');
  const responses = calls.filter(c => c.method === 'permission');
  assert.equal(responses.length, 1);
  assert.equal(responses[0].extra.session, 'delegated');
  assert.equal(responses[0].extra.callId, 'call');
  assert.equal(responses[0].extra.decision, 'allow');
});
test('stale delegated approval cannot be answered', async () => {
  const { calls } = await fixture('Reject', true);
  assert.ok(calls.every(c => c.method === 'jobs'));
});

test('failed verification is not reported as an expired or rejected request', async () => {
  const { calls, notices } = await fixture('Allow once', false, true);
  assert.ok(calls.every(c => c.method === 'jobs'));
  assert.ok(notices.includes('Response was not sent: {0}'));
  assert.ok(!notices.includes('This request is no longer pending.'));
});
