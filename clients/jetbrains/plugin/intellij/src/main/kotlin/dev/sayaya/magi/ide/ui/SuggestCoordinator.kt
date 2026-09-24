package dev.sayaya.magi.ide.ui

/**
 * 자동완성(suggestion) 및 파일 멘션(@mention) 요청의 세대·세션·토큰/본문 귀속과 종료 수명을 조정하는 객체.
 *
 * 타이머 구동(400ms), 백그라운드 RPC 통신, EDT 디스패치 및 팝업/힌트 표시는 View가 전담하며,
 * 이 클래스는 다음 가드를 전담합니다:
 * 1. 요청 시작 시점: 세대 캡처, 활성화 여부 확인, ESC 등으로 취소된(dismissed) 토큰 중복 요청 차단.
 * 2. 응답 수신 시점: 백그라운드 스레드 전달 유효성 판정 (종료 여부, 세대 일치, 세션 일치).
 * 3. EDT 반영 시점: 화면 렌더링 전 최종 일치 판정 (종료 여부, 세대 일치, 세션 일치, 토큰/프리픽스 본문 일치).
 * 4. 팝업 선택 콜백: 사용자 선택 항목 수락 판정 및 취소 토큰 초기화.
 * 5. 팝업 닫힘(취소): ESC 등으로 닫혔을 때 동일 세대/세션 내 재팝업 방지를 위한 dismissedToken 보존.
 */
class SuggestCoordinator(
    initialEpoch: Long = 0L,
) {
    /** 현재 입력 세대. 텍스트 변경 또는 컴포저 무효화 시마다 증가합니다. */
    @Volatile
    var epoch: Long = initialEpoch
        private set

    /** ESC 등으로 닫힌 토큰. 동일 토큰으로의 불필요한 재요청을 차단합니다. */
    @Volatile
    var dismissedToken: String? = null
        private set

    /** 코디네이터 종료 여부 */
    @Volatile
    var isDisposed: Boolean = false
        private set

    sealed interface Ticket {
        val epoch: Long
        val session: String?
    }

    data class MentionTicket(
        override val epoch: Long,
        override val session: String?,
        val token: String,
    ) : Ticket

    data class SuggestionTicket(
        override val epoch: Long,
        override val session: String?,
        val prefix: String,
    ) : Ticket

    /**
     * 파일 멘션 요청 시작.
     * 종료 상태이거나 이미 취소된 토큰과 동일한 경우 null을 반환합니다.
     */
    fun startMention(token: String, session: String?): MentionTicket? {
        if (isDisposed) return null
        if (token == dismissedToken) return null
        return MentionTicket(epoch = epoch, session = session, token = token)
    }

    /**
     * 자동완성 제안 요청 시작.
     * 종료 상태이거나 비활성화(enabled == false)된 경우 null을 반환합니다.
     */
    fun startSuggestion(prefix: String, session: String?, enabled: Boolean): SuggestionTicket? {
        if (isDisposed) return null
        if (!enabled) return null
        return SuggestionTicket(epoch = epoch, session = session, prefix = prefix)
    }

    /**
     * 백그라운드 스레드에서 파일 멘션 응답을 수신했을 때 유효 여부 판정.
     */
    fun canDeliverMention(ticket: MentionTicket, currentSession: String? = ticket.session): Boolean {
        if (isDisposed) return false
        if (ticket.epoch != epoch) return false
        if (ticket.session != currentSession) return false
        return true
    }

    /**
     * 백그라운드 스레드에서 자동완성 제안 응답을 수신했을 때 유효 여부 판정.
     */
    fun canDeliverSuggestion(ticket: SuggestionTicket, currentSession: String? = ticket.session): Boolean {
        if (isDisposed) return false
        if (ticket.epoch != epoch) return false
        if (ticket.session != currentSession) return false
        return true
    }

    /**
     * EDT에서 멘션 후보 팝업을 표시하기 전 최종 유효 여부 판정.
     * 세대, 세션뿐 아니라 현재 입력창의 @토큰이 요청 당시 토큰과 정확히 일치하는지 검사합니다.
     */
    fun canPresentMention(ticket: MentionTicket, currentSession: String?, currentToken: String?): Boolean {
        if (isDisposed) return false
        if (ticket.epoch != epoch) return false
        if (ticket.session != currentSession) return false
        if (ticket.token != currentToken) return false
        return true
    }

    /**
     * EDT에서 자동완성 제안 힌트를 화면에 반영하기 전 최종 유효 여부 판정.
     * 세대, 세션뿐 아니라 현재 입력창 텍스트가 요청 당시 프리픽스와 정확히 일치하는지 검사합니다.
     */
    fun canPresentSuggestion(ticket: SuggestionTicket, currentSession: String?, currentPrefix: String): Boolean {
        if (isDisposed) return false
        if (ticket.epoch != epoch) return false
        if (ticket.session != currentSession) return false
        if (ticket.prefix != currentPrefix) return false
        return true
    }

    /**
     * 팝업에서 사용자가 항목을 선택했을 때 콜백의 유효 여부 판정.
     */
    fun canAcceptMentionChoice(ticket: MentionTicket, currentSession: String?): Boolean {
        if (isDisposed) return false
        if (ticket.epoch != epoch) return false
        if (ticket.session != currentSession) return false
        return true
    }

    /**
     * 팝업 선택을 승인하고 dismissedToken을 초기화합니다.
     * 유효하지 않은 경우(오래된 세대, 세션 불일치, 종료 등) false를 반환하고 상태를 변경하지 않습니다.
     */
    fun acceptMentionChoice(ticket: MentionTicket, currentSession: String?): Boolean {
        if (!canAcceptMentionChoice(ticket, currentSession)) return false
        dismissedToken = null
        return true
    }

    /**
     * 팝업이 선택 없이 닫혔을 때(ESC 등) 동일 토큰 재팝업 방지를 위해 토큰을 기록합니다.
     * 세대나 세션이 이미 변경되었거나 종료된 경우 무시합니다.
     */
    fun dismissMention(ticket: MentionTicket, currentSession: String?) {
        if (!isDisposed && ticket.epoch == epoch && ticket.session == currentSession) {
            dismissedToken = ticket.token
        }
    }

    /**
     * 입력 변경 등으로 세대를 증가시킵니다.
     */
    fun bumpEpoch(): Long {
        return ++epoch
    }

    /**
     * 코디네이터를 종료하고 세대를 증가시켜 잔류 콜백을 무효화합니다.
     */
    fun dispose() {
        isDisposed = true
        epoch++
    }

    companion object {
        const val DEBOUNCE_DELAY_MS = 400

        /** 입력 꼬리의 @토큰 추출 (예: "@셰이" -> "셰이"). 공백이 끊거나 2자 미만이면 null. */
        fun extractAtToken(text: String): String? {
            val at = text.lastIndexOf('@')
            if (at < 0) return null
            if (at > 0 && !text[at - 1].isWhitespace()) return null
            val tail = text.substring(at + 1)
            if (tail.any { it.isWhitespace() } || tail.length < 2) return null
            return tail
        }

        /** glob 메타문자 이스케이프 (*, ?, [, ], \) */
        fun escapeGlob(token: String): String = buildString {
            token.forEach { c ->
                if (c in "*?[]\\") append('\\')
                append(c)
            }
        }
    }
}
