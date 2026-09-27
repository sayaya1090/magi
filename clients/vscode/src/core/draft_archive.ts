import * as v from 'valibot';

// Wire format shared with JetBrains. UI modes and live transport locks are not persisted.
const id = v.pipe(v.string(), v.check(s => s.trim().length > 0));
const counter = v.pipe(v.number(), v.check(n => Number.isSafeInteger(n) && n >= 0));
const ref = v.strictObject({ path: id, start: counter, end: counter });
const entry = v.strictObject({
  id, companionKey: id, sessionId: v.string(), creationTaskId: v.string(), callId: v.string(),
  kind: v.picklist(['general', 'question', 'attempt', 'recovery']),
  status: v.picklist(['draft', 'pending', 'unknown', 'failed']),
  text: v.string(), title: v.string(), reason: v.string(),
  version: counter, attemptId: v.string(), seq: counter, createdAt: counter, attempts: counter,
  refs: v.array(ref),
});
const schema = v.strictObject({
  schemaVersion: v.literal(1), workspace: id, owner: id, revision: counter,
  entries: v.array(entry),
  deleted: v.array(v.strictObject({ id, version: counter })),
});
export type DraftArchive = v.InferOutput<typeof schema>;
export type DraftArchiveEntry = DraftArchive['entries'][number];

/** Reject the complete archive on invalid input; callers must preserve the original file. */
export function parseDraftArchive(text: string, workspace: string): DraftArchive {
  const archive = v.parse(schema, JSON.parse(text));
  if (archive.workspace !== workspace) throw new Error('Draft workspace mismatch');
  const ids = new Set<string>();
  for (const e of archive.entries) {
    if (ids.has(e.id)) throw new Error('Duplicate draft id');
    ids.add(e.id);
    if (!e.sessionId.trim() && !e.creationTaskId.trim()) throw new Error('Missing draft context');
    if (e.kind === 'question' && !e.callId.trim()) throw new Error('Missing question id');
    if ((e.kind === 'attempt') !== (e.attemptId.trim().length > 0)) throw new Error('Invalid attempt identity');
    if ((e.status === 'pending' || e.status === 'unknown') && e.kind !== 'attempt') throw new Error('Invalid attempt status');
    if (e.kind === 'attempt' && e.status === 'draft') throw new Error('Invalid attempt status');
    if (!e.text.length && !e.refs.length) throw new Error('Empty draft');
    if (e.refs.some(r => r.end < r.start)) throw new Error('Invalid reference range');
  }
  return archive;
}

/** Recovery candidates only. This does not restore an input mode or send any RPC. */
export function restoreDraftArchive(archive: DraftArchive): DraftArchiveEntry[] {
  return archive.entries.filter(e => !archive.deleted.some(d => d.id === e.id && d.version >= e.version))
    .map(e => ({ ...e, refs: e.refs.map(r => ({ ...r })), status: e.status === 'pending' ? 'unknown' : e.status }));
}
