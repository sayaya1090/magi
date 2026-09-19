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
const ZIP64_LOC_SIG = 0x07064b50;

/** The end-of-central-directory record, which is the only fixed point a zip has. */
function findEndOfCentralDirectory(buf) {
  // It is last, but a trailing comment may follow it — so scan back over the largest comment the
  // format allows (0xffff) plus the record itself.
  const from = Math.max(0, buf.length - (0xffff + 22));
  for (let i = buf.length - 22; i >= from; i--) {
    if (buf.readUInt32LE(i) === EOCD_SIG) {
      const diskNumber = buf.readUInt16LE(i + 4);
      const diskStart = buf.readUInt16LE(i + 6);
      const diskEntries = buf.readUInt16LE(i + 8);
      const totalEntries = buf.readUInt16LE(i + 10);
      const cdSize = buf.readUInt32LE(i + 12);
      const cdOffset = buf.readUInt32LE(i + 16);

      if (diskNumber !== 0 || diskStart !== 0) {
        throw new Error('split zip archives are not supported');
      }
      if (diskEntries === 0xffff || totalEntries === 0xffff || cdSize === 0xffffffff || cdOffset === 0xffffffff) {
        throw new Error('zip64 archives are not supported');
      }
      if (i >= 20 && buf.readUInt32LE(i - 20) === ZIP64_LOC_SIG) {
        throw new Error('zip64 archives are not supported');
      }
      return i;
    }
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

  if (at < 0 || at > buf.length) {
    throw new Error(`corrupt central directory offset in ${archivePath}`);
  }

  const entries = [];
  for (let i = 0; i < count; i++) {
    if (at + 46 > buf.length) {
      throw new Error(`truncated central directory at entry ${i} of ${archivePath}`);
    }
    if (buf.readUInt32LE(at) !== CENTRAL_SIG) {
      throw new Error(`corrupt central directory at entry ${i} of ${archivePath}`);
    }
    const flag = buf.readUInt16LE(at + 8);
    const method = buf.readUInt16LE(at + 10);
    const crc = buf.readUInt32LE(at + 16);
    const compressedSize = buf.readUInt32LE(at + 20);
    const size = buf.readUInt32LE(at + 24);
    const nameLen = buf.readUInt16LE(at + 28);
    const extraLen = buf.readUInt16LE(at + 30);
    const commentLen = buf.readUInt16LE(at + 32);
    const diskStart = buf.readUInt16LE(at + 34);
    const localOffset = buf.readUInt32LE(at + 42);

    if (diskStart !== 0) {
      throw new Error(`split zip archives are not supported: ${archivePath}`);
    }
    if (compressedSize === 0xffffffff || size === 0xffffffff || localOffset === 0xffffffff) {
      throw new Error(`zip64 archives are not supported: ${archivePath}`);
    }
    if (at + 46 + nameLen + extraLen + commentLen > buf.length) {
      throw new Error(`truncated central directory record for entry ${i} of ${archivePath}`);
    }
    const name = buf.subarray(at + 46, at + 46 + nameLen).toString('utf8');

    // Reject encrypted entries declared in central directory
    if ((flag & 1) !== 0 || (flag & 0x40) !== 0) {
      throw new Error(`encrypted entries are not supported: ${name} in ${archivePath}`);
    }

    entries.push({ name, flag, method, crc, compressedSize, size, localOffset, isDirectory: name.endsWith('/') });
    at += 46 + nameLen + extraLen + commentLen;
  }
  return { buf, entries };
}

/** One entry's bytes, checked against the CRC the archive claims for it. */
function readEntry(buf, entry, archivePath) {
  if (entry.localOffset < 0 || entry.localOffset + 30 > buf.length) {
    throw new Error(`truncated local header for ${entry.name} in ${archivePath}`);
  }
  if (buf.readUInt32LE(entry.localOffset) !== LOCAL_SIG) {
    throw new Error(`corrupt local header for ${entry.name} in ${archivePath}`);
  }
  const localFlag = buf.readUInt16LE(entry.localOffset + 6);
  const localMethod = buf.readUInt16LE(entry.localOffset + 8);
  const localCompressedSize = buf.readUInt32LE(entry.localOffset + 18);
  const localSize = buf.readUInt32LE(entry.localOffset + 22);

  if (localCompressedSize === 0xffffffff || localSize === 0xffffffff) {
    throw new Error(`zip64 archives are not supported: ${entry.name} in ${archivePath}`);
  }

  // Reject encrypted entries in either central or local headers, preventing flag-mismatch bypass (§5.9 P2)
  if ((entry.flag & 1) !== 0 || (localFlag & 1) !== 0 || (entry.flag & 0x40) !== 0 || (localFlag & 0x40) !== 0) {
    throw new Error(`encrypted entries are not supported: ${entry.name} in ${archivePath}`);
  }
  if (localMethod !== entry.method) {
    throw new Error(`compression method mismatch for ${entry.name} in ${archivePath}`);
  }

  const nameLen = buf.readUInt16LE(entry.localOffset + 26);
  const extraLen = buf.readUInt16LE(entry.localOffset + 28);
  const start = entry.localOffset + 30 + nameLen + extraLen;
  if (start > buf.length) {
    throw new Error(`truncated local header for ${entry.name} in ${archivePath}`);
  }
  if (start + entry.compressedSize > buf.length) {
    throw new Error(`truncated entry payload for ${entry.name} in ${archivePath}`);
  }
  const raw = buf.subarray(start, start + entry.compressedSize);
  if (raw.length !== entry.compressedSize) {
    throw new Error(`truncated entry payload for ${entry.name} in ${archivePath}`);
  }

  let data;
  if (entry.method === 0) data = raw;
  else if (entry.method === 8) {
    try {
      data = inflateRawSync(raw);
    } catch (err) {
      throw new Error(`decompression failed for ${entry.name} in ${archivePath}: ${err.message}`);
    }
  } else {
    throw new Error(`unsupported compression method ${entry.method} for ${entry.name} in ${archivePath}`);
  }

  // Compare uncompressed length against authoritative size declared in central directory (§5.9 P2)
  if (data.length !== entry.size) {
    throw new Error(`size mismatch for ${entry.name} in ${archivePath}: expected ${entry.size}, got ${data.length}`);
  }

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
