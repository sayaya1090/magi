import * as fs from 'node:fs/promises';
import * as path from 'node:path';
import { randomUUID } from 'node:crypto';
import { DraftArchive, parseDraftArchive } from '../core/draft_archive';

/** One exclusively-owned file. Cross-window ownership is the caller's responsibility. */
export class DraftArchiveStore {
  private tail: Promise<unknown> = Promise.resolve();
  constructor(private readonly file: string, private readonly workspace: string, private readonly owner: string) {}

  private enqueue<T>(work: () => Promise<T>): Promise<T> {
    const result = this.tail.then(work);
    // A failed write must not poison subsequent explicit retries.
    this.tail = result.then(() => undefined, () => undefined);
    return result;
  }

  private async read(): Promise<DraftArchive | undefined> {
    let raw: string;
    try { raw = await fs.readFile(this.file, 'utf8'); }
    catch (e) { if ((e as NodeJS.ErrnoException).code === 'ENOENT') return undefined; throw e; }
    const a = parseDraftArchive(raw, this.workspace);
    if (a.owner !== this.owner) throw new Error('Draft archive belongs to another owner');
    return a;
  }

  load(): Promise<DraftArchive | undefined> { return this.enqueue(() => this.read()); }

  save(snapshot: DraftArchive): Promise<void> {
    // Capture synchronously, before a later input edit can mutate the caller's objects.
    let raw: string;
    let copy: DraftArchive;
    try {
      raw = JSON.stringify(snapshot);
      copy = parseDraftArchive(raw, this.workspace);
      if (copy.owner !== this.owner) throw new Error('Draft archive belongs to another owner');
    } catch (e) { return Promise.reject(e); }
    return this.enqueue(async () => {
      const previous = await this.read(); // Never replace corrupt or foreign data with an empty archive.
      if (previous && copy.revision <= previous.revision) throw new Error('Stale draft archive revision');
      await fs.mkdir(path.dirname(this.file), { recursive: true });
      const temp = this.file + '.' + randomUUID() + '.tmp';
      try {
        const handle = await fs.open(temp, 'wx', 0o600);
        try { await handle.writeFile(raw, 'utf8'); await handle.sync(); }
        finally { await handle.close(); }
        await fs.rename(temp, this.file);
      } finally {
        await fs.rm(temp, { force: true });
      }
    });
  }
}
