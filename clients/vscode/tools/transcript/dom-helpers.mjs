/**
 * Measures the active button focus ring boundaries against #ask-controls .acts container
 * without arbitrary fallbacks or faked styles (§5.6, §5.8.6).
 * Returns active status, focusVisible, computed outline properties, margins, and 4-direction clipping booleans.
 *
 * @param {import('playwright').Page} page
 * @param {string} buttonSelector
 * @returns {Promise<object>}
 */
export async function measureActiveButtonRing(page, buttonSelector) {
  return await page.evaluate((sel) => {
    const btn = sel === ':focus' ? document.activeElement : (typeof sel === 'string' ? document.querySelector(sel) : sel);
    if (!btn) {
      throw new Error(`Target button not found: ${sel}`);
    }
    const isActive = document.activeElement === btn;
    const isFocusVisible = btn.matches(':focus-visible');
    const style = window.getComputedStyle(btn);
    const outlineStyle = style.outlineStyle;
    const outlineWidth = parseFloat(style.outlineWidth);
    const outlineOffset = parseFloat(style.outlineOffset);
    const ringSpan = (isNaN(outlineWidth) ? 0 : outlineWidth) + (isNaN(outlineOffset) ? 0 : outlineOffset);

    const acts = document.querySelector('#ask-controls .acts');
    if (!acts) {
      throw new Error('#ask-controls .acts container not found');
    }
    const aRect = acts.getBoundingClientRect();
    const clientTop = aRect.top + acts.clientTop;
    const clientLeft = aRect.left + acts.clientLeft;
    const clientBottom = clientTop + acts.clientHeight;
    const clientRight = clientLeft + acts.clientWidth;

    const bRect = btn.getBoundingClientRect();
    const ringTop = bRect.top - ringSpan;
    const ringBottom = bRect.bottom + ringSpan;
    const ringLeft = bRect.left - ringSpan;
    const ringRight = bRect.right + ringSpan;

    return {
      text: btn.textContent?.trim() || '',
      isActive,
      isFocusVisible,
      outlineStyle,
      outlineWidth,
      outlineOffset,
      ringSpan,
      topMargin: bRect.top - clientTop,
      bottomMargin: clientBottom - bRect.bottom,
      leftMargin: bRect.left - clientLeft,
      rightMargin: clientRight - bRect.right,
      clippedTop: ringTop < clientTop - 0.5,
      clippedBottom: ringBottom > clientBottom + 0.5,
      clippedLeft: ringLeft < clientLeft - 0.5,
      clippedRight: ringRight > clientRight + 0.5,
      bRect: { top: bRect.top, bottom: bRect.bottom, left: bRect.left, right: bRect.right },
      clientRect: { top: clientTop, bottom: clientBottom, left: clientLeft, right: clientRight },
    };
  }, buttonSelector);
}
