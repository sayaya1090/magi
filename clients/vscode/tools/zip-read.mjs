import { mkdir, writeFile, readFile } from 'node:fs/promises';
import path from 'node:path';
import { inflateRawSync, crc32 } from 'node:zlib';

/**
 * Read a .vsix (a zip) without an outside tool.
 *
 * ⚠ **The verifier used to shell out to `unzip`, and its own error message said what that costs:**
 * "requires the 'unzip' utility (standard on macOS and Linux/Ubuntu CI)". On Windows it is not
 * standard — Git for Windows ships `unzip` but plenty of machines have no Git, and then the VSIX
 * cannot be verified at all. Measured 2026-09-19: this machine passed only because Git's copy
 * happened to be on PATH.
 *
 * The other half of the same tool already moved in here (the test fixtures build their archives in
 * process, because `zip` does not exist on Windows at all and `Compress-Archive` writes backslash
 * separators). This is the reading half, so packaging and verification now need nothing but node.
 *
 * Deliberately small: enough of the format to read what vsce writes — stored and deflated entries,
 * no encryption, no zip64. Anything else is refused by name rather than half-read.
 */

const EOCD_SIG = 0x06054b50;
const CENTRAL_SIG = 0x02014b50;
const LOCAL_SIG = 0x04034b50;

/** The end-of-central-directory record, which is the only fixed point a zip has. */
function findEndOfCentralDirectory(buf) {
  // It is last, but a trailing comment may follow it — so scan back over the largest comment the
  // format allows (0xffff) plus the record itself.
  const from = Math.max(0, buf.length - (0xffff + 22));
  for (let i = buf.length - 22; i >= from; i--) {
    if (buf.readUInt32LE(i) === EOCD_SIG) return i;
  }
  throw new Error('not a zip archive: no end-of-central-directory record');
}

/**
 * Every entry the archive declares, read from the central directory (the authority; local headers
 * can disagree and a reader that trusts them is how zip smuggling works).
 */
export async function listZipEntries(archivePath) {
  const buf = await readFile(archivePath);
  const eocd = findEndOfCentralDirectory(buf);
  const count = buf.readUInt16LE(eocd + 10);
  let at = buf.readUInt32LE(eocd + 16);

  const entries = [];
  for (let i = 0; i < count; i++) {
    if (buf.readUInt32LE(at) !== CENTRAL_SIG) {
      throw new Error(`corrupt central directory at entry ${i} of ${archivePath}`);
    }
    const method = buf.readUInt16LE(at + 10);
    const crc = buf.readUInt32LE(at + 16);
    const compressedSize = buf.readUInt32LE(at + 20);
    const size = buf.readUInt32LE(at + 24);
    const nameLen = buf.readUInt16LE(at + 28);
    const extraLen = buf.readUInt16LE(at + 30);
    const commentLen = buf.readUInt16LE(at + 32);
    const localOffset = buf.readUInt32LE(at + 42);
    const name = buf.subarray(at + 46, at + 46 + nameLen).toString('utf8');
    entries.push({ name, method, crc, compressedSize, size, localOffset, isDirectory: name.endsWith('/') });
    at += 46 + nameLen + extraLen + commentLen;
  }
  return { buf, entries };
}

/** One entry's bytes, checked against the CRC the archive claims for it. */
function readEntry(buf, entry, archivePath) {
  if (buf.readUInt32LE(entry.localOffset) !== LOCAL_SIG) {
    throw new Error(`corrupt local header for ${entry.name} in ${archivePath}`);
  }
  const nameLen = buf.readUInt16LE(entry.localOffset + 26);
  const extraLen = buf.readUInt16LE(entry.localOffset + 28);
  const start = entry.localOffset + 30 + nameLen + extraLen;
  const raw = buf.subarray(start, start + entry.compressedSize);

  let data;
  if (entry.method === 0) data = raw;
  else if (entry.method === 8) data = inflateRawSync(raw);
  else throw new Error(`unsupported compression method ${entry.method} for ${entry.name} in ${archivePath}`);

  // The archive says what this should be. Checking is the point of a verifier.
  if (crc32(data) !== entry.crc) {
    throw new Error(`checksum mismatch for ${entry.name} in ${archivePath}`);
  }
  return data;
}

/**
 * Unpack every file entry into destDir, returning the names written.
 *
 * ⚠ **A name out of the archive is not a path to trust.** `../` in an entry name walks out of the
 * destination — the "zip slip" shape — and this tool exists to inspect archives it did not make.
 * Names are resolved and refused if they land outside destDir.
 */
export async function extractZip(archivePath, destDir) {
  const { buf, entries } = await listZipEntries(archivePath);
  const written = [];
  for (const entry of entries) {
    if (entry.isDirectory) continue;
    const target = path.resolve(destDir, entry.name);
    const within = path.resolve(destDir) + path.sep;
    if (!target.startsWith(within)) {
      throw new Error(`entry escapes the destination: ${entry.name} in ${archivePath}`);
    }
    await mkdir(path.dirname(target), { recursive: true });
    await writeFile(target, readEntry(buf, entry, archivePath));
    written.push(entry.name);
  }
  return written;
}
