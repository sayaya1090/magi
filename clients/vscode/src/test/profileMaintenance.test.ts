import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vm from 'node:vm';

function ui(pick: (rows: any[]) => any) {
  const exports: any = {}; const notices: string[] = [];
  const api = { l10n: { t: (s: string) => s }, window: {
    showQuickPick: async (rows: any[]) => pick(rows),
    showWarningMessage: async (s: string) => { notices.push(s); },
    showInformationMessage: async (s: string) => { notices.push(s); },
  } };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../ide/profileMaintenance.js'), 'utf8'), { exports, require: () => api });
  return { run: exports.profileMaintenance, notices };
}
test('maintenance status does not request a model refresh until selected', async () => {
  const view = ui(rows => { assert.ok(rows.some(r => r.label === 'Waiting for idle or cooldown')); return undefined; });
  const calls: any[] = [];
  await view.run({ caps: async () => new Set(['profile-maintenance']), ask: async (method: string, extra?: any) => { calls.push({ method, extra }); return { ok: true, out: JSON.stringify({ enabled: true, pending: true, running: false, id: 'internal', history: [] }) }; } }, 'source');
  assert.equal(calls.length, 1); assert.equal(calls[0].extra, undefined);
});
test('maintenance refresh uses selected source chat and reports refusal', async () => {
  const view = ui(rows => rows.find(r => r.refresh));
  let request: any;
  await view.run({ caps: async () => new Set(['profile-maintenance']), ask: async (_: string, extra?: any) => {
    if (extra) { request = extra; return { ok: false, error: 'no new evidence' }; }
    return { ok: true, out: JSON.stringify({ enabled: true, history: [] }) };
  } }, 'source');
  assert.equal(request.session, 'source'); assert.equal(request.name, 'refresh');
  assert.ok(view.notices.includes('no new evidence'));
});
