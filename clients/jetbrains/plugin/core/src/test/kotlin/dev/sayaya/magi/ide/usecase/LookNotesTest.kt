package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class LookNotesTest {
    @Test
    fun `줄에 걸리는 말과 안 걸리는 말이 갈린다`() {
        val s = LookNotes.split("12\t링크에 콜론이 없다\n3\tTODO 가 안 닫혔다")
        assertEquals(listOf(12 to "링크에 콜론이 없다", 3 to "TODO 가 안 닫혔다"), s.anchored)
        assertEquals("", s.loose, "다 걸렸으면 띠에 세울 것이 없다")
    }

    @Test
    fun `모양을 안 지킨 답은 줄 없는 말로 남는다 — 번호를 지어내지 않는다`() {
        val s = LookNotes.split("이 파일 전체가 한 함수로 뭉쳐 있다")
        assertEquals(emptyList<Pair<Int, String>>(), s.anchored)
        assertEquals("이 파일 전체가 한 함수로 뭉쳐 있다", s.loose)
    }

    @Test
    fun `구분자가 탭이 아니어도 읽는다 — 모델은 계약을 늘 지키지 않는다`() {
        // 라이브 실측 모양: 탭 없이 번호가 글자에 바로 붙어 왔다.
        assertEquals(listOf(5 to "broken link missing colon"),
            LookNotes.split("5broken link missing colon").anchored)
        assertEquals(listOf(12 to "링크에 콜론이 없다"), LookNotes.split("12: 링크에 콜론이 없다").anchored)
        assertEquals(listOf(7 to "TODO 가 남았다"), LookNotes.split("7 TODO 가 남았다").anchored)
    }

    @Test
    fun `반쪽 모양들 — 0행·빈 지적·탭 없는 숫자는 걸지 않는다`() {
        assertEquals(emptyList<Pair<Int, String>>(), LookNotes.split("0\t없는 줄").anchored)
        assertEquals(emptyList<Pair<Int, String>>(), LookNotes.split("7\t   ").anchored)
        // 번호만 있고 말이 없으면 걸 수 없다.
        assertEquals(emptyList<Pair<Int, String>>(), LookNotes.split("42").anchored)
    }
}
