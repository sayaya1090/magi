import * as fs from 'fs';
import * as path from 'path';

/** src/web, from out/test/support where this compiles to. */
const WEB = path.join(__dirname, '..', '..', '..', 'src', 'web');

/**
 * The chat webview's source: its markup (chat_html.ts) followed by the code it runs (chat_view.ts).
 *
 * The code used to live inside chat_html.ts as a 573-line script in a template literal, and a good
 * number of guards read that one file to check what the page does — "the tool-row branch tests
 * r.args", "nothing paints the failure reason", and so on. The script is now its own module,
 * type-checked under strict. Those guards still ask the same question about the same page, so they
 * read both files: pointing them at the markup alone would leave them reading nothing (they are built
 * to fail loudly when that happens), and pointing them at the view alone would drop the half that is
 * markup and CSS.
 */
export function chatWebviewSource(): string {
  return (
    fs.readFileSync(path.join(WEB, 'chat_html.ts'), 'utf8') +
    '\n' +
    fs.readFileSync(path.join(WEB, 'chat_view.ts'), 'utf8')
  );
}
