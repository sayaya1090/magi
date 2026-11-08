import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vm from 'node:vm';

function manager(picks: ((rows: any[], token: any) => any)[], confirm = true) {
  const warnings: string[] = [];
  const exports: any = {};
  class Cancel {
    private said: (() => void)[] = [];
    token = { onCancellationRequested: (f: () => void) => { this.said.push(f); } };
    cancel() { for (const f of this.said) f(); }
    dispose() {}
  }
  const api = { CancellationTokenSource: Cancel, ProgressLocation: { Notification: 1 }, l10n: { t: (s: string) => s }, window: {
    showQuickPick: async (rows: any[], _: any, token: any) => picks.shift()?.(rows, token),
    showWarningMessage: async (message: string, ...args: any[]) => { warnings.push(message); return confirm && args.length > 1 ? args[1] : undefined; },
    showInformationMessage: async () => {},
    withProgress: async (_: any, run: () => Promise<any>) => run(),
  } };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../ide/knowledge.js'), 'utf8'), { exports, require: () => api });
  const run = (c: any, ...rest: any[]) => exports.manageKnowledge({ watchSlots: async () => ({ dispose() {} }), ...c }, ...rest);
  return { run, warnings };
}
const item = { name: 'managed:one', description: 'Compiler', kind: 'skill', managed: true, revision: 'displayed-revision', pinned: false, archived: false, application_count: 2 };
test('knowledge action sends displayed identity and revision, then reloads after conflict', async () => {
  const ui = manager([r => r[0], r => r.find(x => x.action === 'pin'), () => undefined]);
  const calls: any[] = [];
  await ui.run({ caps: async () => new Set(['knowledge']), ask: async (method: string, extra?: any) => {
    calls.push({ method, extra });
    return extra ? { ok: false, error: 'revision conflict' } : { ok: true, knowledge: [item] };
  } });
  assert.equal(calls.length, 3);
  assert.deepEqual(JSON.parse(JSON.stringify(calls[1].extra)), { args: { name: item.name, revision: item.revision, action: 'pin' } });
  assert.ok(ui.warnings.some(w => w.includes('list will reload')));
});
test('archive cancellation leaves project knowledge unchanged', async () => {
  const ui = manager([r => r[0], r => r.find(x => x.action === 'archive'), () => undefined], false);
  let writes = 0;
  await ui.run({ caps: async () => new Set(['knowledge']), ask: async (_: string, extra?: any) => {
    if (extra) writes++; return { ok: true, knowledge: [item] };
  } });
  assert.equal(writes, 0);
});
test('legacy entries offer no retention mutations and unsupported companions are not called', async () => {
  const ui = manager([r => r[0], r => { assert.equal(r.length, 1); assert.equal(r[0].action, ''); return undefined; }]);
  await ui.run({ caps: async () => new Set(['knowledge']), ask: async () => ({ ok: true, knowledge: [{ ...item, managed: false }] }) });
  await manager([]).run({ caps: async () => null, ask: async () => { assert.fail('unsupported companion called'); } });
});

test('legacy registration confirms the reviewed hash without claiming verification', async () => {
  const ui = manager([r => r[0], r => r.find(x => x.action === 'import'), () => undefined]);
  let action: any;
  await ui.run({ caps: async () => new Set(['knowledge']), ask: async (_: string, extra?: any) => {
    if (extra) action = extra.args;
    return { ok: true, knowledge: [{ ...item, name: 'legacy:handmade', managed: false, importable: true }] };
  } });
  assert.equal(action.action, 'import');
  assert.equal(action.revision, item.revision);
  assert.equal(action.name, 'legacy:handmade');
  assert.ok(ui.warnings.some(w => w.includes('does not mark the skill as verified')));
});

test('a slot change reloads the open list with the new values', async () => {
  let fire = () => {};
  let watching = 0;
  const seen: string[] = [];
  const ui = manager([
    (rows, token) => new Promise(done => { seen.push(rows[0].label); token.onCancellationRequested(() => done(undefined)); fire(); }),
    rows => { seen.push(rows[0].label); return undefined; },
  ]);
  let reads = 0;
  await ui.run({
    caps: async () => new Set(['knowledge']),
    watchSlots: async (changed: () => void) => { fire = changed; watching++; return { dispose() { watching--; } }; },
    ask: async () => ({ ok: true, knowledge: [], slots: [{ id: 's', plugin: 'p', title: `read ${++reads}` }] }),
  });
  assert.equal(reads, 2);
  assert.deepEqual(seen, ['read 1', 'read 2']);
  assert.equal(watching, 0);
});

test('a slot action names the slot it was drawn from', async () => {
  const ui = manager([rows => rows.find(r => r.slotAction), () => undefined]);
  const calls: any[] = [];
  await ui.run({
    caps: async () => new Set(['knowledge']),
    ask: async (_: string, extra?: any) => {
      if (extra) calls.push(JSON.parse(JSON.stringify(extra.args)));
      return { ok: true, knowledge: [], slots: [{ id: 'second', plugin: 'p', title: 't', actions: [{ id: 'go', label: 'Go' }] }] };
    },
  });
  assert.deepEqual(calls, [{ name: 'p', action: 'go', slot: 'second' }]);
});
