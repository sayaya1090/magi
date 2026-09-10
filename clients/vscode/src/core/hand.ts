/**
 * The tools this editor offers the companion — the hand.
 *
 * What makes this different from everything else in `core` is the DIRECTION. The rest reads magi or
 * speaks to magi; this is the place the agent tells the EDITOR to do something. The reason is that
 * an edit which goes through the editor is seen by the editor: undo, the dirty buffer, the language
 * server's re-check, the open tab refreshing. A file rewritten from outside gets none of that.
 *
 * ⚠ **Kept small on purpose.** magi already reads, searches and runs shell commands well; offering
 * those again here would make the agent choose between two doors on every call with nothing riding
 * on the choice. What belongs here is only what needs the editor to be true.
 *
 * The editor half lives behind `Ide`, so the protocol is testable with no editor — `node --test`
 * cannot load `vscode`, and that split is the whole reason this file is in `core`.
 */

/** What the editor can actually do. Implemented in `ide/`. */
import * as nodePath from 'path';

/**
 * Is this path inside the workspace?
 *
 * ⚠ **Nothing asked.** The editor hand resolved a path the COMPANION named — absolute as given,
 * relative against the workspace — and opened it. An absolute path reached any file on the machine,
 * and a relative one with `..` walked out of the workspace, for `show`, for `apply_edit`, and for
 * the diagnostics of `problems`.
 *
 * The workspace is a trust boundary in this tree, and the sibling client keeps it: its `find`
 * resolves and then refuses anything not under the project ("no such file in this project"). The
 * daemon confines the companion too — and a confinement somebody else enforces is not this window's.
 * The editor hand is a second door into the same machine, opened by the same agent.
 *
 * Compared on RESOLVED paths, so `..` cannot walk out, and by path segments rather than string
 * prefix, so `/a/bc` is not "inside" `/a/b`.
 *
 * ⚠ Symlinks are NOT resolved: a link inside the workspace pointing out of it still passes. Saying
 * so rather than implying otherwise — following links needs the filesystem, and this stays a pure
 * decision the tests can make.
 */
export function inside(workdir: string, target: string): boolean {
  const root = nodePath.resolve(workdir);
  const rel = nodePath.relative(root, nodePath.resolve(root, target));
  return rel === '' || (!rel.startsWith('..' + nodePath.sep) && rel !== '..' && !nodePath.isAbsolute(rel));
}

export interface Ide {
  /** Open a file and put the cursor on a line. Changes what a person is looking at. */
  show(path: string, line?: number): Promise<string>;
  /** Replace text THROUGH the editor, so undo and the language server see it. */
  replace(path: string, old: string, text: string, all: boolean): Promise<string>;
  /** What the editor's own language servers and linters currently say about a file. */
  problems(path?: string): Promise<string>;
}

export interface HandTool {
  name: string;
  description: string;
  schema: unknown;
  /**
   * Whether the call changes the file.
   *
   * The core reads this in two places: it sheds re-readable results first when the window closes,
   * and — for a tool that names a `path` — it decides whether a call COUNTED as an edit. Leave it
   * unsaid and the protocol's default (not read-only) applies, so opening a file to show somebody
   * lands in the record as "this turn changed that file".
   */
  readOnly: boolean;
}

/**
 * The server name, fixed.
 *
 * Every `mcp__` tool is treated as dangerous by construction, and the only thing that excuses one
 * is an allow rule a person wrote — and that rule keys on the NAME. A name that changed per run
 * would make those rules unwritable.
 */
export const HAND_NAME = 'vscode';

const str = { type: 'string' };

export function handTools(): HandTool[] {
  return [
    {
      name: 'show',
      readOnly: true,
      description:
        'Open a file in the editor and put the cursor on a line. Use this to point the person at ' +
        'something rather than describing where it is.',
      schema: { type: 'object', properties: { path: str, line: { type: 'integer' } }, required: ['path'] },
    },
    {
      name: 'apply_edit',
      readOnly: false,
      description:
        'Replace text in a file THROUGH the editor, so undo, the open buffer and the language ' +
        'server all see it. Prefer this over writing the file directly when the file is open. ' +
        'If old appears more than once the edit is REFUSED unless replaceAll is true, so pass it ' +
        'when you mean every occurrence.',
      schema: {
        type: 'object',
        properties: { path: str, old: str, new: str, replaceAll: { type: 'boolean' } },
        required: ['path', 'old', 'new'],
      },
    },
    {
      name: 'problems',
      readOnly: true,
      description:
        "What this editor's language servers and linters say right now, as errors and warnings " +
        'with line numbers. This is the real diagnostic, not the output of a build command. ' +
        'Omit path for every file the editor has diagnostics for.',
      schema: { type: 'object', properties: { path: str } },
    },
  ];
}

export interface HandAnswer {
  text: string;
  error?: boolean;
}

/**
 * Run one tool.
 *
 * An unknown name is REFUSED. Succeeding quietly would let the agent move on believing the thing it
 * asked for happened — the defect shape this tree keeps paying for.
 */
export async function callHand(ide: Ide, name: string, args: Record<string, unknown>): Promise<HandAnswer> {
  const need = (k: string): string => {
    const v = args[k];
    if (typeof v !== 'string' || !v) throw new Error(`${k} is required`);
    return v;
  };
  try {
    switch (name) {
      case 'show': {
        const line = typeof args.line === 'number' ? args.line
          : typeof args.line === 'string' ? Number.parseInt(args.line, 10) : undefined;
        return { text: await ide.show(need('path'), Number.isFinite(line) ? line : undefined) };
      }
      // ⚠ **`need` is what stops an empty `old`, and that matters more than it looks.** The schema
      // makes `old` required and an empty string satisfies required — but `need` rejects an empty
      // string as missing, so it never reaches the editor. Measured on the JVM for the sibling
      // client, which had no such check: an empty needle "matches" between every character, so a
      // replaceAll with it rewrites the file with the new text wedged between every character, and
      // the tool reports that as a success. `hand.test.ts` pins this refusal for that reason.
      //
      // Empty, not blank: `need` passes four spaces through, and replacing them with a tab is a
      // real edit.
      case 'apply_edit':
        return { text: await ide.replace(need('path'), need('old'), String(args.new ?? ''), args.replaceAll === true) };
      case 'problems':
        return { text: await ide.problems(typeof args.path === 'string' ? args.path : undefined) };
      default:
        return { text: `this editor has no tool called "${name}"`, error: true };
    }
  } catch (e) {
    return { text: e instanceof Error ? e.message : String(e), error: true };
  }
}
