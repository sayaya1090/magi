import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'node:fs/promises';
import * as path from 'node:path';
import * as os from 'node:os';
import { DraftArchiveStore } from '../ide/draft_archive_store';
import { DraftArchive } from '../core/draft_archive';
const snapshot = (revision: number): DraftArchive => ({ schemaVersion: 1, workspace: 'file:///work', owner: 'owner', revision, entries: [], deleted: [] });

test('archive store survives recreation and rejects stale, corrupt and foreign snapshots', async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'magi-drafts-'));
  try {
    const file = path.join(dir, 'drafts.json');
    const s = new DraftArchiveStore(file, 'file:///work', 'owner');
    assert.equal(await s.load(), undefined);
    await s.save(snapshot(1));
    assert.deepEqual(await new DraftArchiveStore(file, 'file:///work', 'owner').load(), snapshot(1));
    await assert.rejects(s.save(snapshot(1)), /Stale/);
    await assert.rejects(new DraftArchiveStore(file, 'file:///elsewhere', 'owner').load());
    await assert.rejects(new DraftArchiveStore(file, 'file:///work', 'other').save({ ...snapshot(2), owner: 'other' }));
    assert.deepEqual(await s.load(), snapshot(1));
    await fs.writeFile(file, '{broken');
    await assert.rejects(s.save(snapshot(3)));
    assert.equal(await fs.readFile(file, 'utf8'), '{broken');
    await fs.writeFile(file, JSON.stringify(snapshot(1)));
    await s.save(snapshot(4));
    assert.equal((await s.load())?.revision, 4);
    assert.deepEqual(await fs.readdir(dir), ['drafts.json']);
  } finally { await fs.rm(dir, { recursive: true, force: true }); }
});

test('queued saves capture input and cannot roll revisions back', async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'magi-drafts-'));
  try {
    const s = new DraftArchiveStore(path.join(dir, 'drafts.json'), 'file:///work', 'owner');
    const a = snapshot(1);
    const first = s.save(a);
    a.revision = 99;
    const second = s.save(snapshot(2));
    const stale = assert.rejects(s.save(snapshot(1)), /Stale/);
    await Promise.all([first, second, stale]);
    assert.equal((await s.load())?.revision, 2);
  } finally { await fs.rm(dir, { recursive: true, force: true }); }
});

test('filesystem failure is returned and explicit retry succeeds', async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'magi-drafts-'));
  try {
    const blocked = path.join(dir, 'blocked');
    await fs.writeFile(blocked, 'keep');
    const s = new DraftArchiveStore(path.join(blocked, 'drafts.json'), 'file:///work', 'owner');
    await assert.rejects(s.save(snapshot(1)));
    assert.equal(await fs.readFile(blocked, 'utf8'), 'keep');
    await fs.unlink(blocked);
    await s.save(snapshot(1));
    assert.deepEqual(await s.load(), snapshot(1));
  } finally { await fs.rm(dir, { recursive: true, force: true }); }
});
