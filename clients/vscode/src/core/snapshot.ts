/**
 * Immutable snapshot repository for virtual documents.
 *
 * Preserves the state at the moment of creation without reading the live disk
 * or mutating across subsequent operations.
 *
 * Invariant: Does NOT depend on vscode or DOM APIs.
 *
 * Eviction policy:
 *  - Immutability: Once stored for a key, subsequent calls to put() with the same key
 *    preserve the initial content rather than overwriting.
 *  - Insertion-Order Eviction: When capacity is exceeded, the oldest unpinned entry by
 *    insertion order (FIFO) is evicted. Reads (get/has) do not update order (not LRU).
 *  - Pinned Tab Protection: Entries that are currently pinned (e.g. open editor tabs)
 *    or temporarily protected (in-flight open operations) are preserved rather than evicted.
 *  - Temporary Overflow: If all entries are pinned, allows capacity to be exceeded until unpinned.
 *  - Single Canonical Key: Only the canonical URI string is stored.
 */
export class ImmutableSnapshotStore<T = string> {
  private readonly store = new Map<string, T>();
  private readonly order: string[] = [];
  private readonly tempPinned = new Map<string, number>();

  constructor(
    private readonly maxEntries: number = 100,
    private readonly isPinned?: (key: string) => boolean
  ) {}

  /**
   * Temporarily protects keys (e.g. both sides of a diff or in-flight open operations)
   * from eviction until the editor opens the document or fails.
   *
   * Supports nested/concurrent protections of the same keys using reference counting,
   * ensuring that the completion/failure of one operation does not prematurely unprotect
   * the other operation's documents.
   *
   * Returns an idempotent release function safe against multiple calls.
   */
  protectTemp(keys: string[]): () => void {
    for (const k of keys) {
      const current = this.tempPinned.get(k) ?? 0;
      this.tempPinned.set(k, current + 1);
    }
    let released = false;
    return () => {
      if (released) return; // Idempotent: multiple calls do nothing
      released = true;
      for (const k of keys) {
        const count = this.tempPinned.get(k) ?? 0;
        if (count <= 1) {
          this.tempPinned.delete(k);
        } else {
          this.tempPinned.set(k, count - 1);
        }
      }
      this.evictExcess();
    };
  }

  put(key: string, content: T): boolean {
    if (this.store.has(key)) {
      // Truly immutable: keep initial content, reject/ignore overwrite
      return false;
    }
    this.order.push(key);
    this.store.set(key, content);

    while (this.order.length > this.maxEntries) {
      const evictIndex = this.order.slice(0, -1).findIndex((k) => !this.isProtected(k));
      if (evictIndex < 0) {
        // All older entries are currently protected; allow limit to be exceeded
        break;
      }
      const [evicted] = this.order.splice(evictIndex, 1);
      this.store.delete(evicted);
    }
    return true;
  }

  /**
   * Evaluates protection status and prunes unpinned entries if cache size exceeds maxEntries.
   * Called when documents/tabs close, or when temporary protection is released.
   */
  evictExcess(): number {
    let count = 0;
    while (this.order.length > this.maxEntries) {
      const evictIndex = this.order.findIndex((k) => !this.isProtected(k));
      if (evictIndex < 0) {
        // All tracked entries are currently pinned (open in tabs or in-flight); protect them
        break;
      }
      const [evicted] = this.order.splice(evictIndex, 1);
      this.store.delete(evicted);
      count++;
    }
    return count;
  }

  private isProtected(key: string): boolean {
    if (this.tempPinned.has(key)) return true;
    return this.isPinned ? this.isPinned(key) : false;
  }

  get(key: string): T | undefined {
    return this.store.get(key);
  }

  has(key: string): boolean {
    return this.store.has(key);
  }

  delete(key: string): boolean {
    const idx = this.order.indexOf(key);
    if (idx >= 0) this.order.splice(idx, 1);
    this.tempPinned.delete(key);
    return this.store.delete(key);
  }

  get size(): number {
    return this.store.size;
  }

  get tempPinnedSize(): number {
    return this.tempPinned.size;
  }

  clear(): void {
    this.store.clear();
    this.order.length = 0;
    this.tempPinned.clear();
  }
}
