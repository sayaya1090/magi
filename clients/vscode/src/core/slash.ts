/**
 * Is this line shaped like a slash command — `/name rest` split into its name and the rest.
 *
 * Only the shape. Whether the name really is a command is the daemon's list (`commands`) to say: a
 * `/word` it does not list is still something said to the model, as before (a sentence asking about
 * `/usr/bin` must not be eaten as a command). A line starting `//` is not a command.
 */
export function splitSlash(text: string): { name: string; rest: string } | null {
  const t = text.trim();
  if (!t.startsWith('/') || t.startsWith('//')) return null;
  const m = /^\/(\S+)\s*([\s\S]*)$/.exec(t);
  return m ? { name: m[1], rest: m[2].trim() } : null;
}
