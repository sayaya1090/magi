import * as vscode from 'vscode';
import { found } from '../core/binary';
import { CoreRelease, fetchOffer, shipped } from '../core/release';
import { fetchCore, resolveLatest, wire } from '../core/fetch';
import { configDir } from '../core/workspace';

/**
 * Get a magi to run, fetching one **only if the person says so**.
 *
 * ⚠ **Asked, never assumed.** This puts an executable on somebody's machine; the JetBrains plugin
 * asks first and so does this. The question names the host and, when certificate checking is off,
 * says in as many words that the download will not be verified — see `fetchOffer`, where that text
 * lives so it can be measured without an editor.
 *
 * Returns the path to a usable binary, or null when there is none and none was fetched. A refusal is
 * not an error: the caller falls back to the "not installed" sentence it already had.
 */
export async function getCore(ask = true): Promise<string | null> {
  const already = found();
  if (already) return already;
  if (!ask) return null;

  const conf = shipped();
  const net = wire(new CoreRelease(conf).insecure);
  // Which version, before the question — so the number in the dialog is the one that will arrive.
  const release = await resolveLatest(new CoreRelease(conf), net);
  const offer = fetchOffer(release);
  if (!offer) return null; // nothing configured, or no build for this machine: the old sentence fits

  const yes = 'Download';
  const said = await vscode.window.showInformationMessage(
    offer.message, { modal: true, detail: offer.detail }, yes,
  );
  if (said !== yes) return null;

  return vscode.window.withProgress({
    location: vscode.ProgressLocation.Notification,
    title: `Downloading magi ${release.version}…`,
    cancellable: false,
  }, async () => {
    try {
      return await fetchCore(release, configDir(), net);
    } catch (e) {
      // The step that failed is in the message: a machine with no build, a checksum that does not
      // match and a host that cannot be reached are three different things to do next about.
      void vscode.window.showErrorMessage(`magi could not be downloaded — ${(e as Error).message}`);
      return null;
    }
  });
}
