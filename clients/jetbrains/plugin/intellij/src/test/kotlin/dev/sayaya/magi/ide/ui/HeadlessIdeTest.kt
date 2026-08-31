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

    // ── 사람이 눈으로 보던 것들 ──────────────────────────────────────────────
    //
    // 아래는 전부 **라이브 점검표에 있던 항목**이다. 사람이 IDE 를 띄워 확인하던 것을 여기로
    // 옮긴다 — 눈으로 본 것은 다음 판에서 다시 봐야 하지만, 여기 적힌 것은 매 push 마다 돈다.

    /**
     * 우클릭 항목 넷이 **하위 메뉴 하나**에 접혀 있다. 남의 메뉴 바닥에 우리 것을 넷 줄 까는
     * 것은 그 메뉴를 쓰는 사람의 비용이다.
     */
    fun `test 편집기 우클릭은 하위 메뉴 하나다`() {
        val am = com.intellij.openapi.actionSystem.ActionManager.getInstance()
        val g = am.getAction("magi.editorMenu")
        assertNotNull("magi.editorMenu 그룹이 없다", g)
        assertTrue("하위 메뉴가 아니다", (g as com.intellij.openapi.actionSystem.ActionGroup).isPopup)
        val ids = am.getActionIdList("magi.").filter { am.getAction(it) != null }
        for (id in listOf("magi.lookOver", "magi.attach", "magi.lookNow", "magi.wroteThis")) {
            assertTrue("$id 가 없다: $ids", id in ids)
        }
    }

    /**
     * 그 메뉴 안에서는 `magi: ` 접두를 벗긴다 — 메뉴 이름이 이미 그 말을 했다. 밖에서는 그대로다:
     * Find Action 과 Keymap 은 메뉴 밖이라 접두가 없으면 어느 플러그인 것인지 알 수 없다.
     */
    fun `test 하위 메뉴 안에서만 접두를 벗긴다`() {
        val key = "action.magi.lookOver.text"
        val full = MagiBundle.msg(key)
        assertTrue("번들 값에 접두가 없다: $full", full.startsWith("magi: "))
        assertEquals(full.removePrefix("magi: "), item(com.intellij.openapi.actionSystem.ActionPlaces.EDITOR_POPUP, key))
        assertEquals(full, item(com.intellij.openapi.actionSystem.ActionPlaces.MAIN_TOOLBAR, key))
        assertEquals(full, item("GoToAction", key)) // Find Action
    }

    private fun item(place: String, key: String): String {
        val e = com.intellij.testFramework.TestActionEvent.createTestEvent(
            com.intellij.openapi.actionSystem.impl.SimpleDataContext.getProjectContext(project),
        )
        // place 를 바꿔 같은 사건을 다시 만든다 — 자리별 글자를 정하는 것이 place 이므로.
        val ev = com.intellij.openapi.actionSystem.AnActionEvent.createEvent(
            e.dataContext, com.intellij.openapi.actionSystem.Presentation(), place,
            com.intellij.openapi.actionSystem.ActionUiKind.NONE, null,
        )
        return MagiEditorMenu.item(ev, key)
    }

    /**
     * **설정 설명문이 판을 가로로 안 벌린다.** 사람이 「가로로 쭉 늘어남」으로 잡은 자리다.
     * 접히는 것과 좁게 서는 것은 다른 일이라, 재는 것은 선호 폭이 아니라 **최소 폭**이다 —
     * 최소 폭이 글자 길이만큼이면 판은 그 아래로 못 좁혀진다.
     */
    fun `test 설명문 라벨은 판을 안 벌린다`() {
        val long = MagiBundle.msg("set.byfile.what")
        assertTrue("견본 문구가 짧아 이 시험이 아무것도 안 잰다: ${long.length}자", long.length > 120)
        val c = Look.note(long)
        val min = c.minimumSize.width
        val pref = c.preferredSize.width
        assertTrue("설명문의 최소 폭이 ${min}px 다 — 판이 그 아래로 못 좁혀진다", min < 400)
        assertTrue("선호 폭이 ${pref}px 다 — 처음 열릴 때 판을 그만큼 벌린다", pref < 700)
    }

    /** 도구창 아이콘이 실제로 로드된다. 없으면 스트라이프가 빈 자리로 선다. */
    fun `test 도구창 아이콘이 있다`() {
        val i = com.intellij.openapi.util.IconLoader.findIcon("/icons/magiToolWindow.svg", javaClass)
        assertNotNull("도구창 아이콘을 못 찾는다", i)
    }

    /** 자동 실행은 기본이 켜짐이고, 나머지 셋의 기본값은 웹과 같다. */
    fun `test 이 화면의 스위치 기본값`() {
        assertTrue("자동 실행이 꺼져 있다", LocalPrefs.autostart(project))
        assertTrue(LocalPrefs.complete(project))
        assertTrue(LocalPrefs.suggest(project))
        assertFalse("훑어보기는 기본 꺼짐이다", LocalPrefs.look(project))
    }

    /**
     * **저장된 값이 화면에 선다 — 데몬이 없어도.**
     *
     * 앞의 시험은 `LocalPrefs` 를 재고 화면을 안 쟀다. 그 사이에 결함이 있었다: 네 스위치를
     * 세우는 자리가 데몬 콜백 안이라, 데몬이 없으면 전부 `JCheckBox` 기본값인 **꺼짐**으로 섰다
     * (사용자 실측 2026-09-01). 값은 켜짐인데 화면은 꺼짐이었고, 그 상태로 OK 를 누르면 그
     * 거짓이 저장까지 된다. **가게 아니라 진열장을 재야 잡힌다.**
     *
     * 이 픽스처에는 데몬이 없다 — 그래서 이 시험은 정확히 그 상황이다.
     */
    fun `test 데몬이 없어도 저장된 스위치가 화면에 선다`() {
        val c = MagiConfigurable(project)
        c.createComponent()
        c.reset()
        val boxes = boxes(c)
        assertEquals("체크박스 넷을 못 찾았다: ${boxes.keys}", 4, boxes.size)
        // 기본값 그대로 — 셋은 켜짐, 훑어보기만 꺼짐.
        assertTrue("자동 실행이 화면에서 꺼져 있다", boxes.getValue(MagiBundle.msg("set.autostart.box")))
        assertTrue(boxes.getValue(MagiBundle.msg("set.complete.box")))
        assertTrue(boxes.getValue(MagiBundle.msg("set.suggest.box")))
        assertFalse(boxes.getValue(MagiBundle.msg("set.look.box")))
        // 그리고 아무것도 안 만졌으니 OK 가 할 일이 없어야 한다 — 참이면 열자마자 값이 뒤집힌다.
        assertFalse("연 것만으로 바뀐 것이 있다고 한다", c.isModified)
    }

    /** 화면에 실제로 선 체크박스들: 글자 → 켜짐 여부. */
    private fun boxes(c: com.intellij.openapi.options.Configurable): Map<String, Boolean> {
        val out = LinkedHashMap<String, Boolean>()
        fun walk(comp: java.awt.Component) {
            if (comp is javax.swing.JCheckBox) out[comp.text] = comp.isSelected
            if (comp is java.awt.Container) comp.components.forEach(::walk)
        }
        c.createComponent()?.let(::walk)
        return out
    }
}
