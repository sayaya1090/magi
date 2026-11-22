/**
 * The chat and the MAGI board share one place on the right (user decision, 2026-11-22). VS Code
 * cannot put two views' content in one view, so both views sit in the same container and this
 * context key hides one of them — `when: "!magi.board"` on the chat, `when: "magi.board"` on the
 * board. The gear button on both title bars flips it.
 */
export const BOARD_CONTEXT = 'magi.board';

/** What the gear does from `board`: the key's next value and the view to bring forward. */
export function nextBoard(board: boolean): { board: boolean; focus: string } {
  const next = !board;
  return { board: next, focus: next ? 'magi.plan.focus' : 'magi.chat.focus' };
}
