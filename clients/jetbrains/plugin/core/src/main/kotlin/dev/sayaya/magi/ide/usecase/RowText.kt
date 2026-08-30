package dev.sayaya.magi.ide.usecase

/**
 * 행 하나를 그리기 전에 **글자로 정해지는 것들**.
 *
 * 창 클래스 안에 사는 사설 함수였다. 거기는 시험 소스셋이 없는 모듈이라(`intellij/`), 이
 * 규칙들은 오직 화면을 눈으로 봐야만 확인됐다 — 그런데 정작 하는 일은 문자열 계산이고
 * 플랫폼을 하나도 안 만진다. 재는 자리로 옮긴다. 창은 붓만 들면 된다.
 */
object RowText {

    /**
     * 이벤트 시각을 사람이 읽는 시계로. **못 읽으면 빈 글자다** — 지어내지 않는다.
     *
     * 나노초를 뗀다: 전사는 행이 수백 개고, 그 자리에서 소수점 아래 아홉 자리는 읽는 사람의
     * 눈만 먹는다. 표준시간대는 이 기계의 것이다(로그가 아니라 사람이 보는 줄이다).
     */
    fun clock(at: String?): String = at?.let {
        runCatching {
            java.time.Instant.parse(it).atZone(java.time.ZoneId.systemDefault())
                .toLocalTime().withNano(0).toString()
        }.getOrNull()
    }.orEmpty()

    /** 한 줄로 줄인다. 인자가 길면 전사가 그 인자만으로 화면을 다 먹는다. */
    fun oneLine(s: String, max: Int): String {
        val one = s.lineSequence().joinToString(" ")
        return if (one.length <= max) one else one.take(max) + "…"
    }

    /**
     * 접힘을 기억하는 열쇠. **글자까지 넣는다** — 같은 자리의 행이 내용만 바뀌면 그건 다른
     * 것이고, 펼침을 물려받으면 사람이 안 편 것이 펴진 채로 선다.
     */
    fun foldKey(r: Row): String = "${r.msgId}:${r.who}:${r.callId}:${r.text.hashCode()}"

    /**
     * 리치 렌더 패널을 붙들어 두는 열쇠. `msgId` 가 없으면 시각으로 — 둘 다 없으면 빈 글자라
     * 그 행은 캐시를 안 탄다(같은 열쇠에 다른 답이 묶이는 것보다 낫다).
     */
    fun richKey(r: Row): String = r.msgId.takeIf { it.isNotBlank() } ?: r.at.orEmpty()

    /**
     * 이 행이 편집이면 그 양쪽. **성공한 것만** — 실패한 편집은 디스크를 안 바꿨으니 보일
     * 변화가 없고, 그걸 그리면 안 일어난 일을 그리는 것이다.
     */
    fun diffSides(r: Row): Triple<String, String, String>? =
        if (r.ok != true) null else Rows.EditSides.of(r.tool, r.args)
}
