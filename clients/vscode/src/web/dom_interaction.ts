/**
 * DOM interaction helpers for focus, selection capture/restoration, and state-preserving movement.
 * Completely agnostic to recovery state, sessions, or sending policies.
 */

export interface CapturedSelection {
  anchorNode: Node;
  anchorOffset: number;
  focusNode: Node;
  focusOffset: number;
  isBackwards: boolean;
  expectedText: string;
}

export function docHasNode(node: Node | null, containerEl: HTMLElement, doc?: any): boolean {
  if (!node) return false;
  if (typeof (node as any).isConnected === 'boolean' && !(node as any).isConnected) return false;
  if (doc && typeof doc.contains === 'function' && doc.contains(node)) return true;
  if (doc && doc.body && typeof doc.body.contains === 'function' && doc.body.contains(node)) return true;
  if (typeof containerEl.contains === 'function' && containerEl.contains(node)) return true;
  return false;
}

export function captureSelection(containerEl: HTMLElement, doc?: any): CapturedSelection | null {
  const win = doc ? ((doc as any).defaultView || (typeof window !== 'undefined' ? window : null)) : (typeof window !== 'undefined' ? window : null);
  const sel = win && typeof win.getSelection === 'function' ? win.getSelection() : null;
  if (!sel || sel.rangeCount === 0 || sel.isCollapsed || !sel.anchorNode || !sel.focusNode) {
    return null;
  }

  const anchorNode = sel.anchorNode;
  const focusNode = sel.focusNode;
  const anchorInContainer = typeof containerEl.contains === 'function' && containerEl.contains(anchorNode);
  const focusInContainer = typeof containerEl.contains === 'function' && containerEl.contains(focusNode);

  // Only capture if selection involves containerEl. Outside selections are untouched (§4.6)
  if (!anchorInContainer && !focusInContainer) {
    return null;
  }

  let isBackwards = false;
  try {
    if (anchorNode === focusNode) {
      isBackwards = sel.anchorOffset > sel.focusOffset;
    } else if (typeof anchorNode.compareDocumentPosition === 'function') {
      const pos = anchorNode.compareDocumentPosition(focusNode);
      if (pos & (typeof Node !== 'undefined' ? Node.DOCUMENT_POSITION_PRECEDING : 2)) {
        isBackwards = true;
      }
    }
  } catch {
    isBackwards = false;
  }

  return {
    anchorNode,
    anchorOffset: sel.anchorOffset,
    focusNode,
    focusOffset: sel.focusOffset,
    isBackwards,
    expectedText: sel.toString(),
  };
}

export function restoreSelection(captured: CapturedSelection | null, containerEl: HTMLElement, doc?: any): void {
  if (!captured || !doc) return;
  const win = doc ? ((doc as any).defaultView || (typeof window !== 'undefined' ? window : null)) : (typeof window !== 'undefined' ? window : null);
  const sel = win && typeof win.getSelection === 'function' ? win.getSelection() : null;
  if (!sel) return;

  // DOM 이동 전후 선택이 이미 같으면 removeAllRanges/addRange를 호출하지 않음 (§4.6)
  try {
    if (
      sel.rangeCount > 0 &&
      sel.anchorNode === captured.anchorNode &&
      sel.anchorOffset === captured.anchorOffset &&
      sel.focusNode === captured.focusNode &&
      sel.focusOffset === captured.focusOffset
    ) {
      return;
    }
  } catch {
    // Continue
  }

  const { anchorNode, anchorOffset, focusNode, focusOffset, isBackwards, expectedText } = captured;
  if (!docHasNode(anchorNode, containerEl, doc) || !docHasNode(focusNode, containerEl, doc)) {
    return;
  }

  const anchorLen = anchorNode.nodeType === 3
    ? (anchorNode.nodeValue ? anchorNode.nodeValue.length : 0)
    : (anchorNode.childNodes ? anchorNode.childNodes.length : 0);
  const focusLen = focusNode.nodeType === 3
    ? (focusNode.nodeValue ? focusNode.nodeValue.length : 0)
    : (focusNode.childNodes ? focusNode.childNodes.length : 0);

  // 옛 오프셋을 새 본문에 clamp하지 않음 (범위 초과 시 미적용) (§4.6)
  if (anchorOffset > anchorLen || focusOffset > focusLen) {
    return;
  }

  let textMatches = true;
  try {
    if (typeof doc.createRange === 'function') {
      const testRange = doc.createRange();
      if (isBackwards) {
        testRange.setStart(focusNode, focusOffset);
        testRange.setEnd(anchorNode, anchorOffset);
      } else {
        testRange.setStart(anchorNode, anchorOffset);
        testRange.setEnd(focusNode, focusOffset);
      }
      if (testRange.toString() !== expectedText) {
        textMatches = false;
      }
    }
  } catch {
    textMatches = false;
  }

  if (!textMatches) {
    return;
  }

  try {
    if (typeof sel.setBaseAndExtent === 'function') {
      sel.setBaseAndExtent(anchorNode, anchorOffset, focusNode, focusOffset);
    } else if (typeof doc.createRange === 'function') {
      const restoreRange = doc.createRange();
      if (isBackwards) {
        restoreRange.setStart(focusNode, focusOffset);
        restoreRange.setEnd(anchorNode, anchorOffset);
      } else {
        restoreRange.setStart(anchorNode, anchorOffset);
        restoreRange.setEnd(focusNode, focusOffset);
      }
      sel.removeAllRanges();
      sel.addRange(restoreRange);
    }
  } catch {
    // Ignore if selection could not be applied
  }
}

export function restoreFocus(targetEl: HTMLElement | null, doc?: any): void {
  if (!targetEl || typeof targetEl.focus !== 'function') return;
  if (doc && doc.activeElement === targetEl) return;
  try {
    targetEl.focus({ preventScroll: true });
  } catch {
    targetEl.focus();
  }
}

export function moveDomChild(parentEl: HTMLElement, childEl: HTMLElement, refNode: HTMLElement | null): void {
  const canMoveBefore = typeof (parentEl as any).moveBefore === 'function';
  if (canMoveBefore) {
    try {
      (parentEl as any).moveBefore(childEl, refNode);
      return;
    } catch {
      // Fallback to insertBefore
    }
  }
  if (typeof parentEl.insertBefore === 'function') {
    parentEl.insertBefore(childEl, refNode);
  } else {
    parentEl.append(childEl);
  }
}
