import { RecoveryItem } from '../core/recovery_state';
import { moveDomChild } from './dom_interaction';

export interface RenderedItemEntry {
  root: HTMLElement;
  metaEl: HTMLElement;
  titleEl: HTMLElement;
  reasonEl: HTMLElement;
  previewEl: HTMLElement;
  actsEl: HTMLElement;
  fullBtn: HTMLButtonElement;
  copyBtn: HTMLButtonElement;
  delBtn: HTMLButtonElement;
  fullPre: HTMLPreElement | null;
  confirmBox: HTMLElement | null;
  appendBtn: HTMLButtonElement | null;
  cancelBtn: HTMLButtonElement | null;
}

export interface RecoveryViewCallbacks {
  onToggleFullText: (recoveryId: string) => void;
  onCopyDraft: (recoveryId: string) => void;
  onDeleteItem: (recoveryId: string) => void;
  onConfirmAppend: (recoveryId: string) => void;
  onCancelConfirm: (recoveryId: string) => void;
}

export interface RecoveryViewState {
  openFullTexts: ReadonlySet<string>;
  pendingConfirmId: string | null;
  isComposing: boolean;
}

export interface RecoveryViewOptions {
  containerEl: HTMLElement;
  callbacks: RecoveryViewCallbacks;
  document?: any;
}

export interface RecoveryView {
  render(items: RecoveryItem[], state: RecoveryViewState): void;
  getEntry(recoveryId: string): RenderedItemEntry | undefined;
  getAllEntries(): ReadonlyMap<string, RenderedItemEntry>;
  updateButtonsDisabled(isComposing: boolean): void;
  clear(): void;
}

export function createRecoveryView(options: RecoveryViewOptions): RecoveryView {
  const { containerEl, callbacks } = options;
  const doc = options.document || (containerEl && (containerEl as any).ownerDocument) || (typeof document !== 'undefined' ? document : undefined);

  const renderedItems = new Map<string, RenderedItemEntry>();
  let emptyEl: HTMLElement | null = null;

  function updateButtonsDisabled(isComposing: boolean): void {
    for (const entry of renderedItems.values()) {
      entry.copyBtn.disabled = isComposing;
      if (entry.appendBtn) {
        entry.appendBtn.disabled = isComposing;
      }
    }
  }

  function render(items: RecoveryItem[], state: RecoveryViewState): void {
    if (!doc) return;
    const { openFullTexts, pendingConfirmId, isComposing } = state;

    if (items.length === 0) {
      for (const entry of renderedItems.values()) {
        entry.root.remove();
      }
      renderedItems.clear();

      if (!emptyEl) {
        const el = doc.createElement('div');
        el.className = 'recovery-empty';
        el.textContent = '보관 중인 복구 초안이 없습니다.';
        emptyEl = el;
      }
      if (emptyEl && emptyEl.parentNode !== containerEl) {
        containerEl.append(emptyEl);
      }
      return;
    }

    if (emptyEl && emptyEl.parentNode) {
      emptyEl.remove();
    }

    // 1. Update or create entries for visible items
    for (const item of items) {
      let entry = renderedItems.get(item.recoveryId);
      if (!entry) {
        const itemEl = doc.createElement('div');
        itemEl.className = 'recovery-item';
        itemEl.dataset.recoveryId = item.recoveryId;

        const metaEl = doc.createElement('div');
        metaEl.className = 'recovery-meta';
        itemEl.append(metaEl);

        const titleEl = doc.createElement('div');
        titleEl.className = 'recovery-title';
        itemEl.append(titleEl);

        const reasonEl = doc.createElement('div');
        reasonEl.className = 'recovery-reason';
        itemEl.append(reasonEl);

        const previewEl = doc.createElement('div');
        previewEl.className = 'recovery-preview';
        itemEl.append(previewEl);

        const actsEl = doc.createElement('div');
        actsEl.className = 'recovery-actions';

        const fullBtn = doc.createElement('button');
        fullBtn.type = 'button';
        fullBtn.className = 'fulltext-btn';
        fullBtn.addEventListener('click', () => {
          callbacks.onToggleFullText(item.recoveryId);
        });
        actsEl.append(fullBtn);

        const copyBtn = doc.createElement('button');
        copyBtn.type = 'button';
        copyBtn.className = 'copy-btn';
        copyBtn.textContent = '일반 초안으로 복사';
        copyBtn.addEventListener('click', () => {
          callbacks.onCopyDraft(item.recoveryId);
        });
        actsEl.append(copyBtn);

        const delBtn = doc.createElement('button');
        delBtn.type = 'button';
        delBtn.className = 'delete-btn';
        delBtn.textContent = '삭제';
        delBtn.addEventListener('click', () => {
          callbacks.onDeleteItem(item.recoveryId);
        });
        actsEl.append(delBtn);

        itemEl.append(actsEl);

        entry = {
          root: itemEl,
          metaEl,
          titleEl,
          reasonEl,
          previewEl,
          actsEl,
          fullBtn,
          copyBtn,
          delBtn,
          fullPre: null,
          confirmBox: null,
          appendBtn: null,
          cancelBtn: null,
        };
        renderedItems.set(item.recoveryId, entry);
      }

      // Update text contents only if changed (to preserve selection and minimize churn)
      const originText = item.creationTaskId
        ? '생성 작업: ' + item.creationTaskId
        : '세션: ' + (item.sessionId || '미지정');
      const metaText = originText + ' · 발생: ' + item.attempts + '회';
      if (entry.metaEl.textContent !== metaText) {
        entry.metaEl.textContent = metaText;
      }

      if (entry.titleEl.textContent !== item.title) {
        entry.titleEl.textContent = item.title;
      }

      if (entry.reasonEl.textContent !== item.reason) {
        entry.reasonEl.textContent = item.reason;
      }

      const prevText = item.text.length > 80 ? item.text.slice(0, 80) + '…' : item.text;
      if (entry.previewEl.textContent !== prevText) {
        entry.previewEl.textContent = prevText;
      }

      entry.copyBtn.disabled = isComposing;

      // Full text display toggle
      const isFullOpen = openFullTexts.has(item.recoveryId);
      const fullBtnText = isFullOpen ? '전문 닫기' : '전문 보기';
      if (entry.fullBtn.textContent !== fullBtnText) {
        entry.fullBtn.textContent = fullBtnText;
      }
      entry.fullBtn.setAttribute('aria-expanded', isFullOpen ? 'true' : 'false');

      if (isFullOpen) {
        if (!entry.fullPre) {
          const fullPre = doc.createElement('pre');
          fullPre.className = 'recovery-full-text';
          fullPre.textContent = item.text;
          entry.root.insertBefore(fullPre, entry.actsEl);
          entry.fullPre = fullPre;
        } else if (entry.fullPre.textContent !== item.text) {
          entry.fullPre.textContent = item.text;
        }
      } else if (entry.fullPre) {
        entry.fullPre.remove();
        entry.fullPre = null;
      }

      // Confirm box toggle
      const isPendingConfirm = (pendingConfirmId === item.recoveryId);
      if (isPendingConfirm) {
        if (!entry.confirmBox) {
          const confirmBox = doc.createElement('div');
          confirmBox.className = 'recovery-confirm-box';

          const msgSpan = doc.createElement('div');
          msgSpan.className = 'recovery-confirm-msg';
          msgSpan.textContent = '작성 중인 일반 초안이 있습니다. 이어 붙이시겠습니까?';
          confirmBox.append(msgSpan);

          const appendBtn = doc.createElement('button');
          appendBtn.type = 'button';
          appendBtn.className = 'confirm-append-btn';
          appendBtn.textContent = '이어 붙이기';
          appendBtn.disabled = isComposing;
          appendBtn.addEventListener('click', () => {
            callbacks.onConfirmAppend(item.recoveryId);
          });
          confirmBox.append(appendBtn);

          const cancelBtn = doc.createElement('button');
          cancelBtn.type = 'button';
          cancelBtn.className = 'confirm-cancel-btn';
          cancelBtn.textContent = '취소';
          cancelBtn.addEventListener('click', () => {
            callbacks.onCancelConfirm(item.recoveryId);
          });
          confirmBox.append(cancelBtn);

          entry.root.append(confirmBox);
          entry.confirmBox = confirmBox;
          entry.appendBtn = appendBtn;
          entry.cancelBtn = cancelBtn;
        } else if (entry.appendBtn) {
          entry.appendBtn.disabled = isComposing;
        }
      } else if (entry.confirmBox) {
        entry.confirmBox.remove();
        entry.confirmBox = null;
        entry.appendBtn = null;
        entry.cancelBtn = null;
      }
    }

    // 2. Remove entries no longer in visible items
    const visibleIds = new Set(items.map((it) => it.recoveryId));
    for (const [id, entry] of renderedItems) {
      if (!visibleIds.has(id)) {
        entry.root.remove();
        renderedItems.delete(id);
      }
    }

    // 3. Ensure proper order in DOM (§4.6)
    // Use state-preserving DOM move (Element.moveBefore) if available, with safe fallback to insertBefore
    for (let i = 0; i < items.length; i++) {
      const entry = renderedItems.get(items[i].recoveryId);
      if (!entry) continue;
      const elChildren = (containerEl.children || (containerEl as any).childNodes || []) as unknown as HTMLElement[];
      if (elChildren[i] !== entry.root) {
        const refNode = elChildren[i] || null;
        moveDomChild(containerEl, entry.root, refNode);
      }
    }
  }

  function clear(): void {
    for (const entry of renderedItems.values()) {
      entry.root.remove();
    }
    renderedItems.clear();
    if (emptyEl && emptyEl.parentNode) {
      emptyEl.remove();
    }
  }

  return {
    render,
    getEntry: (id: string) => renderedItems.get(id),
    getAllEntries: () => renderedItems,
    updateButtonsDisabled,
    clear,
  };
}
