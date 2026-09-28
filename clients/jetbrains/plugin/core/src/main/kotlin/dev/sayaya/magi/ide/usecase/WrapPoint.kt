package dev.sayaya.magi.ide.usecase

/**
 * 자동 줄바꿈이 끊을 자리를 **낱말 경계**로 물린다.
 *
 * Swing 의 낱말 줄바꿈(`wrapStyleWord`)은 한글 음절 사이를 낱말 경계로 보고 낱말 한가운데를 끊는다 —
 * 설정 화면의 설명이 「저장되거 / 나」, 「데 / 몬 재시작」으로 섰다(2026-09-28 스크린샷). 한국어는 띄어쓰기
 * 단위로 끊는 것이 읽히므로, 끊을 자리가 낱말 안이면 그 줄의 마지막 공백 뒤로 물린다. 한 낱말이 한 줄보다
 * 길면 물릴 곳이 없으니 원래 자리에서 끊는다(넘치는 것보다 낫다).
 *
 * `intellij` 모듈엔 시험이 없어 여기 둔다.
 */
object WrapPoint {
    /** [line] 은 한 문단의 이 줄부터 끝까지, [at] 은 줄바꿈이 고른 끝(0 < at ≤ line.length). */
    fun keepWords(line: CharSequence, at: Int): Int {
        if (at <= 0 || at >= line.length) return at
        if (line[at - 1].isWhitespace() || line[at].isWhitespace()) return at
        var i = at - 1
        while (i > 0 && !line[i - 1].isWhitespace()) i--
        return if (i > 0) i else at
    }
}
