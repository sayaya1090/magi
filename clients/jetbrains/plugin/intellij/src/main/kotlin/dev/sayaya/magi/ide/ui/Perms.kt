package dev.sayaya.magi.ide.ui

/**
 * 승인 모드의 와이어 프로토콜 토큰과 UI 표시 레이블 간 변환을 전담한다.
 *
 * 콤보박스 모델이 `ask`, `auto`, `allow`, `deny`를 직접 노출할 경우, 플랫폼 가이드라인의 문장형 대문자(Capitalization)
 * 및 다국어 번들 규약을 위반하게 된다. 반면 데몬으로 송신되는 네트워크 페이로드는 소문자 토큰을 유지해야 한다.
 * 따라서 데이터 모델에는 와이어 토큰을 유지하고, UI 렌더러에서만 로컬라이즈된 텍스트로 변환한다.
 *
 * 정의되지 않은 미지의 토큰이 전달될 경우 임의로 가공하지 않고 원본 토큰 문자열을 그대로 반환한다 (§0.5-7, 상태 왜곡 방지).
 */
internal object Perms {

    /** 콤보박스 모델에 바인딩되는 와이어 프로토콜 토큰 목록. 렌더링 시에는 [label]을 통해 지역화된다. */
    val TOKENS = listOf("ask", "auto", "allow", "deny")

    fun label(token: String?): String = when (token) {
        "ask" -> MagiBundle.msg("perm.ask")
        "auto" -> MagiBundle.msg("perm.auto")
        "allow" -> MagiBundle.msg("perm.allow")
        "deny" -> MagiBundle.msg("perm.deny")
        null -> MagiBundle.msg("set.notsaid")
        else -> token // 미정의 토큰은 원본 문자열을 그대로 출력
    }
}
