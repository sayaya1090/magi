import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { parseDraftArchive, restoreDraftArchive } from '../core/draft_archive';
const cases = JSON.parse(readFileSync(resolve(__dirname, '../../../contract/draft-archive-fixtures.json'), 'utf8'));
for (const c of cases) test(`draft archive shared contract: ${c.name}`, () => {
  if (!c.valid) { assert.throws(() => parseDraftArchive(c.raw, c.workspace)); return; }
  const a = parseDraftArchive(c.raw, c.workspace);
  const before = JSON.stringify(a);
  assert.deepEqual(restoreDraftArchive(a), c.expected);
  assert.equal(JSON.stringify(a), before, 'restore must not mutate the stored snapshot');
  assert.deepEqual(parseDraftArchive(JSON.stringify(a), c.workspace), a);
});
