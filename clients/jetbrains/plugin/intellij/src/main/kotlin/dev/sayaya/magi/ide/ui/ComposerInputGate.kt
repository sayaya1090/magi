package dev.sayaya.magi.ide.ui

/**
 * 조합·키 누름/해제·타이머 만료·포커스 상실·종료 상태를 관리하는 컴포저 입력 게이트.
 * Swing 컴포넌트, RPC 전송, 초안 관리 라이프사이클을 직접 소유하지 않으며,
 * 결정적인 이벤트 시퀀스를 입력받아 키보드 전송 허용 여부와 취소 허용 여부를 판정합니다.
 *
 * 조합을 확정한 Enter 는 그 한 번으로 전송한다(2026-11-12, 「한글 칠 때 엔터 두번」 보고로 계약 변경).
 * 같은 Enter 를 떼기 전의 반복(auto-repeat)은 두 번째 전송이 되지 않는다.
 */
class ComposerInputGate(
    private val onScheduleTimer: (delayMs: Long) -> Unit = {},
    private val onCancelTimer: () -> Unit = {},
    /** 조합 중에 눌린 Enter 를 확정 이벤트 뒤에 한 번 보낼 때 부른다. */
    private val onDeferredSend: () -> Unit = {},
) {
    var isComposing: Boolean = false
        private set
    var enterPressed: Boolean = false
        private set
    var enterCommitted: Boolean = false
        private set
    var isDisposed: Boolean = false
        private set
    /** 지금 눌린 확정 Enter 로 이미 한 번 보냈다 — 떼기 전 반복은 두 번째 전송이 되지 않는다. */
    var commitSent: Boolean = false
        private set
    /** 조합 중에 Enter 가 왔다 — 확정 이벤트가 오면 그때 한 번 보낸다. */
    private var sendAfterCommit: Boolean = false

    /**
     * IME 조합 상태 변화 수신.
     * [isComposing]: 현재 글자가 조합 중(preedit)인지 여부
     * [committedCount]: 이번 이벤트로 확정된 문자 수
     */
    fun onCompositionChanged(isComposing: Boolean, committedCount: Int) {
        if (isDisposed) return
        val wasComposing = this.isComposing
        this.isComposing = isComposing
        if (wasComposing && !isComposing && committedCount > 0) {
            enterCommitted = true
            onCancelTimer()
            onScheduleTimer(SAFETY_TIMEOUT_MS)
            if (sendAfterCommit) {
                sendAfterCommit = false
                commitSent = true
                onDeferredSend()
            }
        } else if (wasComposing && !isComposing) {
            // 확정 없이 조합이 끝났다(취소) — 예약한 전송을 버린다
            sendAfterCommit = false
        }
    }

    /**
     * 키 누름 이벤트 수신.
     * [isEnter]: Enter 키 여부 (KeyEvent.VK_ENTER)
     *
     * 규칙:
     * - Enter 누름: enterPressed = true.
     * - 비-Enter 누름:
     *   - Enter가 이미 눌려 있는 상태라면 Shift 등 다른 키가 함께 눌려도 물리 Enter 는 여전히 눌려 있으므로
     *     확정 상태를 해제하지 않습니다.
     *   - Enter가 눌려 있지 않은 상태에서 비-Enter 키가 눌린 경우에만(예: Space로 음절 확정)
     *     확정 상태를 즉시 해제하고 타이머를 취소합니다.
     */
    fun onKeyPressed(isEnter: Boolean) {
        if (isDisposed) return
        if (isEnter) {
            enterPressed = true
        } else {
            if (!enterPressed) {
                enterCommitted = false
                commitSent = false
                sendAfterCommit = false
                onCancelTimer()
            }
        }
    }

    /**
     * 키 릴리즈 이벤트 수신.
     * - Enter 릴리즈: 물리 Enter 키가 떼어졌으므로 모든 확정·전송 표시를 지우고 타이머를 취소합니다.
     * - 비-Enter 릴리즈: Enter 키의 물리 누름 상태와 무관하므로 건드리지 않습니다.
     */
    fun onKeyReleased(isEnter: Boolean) {
        if (isDisposed) return
        if (isEnter) {
            enterPressed = false
            enterCommitted = false
            commitSent = false
            sendAfterCommit = false
            onCancelTimer()
        }
    }

    /**
     * 안전 타이머 만료 콜백.
     * 물리 키가 여전히 눌려 있으면 시간 경과가 키 해제를 대신하지 않습니다.
     * 키를 누르지 않은 상태에서만 잔류 확정 표시를 정리합니다.
     */
    fun onTimerExpired() {
        if (isDisposed) return
        if (!enterPressed) {
            enterCommitted = false
            commitSent = false
        }
    }

    /**
     * 포커스 상실 시 키 및 확정 상태 안전 정리.
     */
    fun onFocusLost() {
        if (isDisposed) return
        enterPressed = false
        enterCommitted = false
        commitSent = false
        sendAfterCommit = false
        onCancelTimer()
    }

    /**
     * 뷰 종료 시 모든 상태 초기화 및 타이머 정리.
     */
    fun dispose() {
        isDisposed = true
        enterPressed = false
        enterCommitted = false
        commitSent = false
        sendAfterCommit = false
        isComposing = false
        onCancelTimer()
    }

    /**
     * Enter 키 액션(ActionMap "magi.send") 발생 시 전송 허용 여부 판정.
     * - 조합 중(preedit)이면 지금은 보내지 않는다. Enter 가 눌려 있으면 확정 이벤트 뒤 한 번 보내도록 예약한다.
     * - 확정 Enter 는 한 번만 보낸다. 떼기 전 반복(auto-repeat)은 두 번째 전송이 되지 않는다(commitSent).
     */
    fun canSendOnEnter(): Boolean {
        if (isDisposed) return false
        if (isComposing) {
            if (enterPressed) sendAfterCommit = true
            return false
        }
        if (enterCommitted) {
            if (commitSent) return false
            if (enterPressed) {
                commitSent = true
            } else {
                enterCommitted = false
                onCancelTimer()
            }
        }
        return true
    }

    /**
     * 작업 취소(Escape 또는 명시적 취소) 허용 여부:
     * IME 음절 조합 중에는 컴포저 취소 동작을 차단하여 IME 자체 취소(preedit 취소)를 우선 보장합니다.
     */
    fun canCancel(): Boolean {
        if (isDisposed || isComposing) return false
        return true
    }

    /**
     * 수동/명시적 전송(마우스 버튼 클릭 등) 시:
     * 물리 Enter 가 눌려 있지 않으면 잔류 확정 표시를 정리합니다.
     * Enter 가 눌린 채 버튼으로 보냈다면, 그 Enter 의 반복이 두 번째 전송이 되지 않게 전송 표시를 남깁니다.
     */
    fun onExplicitAction() {
        if (isDisposed) return
        if (!enterPressed) {
            enterCommitted = false
            commitSent = false
            onCancelTimer()
        } else if (enterCommitted) {
            commitSent = true
        }
    }

    companion object {
        const val SAFETY_TIMEOUT_MS = 250L
    }
}
