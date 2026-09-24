package dev.sayaya.magi.ide.ui

import org.junit.Assert.*
import org.junit.Test

class ComposerInputGateTest {
    private var scheduledDelay: Long? = null
    private var timerCancelled: Boolean = false

    private fun createGate(): ComposerInputGate {
        scheduledDelay = null
        timerCancelled = false
        return ComposerInputGate(
            onScheduleTimer = { delay -> scheduledDelay = delay },
            onCancelTimer = { timerCancelled = true },
        )
    }

    @Test
    fun testCommitEnterBlocksSend() {
        val gate = createGate()

        // 1. 조합 중
        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        assertTrue(gate.isComposing)
        assertFalse(gate.canSendOnEnter())

        // 2. 음절 확정 이벤트
        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        assertFalse(gate.isComposing)
        assertTrue(gate.enterCommitted)
        assertEquals(ComposerInputGate.SAFETY_TIMEOUT_MS, scheduledDelay)

        // 3. 동일 틱 Enter 키 누름
        gate.onKeyPressed(isEnter = true)
        assertTrue(gate.enterPressed)

        // 4. magi.send 액션 발생 -> 확정 Enter이므로 차단되어야 함
        assertFalse(gate.canSendOnEnter())
    }

    @Test
    fun testAutoRepeatEnterDoesNotSendEvenAfterTimerExpired() {
        val gate = createGate()

        // 1. 조합 및 Enter 확정
        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        gate.onKeyPressed(isEnter = true)
        assertFalse(gate.canSendOnEnter())

        // 2. 키를 떼지 않은 상태(enterPressed == true)에서 타이머 만료 주입
        assertTrue(gate.enterPressed)
        gate.onTimerExpired()

        // 물리 키 해제 없이 시간만 경과한 경우 확정 보호가 풀리지 않아야 함
        assertTrue(gate.enterCommitted)

        // 3. 키 반복(auto-repeat) Enter 이벤트 발생
        gate.onKeyPressed(isEnter = true)
        assertFalse(gate.canSendOnEnter()) // 차단 유지 확인
    }

    @Test
    fun testSubsequentEnterSendsAfterKeyReleased() {
        val gate = createGate()

        // 1. 조합 및 Enter 확정
        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        gate.onKeyPressed(isEnter = true)
        assertFalse(gate.canSendOnEnter())

        // 2. 키를 뗌 (keyReleased)
        gate.onKeyReleased(isEnter = true)
        assertFalse(gate.enterPressed)
        assertFalse(gate.enterCommitted)
        assertTrue(timerCancelled)

        // 3. 별도의 후속 Enter 타건
        gate.onKeyPressed(isEnter = true)
        assertTrue(gate.enterPressed)
        assertTrue(gate.canSendOnEnter()) // 정상 1회 전송 허용
    }

    @Test
    fun testSafetyTimerClearsCommitProtectionWhenKeyNotPressed() {
        val gate = createGate()

        // 1. Enter 키 누름 없이 마우스 클릭 등으로 음절이 확정된 경우
        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        assertFalse(gate.enterPressed)
        assertTrue(gate.enterCommitted)

        // 2. 250ms 경과 후 타이머 만료
        gate.onTimerExpired()

        // 키가 눌려있지 않은 상태이므로 안전 타이머가 확정 플래그를 정상 정리
        assertFalse(gate.enterCommitted)

        // 이후 Enter 타건 시 차단되지 않고 전송 가능
        gate.onKeyPressed(isEnter = true)
        assertTrue(gate.canSendOnEnter())
    }

    @Test
    fun testNonEnterCommitAllowsSubsequentEnterImmediately() {
        val gate = createGate()

        // 1. 조합 및 확정
        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        assertTrue(gate.enterCommitted)

        // 2. Space 등 비-Enter 키가 눌림
        gate.onKeyPressed(isEnter = false)
        assertFalse(gate.enterPressed)
        assertFalse(gate.enterCommitted) // 비-Enter 키로 즉시 보호 해제
        assertTrue(timerCancelled)

        // 3. 이후 Enter 타건 시 즉시 전송 허용
        gate.onKeyPressed(isEnter = true)
        assertTrue(gate.canSendOnEnter())
    }

    @Test
    fun testFocusLostClearsCommitProtection() {
        val gate = createGate()

        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        gate.onKeyPressed(isEnter = true)
        assertTrue(gate.enterCommitted)
        assertTrue(gate.enterPressed)

        // 포커스 상실
        gate.onFocusLost()
        assertFalse(gate.enterPressed)
        assertFalse(gate.enterCommitted)
        assertTrue(timerCancelled)

        // 포커스 복귀 후 후속 Enter 타건
        gate.onKeyPressed(isEnter = true)
        assertTrue(gate.canSendOnEnter())
    }

    @Test
    fun testDisposeClearsProtectionAndIgnoresLateTimer() {
        val gate = createGate()

        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        gate.onKeyPressed(isEnter = true)

        gate.dispose()
        assertTrue(gate.isDisposed)
        assertFalse(gate.enterPressed)
        assertFalse(gate.enterCommitted)
        assertFalse(gate.canSendOnEnter())
        assertFalse(gate.canCancel())

        // 늦은 타이머 만료나 키 이벤트가 들어와도 부활하지 않음
        gate.onTimerExpired()
        gate.onKeyPressed(isEnter = true)
        assertFalse(gate.canSendOnEnter())
    }

    @Test
    fun testComposingBlocksSendAndCancel() {
        val gate = createGate()

        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        assertTrue(gate.isComposing)
        assertFalse(gate.canSendOnEnter())
        assertFalse(gate.canCancel()) // 조합 중 취소 차단

        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        assertFalse(gate.isComposing)
        assertTrue(gate.canCancel()) // 조합 종료 후 취소 가능
    }

    @Test
    fun testHeldCommitEnterRemainsBlockedAfterModifierKeyPressAndRelease() {
        val gate = createGate()

        // 1. 조합 및 Enter 확정
        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        gate.onKeyPressed(isEnter = true)
        assertFalse(gate.canSendOnEnter())

        // 2. Enter를 계속 누르고 있는 상태에서 Shift 등 보조 키 press / release
        gate.onKeyPressed(isEnter = false)
        assertTrue(gate.enterPressed) // 다른 키가 눌려도 물리 Enter 누름 상태는 유지됨
        assertTrue(gate.enterCommitted)

        gate.onKeyReleased(isEnter = false)
        assertTrue(gate.enterPressed)
        assertTrue(gate.enterCommitted)

        // 3. 타이머 만료가 중간에 발생해도 Enter가 눌려 있으므로 차단 유지
        gate.onTimerExpired()
        assertTrue(gate.enterCommitted)

        // 4. 반복 Enter 발생 -> 차단 유지
        gate.onKeyPressed(isEnter = true)
        assertFalse(gate.canSendOnEnter())

        // 5. 물리 Enter 키 해제
        gate.onKeyReleased(isEnter = true)
        assertFalse(gate.enterPressed)
        assertFalse(gate.enterCommitted)

        // 6. 별도의 후속 Enter는 전송 허용
        gate.onKeyPressed(isEnter = true)
        assertTrue(gate.canSendOnEnter())
    }

    @Test
    fun testExplicitActionWhileEnterHeldPreservesHeldEnterProtection() {
        val gate = createGate()

        // 1. 조합 및 Enter 확정
        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        gate.onKeyPressed(isEnter = true)
        assertTrue(gate.enterPressed)
        assertTrue(gate.enterCommitted)

        // 2. Enter를 누른 상태에서 마우스 버튼 클릭 등으로 명시적 액션 실행
        gate.onExplicitAction()

        // 물리 Enter가 여전히 눌려 있으므로 held Enter 보호가 불필요하게 해제되지 않아야 함
        assertTrue(gate.enterCommitted)
        assertTrue(gate.enterPressed)

        // 3. 반복 Enter -> 차단 유지
        gate.onKeyPressed(isEnter = true)
        assertFalse(gate.canSendOnEnter())

        // 4. Enter 해제 후 후속 Enter는 전송 허용
        gate.onKeyReleased(isEnter = true)
        gate.onKeyPressed(isEnter = true)
        assertTrue(gate.canSendOnEnter())
    }

    @Test
    fun testExplicitActionWhenEnterNotHeldClearsProtection() {
        val gate = createGate()

        // Enter 누름 없이 확정 (마우스 확정 등)
        gate.onCompositionChanged(isComposing = true, committedCount = 0)
        gate.onCompositionChanged(isComposing = false, committedCount = 1)
        assertFalse(gate.enterPressed)
        assertTrue(gate.enterCommitted)

        // 마우스 버튼 클릭 등으로 명시적 액션 실행
        gate.onExplicitAction()
        assertFalse(gate.enterCommitted)
        assertTrue(timerCancelled)
    }
}
