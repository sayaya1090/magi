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
     *
     * 규칙:
     * - Enter 누름: enterPressed = true.
     * - 비-Enter 누름:
     *   - 만약 Enter가 이미 눌려 있는 상태(enterPressed == true)라면, Shift 등 보조 키나 다른 키가
     *     함께 눌렸더라도 물리 Enter 키는 여전히 눌려 있으므로 enterPressed와 enterCommitted를 해제하지 않습니다.
     *   - Enter가 눌려 있지 않은 상태(!enterPressed)에서 비-Enter 키가 눌린 경우에만
     *     (예: Space로 음절이 확정된 경우) enterCommitted를 즉시 해제하고 타이머를 취소합니다.
     */
    fun onKeyPressed(isEnter: Boolean) {
        if (isDisposed) return
        if (isEnter) {
            enterPressed = true
        } else {
            if (!enterPressed) {
                enterCommitted = false
                onCancelTimer()
            }
        }
    }

    /**
     * 키 릴리즈 이벤트 수신.
     * [isEnter]: Enter 키 여부 (KeyEvent.VK_ENTER)
     *
     * 규칙:
     * - Enter 릴리즈: 물리 Enter 키가 떼어졌으므로 enterPressed = false, enterCommitted = false, 타이머 취소.
     * - 비-Enter 릴리즈: Enter 키의 물리 누름 상태와 무관하므로 enterPressed나 enterCommitted를 건드리지 않습니다.
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
     * 수동/명시적 전송(마우스 버튼 클릭 등) 시:
     * 물리 Enter 키가 눌려 있지 않은 경우(!enterPressed)에만 잔류 확정 플래그를 정리합니다.
     * Enter가 눌려 있는 상태(enterPressed == true)에서 마우스 버튼이 클릭되더라도
     * 눌려 있는 Enter의 확정 보호는 불필요하게 해제되지 않습니다.
     */
    fun onExplicitAction() {
        if (isDisposed) return
        if (!enterPressed) {
            enterCommitted = false
            onCancelTimer()
        }
    }

    companion object {
        const val SAFETY_TIMEOUT_MS = 250L
    }
}
