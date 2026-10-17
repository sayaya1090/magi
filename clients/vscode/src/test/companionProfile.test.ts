import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as vm from 'node:vm';

function editor(quick: ((rows: any[]) => any)[], inputs: (string | undefined)[]) {
  const exports: any = {};
  const api = { l10n: { t: (s: string) => s }, window: {
    showQuickPick: async (rows: any[]) => quick.shift()?.(rows),
    showInputBox: async () => inputs.shift(),
    showErrorMessage: () => {},
  } };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../ide/companionProfile.js'), 'utf8'), { exports, require: () => api });
  return exports.editCompanionProfile as (raw: string) => Promise<string | undefined>;
}
const original = { purpose: 'workspace', responsibilities: ['sources'], tasks: [{ id: 'build', description: 'build', constraints: ['one; two'] }] };
test('workspace editor preserves task identity and unchanged limits when editing a description', async () => {
  const edit = editor([r => r.find(x => x.action === 'task'), r => r.find(x => x.value === 'edit'), r => r.find(x => x.action === 'save')], ['build and test', 'one; two']);
  const p = JSON.parse((await edit(JSON.stringify(original)))!);
  assert.equal(p.tasks[0].id, 'build');
  assert.equal(p.tasks[0].description, 'build and test');
  assert.deepEqual(p.tasks[0].constraints, ['one; two']);
  assert.equal(p.purpose, original.purpose);
});
test('workspace editor adds and removes offers before committing the draft', async () => {
  const edit = editor([r => r.find(x => x.action === 'add'), r => r.find(x => x.action === 'task' && x.index === 0), r => r.find(x => x.value === 'remove'), r => r.find(x => x.action === 'save')], ['run tests', 'local only']);
  const p = JSON.parse((await edit(JSON.stringify(original)))!);
  assert.equal(p.tasks.length, 1);
  assert.equal(p.tasks[0].description, 'run tests');
  assert.match(p.tasks[0].id, /^task-[a-z0-9-]+$/);
});
test('cancelling workspace editor does not submit an edited draft', async () => {
  const edit = editor([r => r.find(x => x.action === 'purpose'), () => undefined], ['changed']);
  assert.equal(await edit(JSON.stringify(original)), undefined);
});
