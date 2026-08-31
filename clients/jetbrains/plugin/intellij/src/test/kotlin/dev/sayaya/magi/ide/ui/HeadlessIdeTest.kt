package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase

/**
 * **창 없이 진짜 IDE 를 세우고 우리 것을 눌러 본다.**
 *
 * 이 모듈에는 오랫동안 시험 소스셋이 아예 없었다. 그래서 여기 사는 규칙 — 액션이 언제 보이나,
 * 인텐션이 언제 서나, 번들이 어느 글자를 주나 — 은 전부 **사람이 IDE 를 띄워 눈으로 봐야만**
 * 확인됐고, 실제로 그 값을 여러 번 치렀다(안 뜨는 액션, 안 닫히는 탭, 판을 벌리는 라벨).
 *
 * 이 픽스처는 화면을 안 띄운다. `Project`·`Editor`·액션 시스템이 프로세스 안에 서고, 우리는
 * 사람이 하는 것과 같은 입구로 들어간다 — 그래서 결과가 **로그로 찍힌다.**
 *
 * 여기 있는 것은 **모델이 없어도 참인 것들**이다. 모델을 부르는 적합성 배터리는
 * `core` 의 `live/ModelConformanceTest` 가 맡는다(그쪽은 데몬이 있어야 돈다).
 */
class HeadlessIdeTest : BasePlatformTestCase() {

    /**
     * 액션 넷이 **등록되어 있고 글자를 갖는다.** `plugin.xml` 의 id 와 번들의 열쇠가 갈리면
     * 여기서 운다 — 전에는 그 갈림이 「메뉴에 안 뜬다」로만 보였다.
     */
    fun `test 우리 액션들이 등록되어 있고 이름이 있다`() {
        val am = com.intellij.openapi.actionSystem.ActionManager.getInstance()
        for (id in listOf("magi.lookOver", "magi.attach", "magi.lookNow", "magi.wroteThis", "magi.askConsole")) {
            val a = am.getAction(id)
            assertNotNull("액션 $id 가 등록되어 있지 않다", a)
        }
    }

    /**
     * **고른 것이 없으면 첨부 인텐션이 안 선다.** Alt+Enter 는 남들도 채우는 순위 목록이라,
     * 할 일이 없는 항목이 서면 그게 소음이다. 이 규칙은 화면으로만 확인되던 것이다.
     */
    fun `test 첨부 인텐션은 고른 것이 있을 때만 선다`() {
        myFixture.configureByText("A.kt", "fun main() {\n    println(1)\n}\n")
        val names = myFixture.availableIntentions.map { it.text }
        assertFalse(
            "고른 것이 없는데 첨부 인텐션이 섰다: $names",
            names.any { it == MagiBundle.msg("intention.attach.text") },
        )
    }

    /** 반대쪽: 고르면 선다. 둘을 같이 재야 「안 뜬다」가 규칙인지 고장인지 갈린다. */
    fun `test 고르면 첨부 인텐션이 선다`() {
        myFixture.configureByText("A.kt", "fun main() {\n    println(1)\n}\n")
        myFixture.editor.selectionModel.setSelection(0, 10)
        val names = myFixture.availableIntentions.map { it.text }
        assertTrue(
            "고른 것이 있는데 첨부 인텐션이 안 섰다: $names",
            names.any { it == MagiBundle.msg("intention.attach.text") },
        )
    }

    /**
     * 화면 글자가 **번들에서 온다.** 언어팩이 없는 이 픽스처에서는 영어여야 한다 — 사용자가
     * 「다른 설정은 다 영문인데 우리꺼만 한글」로 잡은 그 결함이 여기서 잡힌다.
     */
    fun `test 언어팩이 없으면 영어로 그린다`() {
        val t = MagiBundle.msg("action.magi.lookNow.text")
        assertEquals("magi: Review Now", t)
    }
}
