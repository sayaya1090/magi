package dev.sayaya.magi.ide.ui

/**
 * 조합·키 누름/해제·타이머 만료·포커스 상실·종료 상태를 관리하는 컴포저 입력 게이트.
 * Swing 컴포넌트, RPC 전송, 초안 관리 라이프사이클을 직접 소유하지 않으며,
 * 결정적인 이벤트 시퀀스를 입력받아 키보드 전송 허용 여부와 취소 허용 여부를 판정합니다.
 */
class ComposerInputGate(
    private val onScheduleTimer: (delayMs: Long) -> Unit = {},
    private val onCancelTimer: () -> Unit = {},
) {
    var isComposing: Boolean = false
        private set
    var enterPressed: Boolean = false
        private set
    var enterCommitted: Boolean = false
        private set
    var isDisposed: Boolean = false
        private set

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
        }
    }

    /**
     * 키 누름 이벤트 수신.
     * [isEnter]: Enter 키 여부 (KeyEvent.VK_ENTER)
     */
    fun onKeyPressed(isEnter: Boolean) {
        if (isDisposed) return
        if (isEnter) {
            enterPressed = true
        } else {
            enterPressed = false
            enterCommitted = false
            onCancelTimer()
        }
    }

    /**
     * 키 릴리즈 이벤트 수신.
     * [isEnter]: Enter 키 여부 (KeyEvent.VK_ENTER)
     */
    fun onKeyReleased(isEnter: Boolean) {
        if (isDisposed) return
        if (isEnter) {
            enterPressed = false
            enterCommitted = false
            onCancelTimer()
        }
    }

    /**
     * 안전 타이머 만료 콜백.
     * 물리 키가 여전히 눌려 있는 상태(enterPressed == true)에서는 시간 경과가 물리 키 해제를 대신하지 않습니다.
     * 키를 누르지 않은 상태(!enterPressed)에서만 잔류 확정 플래그를 안전하게 정리합니다.
     */
    fun onTimerExpired() {
        if (isDisposed) return
        if (!enterPressed) {
            enterCommitted = false
        }
    }

    /**
     * 포커스 상실 시 키 및 확정 상태 안전 정리.
     */
    fun onFocusLost() {
        if (isDisposed) return
        enterPressed = false
        enterCommitted = false
        onCancelTimer()
    }

    /**
     * 뷰 종료 시 모든 상태 초기화 및 타이머 정리.
     */
    fun dispose() {
        isDisposed = true
        enterPressed = false
        enterCommitted = false
        isComposing = false
        onCancelTimer()
    }

    /**
     * Enter 키 액션(ActionMap "magi.send") 발생 시 전송 허용 여부 판정:
     * - 조합 중(preedit)이면 차단
     * - Enter로 확정된 상태(enterCommitted)이면 차단
     *   (단, 물리 키가 이미 떼어진 비정상 상태였다면 안전을 위해 플래그를 소비하고 차단)
     */
    fun canSendOnEnter(): Boolean {
        if (isDisposed || isComposing) return false
        if (enterCommitted) {
            if (!enterPressed) {
                enterCommitted = false
                onCancelTimer()
            }
            return false
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
     * 수동/명시적 전송(마우스 버튼 클릭 등) 시 키 게이트 확정 플래그 정리.
     */
    fun onExplicitAction() {
        if (isDisposed) return
        enterCommitted = false
        onCancelTimer()
    }

    companion object {
        const val SAFETY_TIMEOUT_MS = 250L
    }
}
