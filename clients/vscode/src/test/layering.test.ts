import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

const CORE = path.join(__dirname, '..', '..', 'src', 'core');

function files(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? files(path.join(dir, e.name))
      : e.name.endsWith('.ts') ? [path.join(dir, e.name)] : []);
}

/**
 * `core` does not know it is inside an editor.
 *
 * That split is what makes the protocol, the transcript and the permission vocabulary testable at
 * all — `node --test` cannot load `vscode`. Without a rule, one import at a time leaks in for
 * convenience and the layer stops being testable long before anyone notices.
 */
test('core does not import vscode', () => {
  const found = files(CORE);
  // The walk asserts it found something. A rule that scans an empty list passes over anything, and
  // a moved directory is exactly how that happens.
  assert.ok(found.length >= 4, `only ${found.length} core files walked — the walk is broken`);
  for (const f of found) {
    const src = fs.readFileSync(f, 'utf8');
    assert.ok(!/from ['"]vscode['"]/.test(src) && !/require\(['"]vscode['"]\)/.test(src),
      `${path.relative(CORE, f)} imports vscode — core must run without an editor`);
  }
});

/** And the scanner itself is checked, so a broken pattern cannot read as a clean layer. */
test('the vscode-import scanner still matches what it claims', () => {
  const yes = ["import * as vscode from 'vscode';", 'import {window} from "vscode";',
               "const v = require('vscode');"];
  const no = ["import * as net from 'net';", "// vscode is not imported here",
              "import { Row } from './transcript';"];
  const hit = (s: string) => /from ['"]vscode['"]/.test(s) || /require\(['"]vscode['"]\)/.test(s);
  for (const s of yes) assert.ok(hit(s), `should have matched: ${s}`);
  for (const s of no) assert.ok(!hit(s), `should not have matched: ${s}`);
});
