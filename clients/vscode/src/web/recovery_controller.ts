import { AnswerStateManager } from '../core/answer_state';
import { RecoveryItem } from '../core/recovery_state';
import { captureSelection, restoreFocus, restoreSelection } from './dom_interaction';
import { createRecoveryView, RecoveryView } from './recovery_view';

export interface RecoveryInputTarget {
  clearAutoCompletion(): void;
  applyGeneralModeUI(text: string): void;
}

export interface RecoveryElements {
  recoveryBtn: HTMLElement;
  recoveryPanel: HTMLElement;
  recoveryItemsEl: HTMLElement;
  recoveryScopeAll?: HTMLInputElement | null;
  recoveryStatus?: HTMLElement | null;
  say: HTMLTextAreaElement;
}

export interface RecoveryControllerOptions {
  elements: RecoveryElements;
  answerState: AnswerStateManager;
  inputAdapter: RecoveryInputTarget;
  getCurrentCompanionKey: () => string;
  getCurrentSession: () => string;
  document?: any;
}

export interface RecoveryController {
  refresh(): void;
  open(): void;
  close(): void;
  toggle(): void;
  isOpen(): boolean;
  setScopeAll(all: boolean): void;
  getScopeAll(): boolean;
  toggleFullText(recoveryId: string): void;
  isFullTextOpen(recoveryId: string): boolean;
  copyDraft(recoveryId: string): boolean;
  confirmAppend(recoveryId: string): boolean;
  cancelConfirm(recoveryId: string): void;
  deleteItem(recoveryId: string): boolean;
  setComposing(composing: boolean): void;
  isComposing(): boolean;
  getPendingConfirmId(): string | null;
  onContextChange(companionKey: string, sessionId: string): void;
  dispose(): void;
}

export function createWebviewRecoveryController(options: RecoveryControllerOptions): RecoveryController {
  const { elements, answerState, inputAdapter, getCurrentCompanionKey, getCurrentSession } = options;
  const { recoveryBtn, recoveryPanel, recoveryItemsEl, recoveryScopeAll, recoveryStatus, say } = elements;
  const doc = options.document || (recoveryItemsEl && (recoveryItemsEl as any).ownerDocument) || (typeof document !== 'undefined' ? document : undefined);

  let panelOpen = false;
  let scopeAll = false;
  let isComposing = false;
  const openFullTexts = new Set<string>();
  let pendingConfirmId: string | null = null;
  let confirmSession: string = '';
  let confirmCompanion: string = '';

  function setStatus(msg: string): void {
    if (recoveryStatus) {
      recoveryStatus.textContent = msg;
    }
  }

  function getVisibleItems(): RecoveryItem[] {
    const compKey = getCurrentCompanionKey();
    const sessId = getCurrentSession();
    if (!scopeAll) {
      return answerState.listRecoveryItems({
        companionKey: compKey,
        sessionId: sessId,
      });
    } else {
      return answerState.listRecoveryItems({
        companionKey: compKey,
        includeOtherSessions: true,
      });
    }
  }

  const view: RecoveryView = createRecoveryView({
    containerEl: recoveryItemsEl,
    document: doc,
    callbacks: {
      onToggleFullText: (id: string) => toggleFullText(id),
      onCopyDraft: (id: string) => copyDraft(id),
      onDeleteItem: (id: string) => deleteItem(id),
      onConfirmAppend: (id: string) => confirmAppend(id),
      onCancelConfirm: (id: string) => cancelConfirm(id),
    },
  });

  function refresh(): void {
    const currentComp = getCurrentCompanionKey();
    const currentSess = getCurrentSession();

    // Cancel pending confirm if context changed (§4.6.3)
    if (pendingConfirmId !== null && (confirmCompanion !== currentComp || confirmSession !== currentSess)) {
      pendingConfirmId = null;
    }

    const items = getVisibleItems();
    recoveryBtn.textContent = '복구 초안 ' + items.length;
    recoveryBtn.setAttribute('aria-label', '복구 초안 ' + items.length + '개');
    if (recoveryScopeAll) {
      recoveryScopeAll.checked = scopeAll;
    }

    if (!panelOpen) {
      recoveryPanel.hidden = true;
      recoveryBtn.setAttribute('aria-expanded', 'false');
      return;
    }

    recoveryPanel.hidden = false;
    recoveryBtn.setAttribute('aria-expanded', 'true');

    if (!doc) return;

    // Pre-refresh capture of active element and selection within recoveryItemsEl (§4.6)
    let focusedRecoveryId: string | null = null;
    let focusedAction: 'full' | 'copy' | 'del' | 'append' | 'cancel' | null = null;
    const activeEl = doc ? (doc.activeElement as HTMLElement | null) : null;

    if (activeEl && (activeEl === recoveryItemsEl || (typeof recoveryItemsEl.contains === 'function' && recoveryItemsEl.contains(activeEl)))) {
      for (const [id, entry] of view.getAllEntries()) {
        if (entry.root === activeEl || (typeof entry.root.contains === 'function' && entry.root.contains(activeEl))) {
          focusedRecoveryId = id;
          if (activeEl === entry.fullBtn || (activeEl.classList && activeEl.classList.contains && activeEl.classList.contains('fulltext-btn'))) {
            focusedAction = 'full';
          } else if (activeEl === entry.copyBtn || (activeEl.classList && activeEl.classList.contains && activeEl.classList.contains('copy-btn'))) {
            focusedAction = 'copy';
          } else if (activeEl === entry.delBtn || (activeEl.classList && activeEl.classList.contains && activeEl.classList.contains('delete-btn'))) {
            focusedAction = 'del';
          } else if (activeEl === entry.appendBtn || (activeEl.classList && activeEl.classList.contains && activeEl.classList.contains('confirm-append-btn'))) {
            focusedAction = 'append';
          } else if (activeEl === entry.cancelBtn || (activeEl.classList && activeEl.classList.contains && activeEl.classList.contains('confirm-cancel-btn'))) {
            focusedAction = 'cancel';
          }
          break;
        }
      }
    }

    const capturedSelection = captureSelection(recoveryItemsEl, doc);

    // Render items via view module (§4.7)
    view.render(items, {
      openFullTexts,
      pendingConfirmId,
      isComposing,
    });

    // Restore focus if it was inside a recovery item before refresh (§4.6)
    if (focusedRecoveryId && focusedAction) {
      const focusedEntry = view.getEntry(focusedRecoveryId);
      if (focusedEntry) {
        let targetEl: HTMLElement | null = null;
        if (focusedAction === 'full') targetEl = focusedEntry.fullBtn;
        else if (focusedAction === 'copy') targetEl = focusedEntry.copyBtn;
        else if (focusedAction === 'del') targetEl = focusedEntry.delBtn;
        else if (focusedAction === 'append') targetEl = focusedEntry.appendBtn;
        else if (focusedAction === 'cancel') targetEl = focusedEntry.cancelBtn;

        restoreFocus(targetEl, doc);
      }
    }

    // Restore text selection if it was inside a recovery item before refresh (§4.6)
    restoreSelection(capturedSelection, recoveryItemsEl, doc);
  }

  function toggleFullText(recoveryId: string): void {
    if (openFullTexts.has(recoveryId)) {
      openFullTexts.delete(recoveryId);
    } else {
      openFullTexts.add(recoveryId);
    }
    refresh();
  }

  function copyDraft(recoveryId: string): boolean {
    if (isComposing) return false;
    const item = answerState.getRecoveryItem(recoveryId);
    if (!item) return false;

    const compKey = getCurrentCompanionKey();
    const sessId = getCurrentSession();
    const res = answerState.applyRecoveryDraft({
      recoveryId,
      companionKey: compKey,
      sessionId: sessId,
      currentInputText: say.value,
      append: false,
    });

    if (!res.ok) {
      if (res.reason === 'requires_confirm') {
        pendingConfirmId = recoveryId;
        confirmSession = sessId;
        confirmCompanion = compKey;
        setStatus('이어 붙이기 확인이 필요합니다.');
        refresh();
        return true;
      }
      return false;
    }

    inputAdapter.clearAutoCompletion();
    inputAdapter.applyGeneralModeUI(res.nextInputText || '');
    say.focus();
    setStatus('일반 초안으로 복사되었습니다.');
    refresh();
    return true;
  }

  function confirmAppend(recoveryId: string): boolean {
    if (isComposing) return false;
    const compKey = getCurrentCompanionKey();
    const sessId = getCurrentSession();

    if (pendingConfirmId !== recoveryId || sessId !== confirmSession || compKey !== confirmCompanion) {
      pendingConfirmId = null;
      setStatus('문맥이 변경되어 복사가 취소되었습니다.');
      refresh();
      return false;
    }

    const item = answerState.getRecoveryItem(recoveryId);
    if (!item) {
      pendingConfirmId = null;
      setStatus('항목이 삭제되어 복사가 취소되었습니다.');
      refresh();
      return false;
    }

    const res = answerState.applyRecoveryDraft({
      recoveryId,
      companionKey: compKey,
      sessionId: sessId,
      currentInputText: say.value,
      append: true,
    });

    pendingConfirmId = null;
    if (!res.ok) {
      setStatus('복사 적용에 실패했습니다.');
      refresh();
      return false;
    }

    inputAdapter.clearAutoCompletion();
    inputAdapter.applyGeneralModeUI(res.nextInputText || '');
    say.focus();
    setStatus('일반 초안에 이어 붙였습니다.');
    refresh();
    return true;
  }

  function cancelConfirm(recoveryId: string): void {
    if (pendingConfirmId === recoveryId) {
      pendingConfirmId = null;
      setStatus('복사가 취소되었습니다.');
      refresh();
    }
  }

  function deleteItem(recoveryId: string): boolean {
    if (pendingConfirmId === recoveryId) {
      pendingConfirmId = null;
    }
    openFullTexts.delete(recoveryId);

    // §4.6.3: Focus preservation on deletion
    const activeEl = doc ? (doc.activeElement as HTMLElement | null) : null;
    const entryToDelete = view.getEntry(recoveryId);
    let targetSiblingId: string | null = null;
    let focusRole: 'del' | 'copy' | 'full' | 'default' = 'default';
    let shouldShiftFocus = false;

    if (entryToDelete && activeEl && (activeEl === entryToDelete.root || entryToDelete.root.contains(activeEl))) {
      shouldShiftFocus = true;
      if (activeEl === entryToDelete.delBtn || activeEl.classList.contains('delete-btn')) {
        focusRole = 'del';
      } else if (activeEl === entryToDelete.copyBtn || activeEl.classList.contains('copy-btn')) {
        focusRole = 'copy';
      } else if (activeEl === entryToDelete.fullBtn || activeEl.classList.contains('fulltext-btn')) {
        focusRole = 'full';
      }

      const visible = getVisibleItems();
      const idx = visible.findIndex((it) => it.recoveryId === recoveryId);
      if (idx !== -1) {
        const sibling = (idx + 1 < visible.length)
          ? visible[idx + 1]
          : (idx - 1 >= 0 ? visible[idx - 1] : null);
        if (sibling) {
          targetSiblingId = sibling.recoveryId;
        }
      }
    }

    const deleted = answerState.deleteRecoveryItem(recoveryId);
    if (deleted) {
      setStatus('복구 초안이 삭제되었습니다.');
      refresh();

      if (shouldShiftFocus) {
        if (targetSiblingId) {
          const siblingEntry = view.getEntry(targetSiblingId);
          if (siblingEntry) {
            if (focusRole === 'del') siblingEntry.delBtn.focus();
            else if (focusRole === 'copy') siblingEntry.copyBtn.focus();
            else if (focusRole === 'full') siblingEntry.fullBtn.focus();
            else siblingEntry.copyBtn.focus();
          } else {
            recoveryBtn.focus();
          }
        } else {
          recoveryBtn.focus();
        }
      }
    }
    return deleted;
  }

  function onBtnClick(): void {
    panelOpen = !panelOpen;
    refresh();
  }

  function onScopeChange(): void {
    if (recoveryScopeAll) {
      scopeAll = recoveryScopeAll.checked;
      refresh();
    }
  }

  function onCompositionStart(): void {
    isComposing = true;
    view.updateButtonsDisabled(isComposing);
  }

  function onCompositionEnd(): void {
    isComposing = false;
    view.updateButtonsDisabled(isComposing);
  }

  recoveryBtn.addEventListener('click', onBtnClick);
  if (recoveryScopeAll) {
    recoveryScopeAll.addEventListener('change', onScopeChange);
  }
  say.addEventListener('compositionstart', onCompositionStart);
  say.addEventListener('compositionend', onCompositionEnd);

  // Initial render
  refresh();

  return {
    refresh,
    open(): void {
      panelOpen = true;
      refresh();
    },
    close(): void {
      panelOpen = false;
      refresh();
    },
    toggle(): void {
      panelOpen = !panelOpen;
      refresh();
    },
    isOpen: () => panelOpen,
    setScopeAll(val: boolean): void {
      scopeAll = val;
      refresh();
    },
    getScopeAll: () => scopeAll,
    toggleFullText,
    isFullTextOpen: (id: string) => openFullTexts.has(id),
    copyDraft,
    confirmAppend,
    cancelConfirm,
    deleteItem,
    setComposing(val: boolean): void {
      isComposing = val;
      view.updateButtonsDisabled(isComposing);
    },
    isComposing: () => isComposing,
    getPendingConfirmId: () => pendingConfirmId,
    onContextChange(_companionKey: string, _sessionId: string): void {
      if (pendingConfirmId !== null) {
        pendingConfirmId = null;
        setStatus('문맥이 변경되어 복사가 취소되었습니다.');
      }
      refresh();
    },
    dispose(): void {
      recoveryBtn.removeEventListener('click', onBtnClick);
      if (recoveryScopeAll) {
        recoveryScopeAll.removeEventListener('change', onScopeChange);
      }
      say.removeEventListener('compositionstart', onCompositionStart);
      say.removeEventListener('compositionend', onCompositionEnd);
      view.clear();
    },
  };
}
