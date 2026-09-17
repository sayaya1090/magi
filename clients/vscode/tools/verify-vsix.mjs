import { readFile, stat, mkdtemp, rm } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const execFileAsync = promisify(execFile);

export class MockNode {
  nodeType = 1;
  _textContent = '';
  childNodes = [];
  parentNode = null;
  dataset = {};
  className = '';
  tagName = '';
  attributes = {};

  get textContent() {
    if (this.nodeType === 3) return this._textContent;
    if (this.childNodes.length > 0) {
      return this.childNodes.map(c => c.textContent).join('');
    }
    return this._textContent;
  }
  set textContent(v) {
    this._textContent = String(v);
    this.childNodes = [];
  }

  appendChild(child) {
    child.parentNode = this;
    this.childNodes.push(child);
    return child;
  }
  setAttribute(k, v) {
    this.attributes[k] = String(v);
  }
  getAttribute(k) {
    return this.attributes[k];
  }
}

export class MockTextNode extends MockNode {
  nodeType = 3;
  constructor(text) {
    super();
    this._textContent = String(text);
  }
}

export class MockElement extends MockNode {
  constructor(tagName) {
    super();
    this.tagName = tagName.toUpperCase();
  }
  get href() { return this.attributes['href'] || ''; }
  set href(v) { this.attributes['href'] = v; }
  get target() { return this.attributes['target'] || ''; }
  set target(v) { this.attributes['target'] = v; }
  get rel() { return this.attributes['rel'] || ''; }
  set rel(v) { this.attributes['rel'] = v; }
}

export class MockDocument {
  createElement(tag) {
    return new MockElement(tag);
  }
  createTextNode(text) {
    return new MockTextNode(text);
  }
}

/**
 * Verifies that a VSIX archive matches the current repository build, includes all required
 * license notices byte-for-byte, excludes unwanted files (node_modules, out/test, src),
 * and executes webview bundles in an isolated sandbox with zero external dependencies.
 *
 * @param {string} vsixPath Absolute or relative path to the .vsix file.
 * @param {object} [options]
 * @param {string} [options.rootDir] Root directory of clients/vscode (defaults to repository clients/vscode).
 * @param {string} [options.expectedVersion] Expected version in extension/package.json.
 * @returns {Promise<{ version: string, fileCount: number, sizeBytes: number, vsixPath: string }>}
 */
export async function verifyVsixArchive(vsixPath, options = {}) {
  const rootDir = options.rootDir ? path.resolve(options.rootDir) : path.resolve(fileURLToPath(import.meta.url), '..', '..');
  const resolvedVsixPath = path.resolve(vsixPath);

  // 1. Verify that the input VSIX archive exists
  try {
    const st = await stat(resolvedVsixPath);
    if (!st.isFile()) {
      throw new Error(`Specified path is not a file: ${resolvedVsixPath}`);
    }
  } catch (err) {
    throw new Error(`Specified VSIX archive file does not exist: ${resolvedVsixPath} (${err.message})`);
  }

  const vsixStat = await stat(resolvedVsixPath);
  const sizeBytes = vsixStat.size;

  // Determine expected version if not explicitly supplied
  let expectedVersion = options.expectedVersion;
  if (!expectedVersion) {
    const rootPkgText = await readFile(path.join(rootDir, 'package.json'), 'utf8');
    const rootPkg = JSON.parse(rootPkgText);
    expectedVersion = rootPkg.version;
  }

  // 2. Unpack archive to an isolated temp directory using OS unzip
  const tempDir = await mkdtemp(path.join(tmpdir(), 'magi-vsix-verify-'));
  try {
    try {
      await execFileAsync('unzip', ['-q', resolvedVsixPath, '-d', tempDir]);
    } catch (err) {
      if (err.code === 'ENOENT') {
        throw new Error(`OS unzip command not found in PATH (${process.env.PATH}). VSIX archive verification requires the 'unzip' utility (standard on macOS and Linux/Ubuntu CI). Failed while attempting to unpack: ${resolvedVsixPath}`);
      }
      throw new Error(`Failed to unpack VSIX archive with unzip at ${resolvedVsixPath}: ${err.stderr || err.message}`);
    }

    // 3. Verify package.json in archive
    const unpackedPkgPath = path.join(tempDir, 'extension', 'package.json');
    assert.ok(existsSync(unpackedPkgPath), `VSIX missing extension/package.json: ${resolvedVsixPath}`);
    const unpackedPkg = JSON.parse(await readFile(unpackedPkgPath, 'utf8'));
    assert.equal(unpackedPkg.name, 'magi', 'VSIX extension/package.json name must be magi');
    assert.equal(
      unpackedPkg.version,
      expectedVersion,
      `VSIX version mismatch: expected ${expectedVersion}, found ${unpackedPkg.version} in ${resolvedVsixPath}`
    );

    // 4. Verify LICENSE.txt byte-for-byte match
    const unpackedLicensePath = path.join(tempDir, 'extension', 'LICENSE.txt');
    assert.ok(existsSync(unpackedLicensePath), `VSIX missing extension/LICENSE.txt: ${resolvedVsixPath}`);
    const expectedLicenseBytes = await readFile(path.join(rootDir, 'LICENSE'));
    const actualLicenseBytes = await readFile(unpackedLicensePath);
    assert.equal(
      Buffer.compare(actualLicenseBytes, expectedLicenseBytes),
      0,
      'extension/LICENSE.txt must match root LICENSE byte-for-byte'
    );

    // 5. Verify THIRD_PARTY_LICENSES.txt byte-for-byte match & mandatory notice text
    const unpackedThirdPartyPath = path.join(tempDir, 'extension', 'THIRD_PARTY_LICENSES.txt');
    assert.ok(existsSync(unpackedThirdPartyPath), `VSIX missing extension/THIRD_PARTY_LICENSES.txt: ${resolvedVsixPath}`);
    const expectedThirdPartyBytes = await readFile(path.join(rootDir, 'THIRD_PARTY_LICENSES.txt'));
    const actualThirdPartyBytes = await readFile(unpackedThirdPartyPath);
    assert.equal(
      Buffer.compare(actualThirdPartyBytes, expectedThirdPartyBytes),
      0,
      'extension/THIRD_PARTY_LICENSES.txt must match root THIRD_PARTY_LICENSES.txt byte-for-byte'
    );

    const thirdPartyText = actualThirdPartyBytes.toString('utf8');
    assert.match(
      thirdPartyText,
      /Copyright \(c\) 2014 Vitaly Puzrin, Alex Kocharin\./,
      'THIRD_PARTY_LICENSES.txt must contain markdown-it copyright notice verbatim'
    );
    assert.match(
      thirdPartyText,
      /Permission is hereby granted, free of charge/,
      'THIRD_PARTY_LICENSES.txt must contain MIT permission notice verbatim'
    );

    // 6. Verify core-release.properties if present in root
    const rootCoreProps = path.join(rootDir, 'core-release.properties');
    if (existsSync(rootCoreProps)) {
      const unpackedCoreProps = path.join(tempDir, 'extension', 'core-release.properties');
      assert.ok(existsSync(unpackedCoreProps), 'VSIX missing extension/core-release.properties');
      const expectedProps = await readFile(rootCoreProps);
      const actualProps = await readFile(unpackedCoreProps);
      assert.equal(
        Buffer.compare(actualProps, expectedProps),
        0,
        'extension/core-release.properties must match root core-release.properties byte-for-byte'
      );
    }

    // 7. Verify exclusion of forbidden directories
    const forbiddenPaths = [
      ['extension', 'node_modules'],
      ['extension', 'out', 'test'],
      ['extension', 'src'],
    ];
    for (const segs of forbiddenPaths) {
      const fullP = path.join(tempDir, ...segs);
      assert.equal(existsSync(fullP), false, `VSIX must not contain forbidden path: ${segs.join('/')}`);
    }

    // 8. Verify bundle presence and byte-for-byte match against repository build
    const unpackedChatAdapter = path.join(tempDir, 'extension', 'out', 'web', 'chat_adapter.bundle.js');
    const unpackedMarkdownRender = path.join(tempDir, 'extension', 'out', 'web', 'markdown_render.js');
    assert.ok(existsSync(unpackedChatAdapter), 'VSIX missing extension/out/web/chat_adapter.bundle.js');
    assert.ok(existsSync(unpackedMarkdownRender), 'VSIX missing extension/out/web/markdown_render.js');

    const expectedChatAdapterBytes = await readFile(path.join(rootDir, 'out', 'web', 'chat_adapter.bundle.js'));
    const actualChatAdapterBytes = await readFile(unpackedChatAdapter);
    assert.equal(
      Buffer.compare(actualChatAdapterBytes, expectedChatAdapterBytes),
      0,
      'extension/out/web/chat_adapter.bundle.js must match built out/web/chat_adapter.bundle.js byte-for-byte'
    );

    const expectedMarkdownRenderBytes = await readFile(path.join(rootDir, 'out', 'web', 'markdown_render.js'));
    const actualMarkdownRenderBytes = await readFile(unpackedMarkdownRender);
    assert.equal(
      Buffer.compare(actualMarkdownRenderBytes, expectedMarkdownRenderBytes),
      0,
      'extension/out/web/markdown_render.js must match built out/web/markdown_render.js byte-for-byte'
    );

    // Assert zero external require('markdown-it') calls
    const adapterJs = actualChatAdapterBytes.toString('utf8');
    assert.equal(adapterJs.includes("require('markdown-it')"), false, 'chat_adapter.bundle.js must not contain require("markdown-it")');
    assert.equal(adapterJs.includes('require("markdown-it")'), false);

    const renderJs = actualMarkdownRenderBytes.toString('utf8');
    assert.equal(renderJs.includes("require('markdown-it')"), false, 'markdown_render.js must not contain require("markdown-it")');
    assert.equal(renderJs.includes('require("markdown-it")'), false);

    // 9. Standalone execution in a clean sandbox without node_modules:
    // A) markdown_render.js execution and DOM generation
    const renderSandbox = {
      exports: {},
      module: { exports: {} },
      require: (mod) => { throw new Error(`External require not allowed in sandboxed VSIX bundle: ${mod}`); },
      console,
    };
    renderSandbox.module.exports = renderSandbox.exports;
    vm.createContext(renderSandbox);
    vm.runInContext(renderJs, renderSandbox);
    assert.equal(typeof renderSandbox.module.exports.renderMarkdown, 'function', 'renderMarkdown must be exported');

    // Call renderMarkdown with MockDocument and assert DOM tree construction
    const mockDoc = new MockDocument();
    const container = new MockElement('DIV');
    renderSandbox.module.exports.renderMarkdown(
      container,
      '# 검증 표제\n\n```ts\nconst answer = 42;\n```',
      { document: mockDoc }
    );

    assert.equal(container.childNodes.length, 2, 'renderMarkdown must create 2 block children (H1 and PRE)');
    assert.equal(container.childNodes[0].tagName, 'H1');
    assert.equal(container.childNodes[0].textContent, '검증 표제');
    assert.equal(container.childNodes[1].tagName, 'PRE');
    assert.equal(container.childNodes[1].childNodes[0].tagName, 'CODE');
    assert.equal(container.childNodes[1].childNodes[0].textContent, 'const answer = 42;');

    // B) chat_adapter.bundle.js execution in browser-like environment
    const browserSandbox = {
      window: {},
      document: mockDoc,
      navigator: { userAgent: 'HeadlessTest' },
      console,
    };
    browserSandbox.window = browserSandbox;
    browserSandbox.self = browserSandbox;
    vm.createContext(browserSandbox);
    vm.runInContext(adapterJs, browserSandbox);

    // 10. Count unpacked files
    const { stdout: zipListOut } = await execFileAsync('unzip', ['-l', resolvedVsixPath]);
    const lines = zipListOut.trim().split('\n');
    let fileCount = 0;
    const summaryMatch = /(\d+)\s+files?/.exec(lines[lines.length - 1]);
    if (summaryMatch) {
      fileCount = parseInt(summaryMatch[1], 10);
    }

    return {
      version: unpackedPkg.version,
      fileCount,
      sizeBytes,
      vsixPath: resolvedVsixPath,
    };
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
}

// CLI entrypoint
async function cli() {
  const args = process.argv.slice(2);
  let vsixArg = null;
  let expectedVersion = null;
  let rootDir = null;

  for (const arg of args) {
    if (arg.startsWith('--expected-version=')) {
      expectedVersion = arg.slice('--expected-version='.length);
    } else if (arg.startsWith('--root=')) {
      rootDir = arg.slice('--root='.length);
    } else if (!arg.startsWith('--') && !vsixArg) {
      vsixArg = arg;
    }
  }

  if (!vsixArg) {
    console.error('Error: Missing required VSIX archive path.');
    console.error('Usage: node tools/verify-vsix.mjs <path-to-vsix> [--expected-version=<ver>] [--root=<dir>]');
    process.exit(1);
  }

  try {
    const result = await verifyVsixArchive(vsixArg, { expectedVersion, rootDir });
    console.log(`✓ VSIX archive verification PASSED: ${result.vsixPath}`);
    console.log(`  Version: ${result.version}, Size: ${result.sizeBytes} bytes, Files: ${result.fileCount}`);
  } catch (err) {
    console.error(`✗ VSIX archive verification FAILED: ${err.message}`);
    process.exit(1);
  }
}

if (process.argv[1] && process.argv[1].endsWith('verify-vsix.mjs')) {
  cli();
}
