package dev.sayaya.magi.ide.ui

import com.intellij.openapi.util.Disposer
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.ui.components.JBTextArea
import com.intellij.util.ui.UIUtil
import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.Waiting
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.Daemon
import dev.sayaya.magi.ide.usecase.End
import dev.sayaya.magi.ide.usecase.Transcript
import java.awt.Dimension
import java.awt.event.ActionEvent
import java.awt.geom.Rectangle2D
import javax.swing.JButton
import javax.swing.JLabel
import javax.swing.JPanel
import javax.swing.JTextPane
import javax.swing.SwingConstants
import javax.swing.text.StyleConstants

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
        for (id in listOf(
            "magi.lookOver", "magi.attach", "magi.lookNow", "magi.wroteThis", "magi.askConsole",
            // 데몬을 띄우는 액션. 오래 없었고, 없다는 것이 「자동 기동이 막히면 사람이 할 일이
            // 없다」였다 — 액션 열둘 중 하나도 그 일을 안 했다.
            "magi.startDaemon",
        )) {
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
        // 견본은 「이 트리에서 가장 긴 설명문」이면 된다. 예전 견본(`set.byfile.what`)은
        // 설정 화면이 문을 안 쓰던 시절의 문장이라 사라졌다 — 그 문장이 「데몬에 문이 없다」고
        // 단언하고 있었고, 문이 생긴 뒤 거짓이 됐다.
        val long = MagiBundle.msg("plan.schedule.hint")
        assertTrue("견본 문구가 짧아 이 시험이 아무것도 안 잰다: ${long.length}자", long.length > 120)
        val c = Look.note(long)
        val min = c.minimumSize.width
        val pref = c.preferredSize.width
        assertTrue("설명문의 최소 폭이 ${min}px 다 — 판이 그 아래로 못 좁혀진다", min < 400)
        assertTrue("선호 폭이 ${pref}px 다 — 처음 열릴 때 판을 그만큼 벌린다", pref < 700)
    }

    /**
     * **드롭다운도 판을 안 벌린다 — 긴 항목이 온 뒤에도.**
     *
     * 사람이 잡았다(2026-09-01): 「한번 커진 드롭다운은 다시 작아지지 않는다. 드롭다운 빼고는
     * 크기가 줄어드는 거 확인」. 라벨 쪽은 이미 최소 폭을 쟀는데(`설명문 라벨은 판을 안 벌린다`)
     * 콤보는 안 쟀다. 그 사이에 결함이 살아 있었다 — 프로토타입은 **그리는** 폭만 정하고, 판이
     * 못 좁혀지게 막는 **최소 폭**은 안 잡는다. 실측:
     *
     * - 권한(편집 불가·프로토타입 없음): 95 → **451**
     * - 모델(편집 가능·프로토타입 있음): 300 → **428** — 프로토타입이 있어도 커졌다. 편집 가능한
     *   콤보의 최소 폭은 항목이 아니라 편집칸에서 나오기 때문이다.
     *
     * 그래서 재는 것은 선호 폭이 아니라 **긴 항목이 도착한 뒤의 최소 폭**이다. 견본이 상한보다
     * 훨씬 길어야 이 시험이 무언가를 재므로 그것도 같이 확인한다.
     */
    fun `test 드롭다운은 긴 항목이 와도 판을 안 벌린다`() {
        val c = MagiConfigurable(project)
        val panel = c.createComponent()!!
        val combos = ArrayList<javax.swing.JComboBox<*>>()
        fun walk(x: java.awt.Component) {
            if (x is javax.swing.JComboBox<*>) combos += x
            if (x is java.awt.Container) x.components.forEach(::walk)
        }
        walk(panel)
        assertEquals("이 화면의 콤보는 둘이다 — 늘거나 줄면 이 시험을 고쳐야 한다", 2, combos.size)
        val long = "qwen3-coder-next:latest-a-very-long-model-identifier-here"
        assertTrue("견본이 짧아 아무것도 안 잰다: ${long.length}자", long.length > 40)
        for ((i, cb) in combos.withIndex()) {
            @Suppress("UNCHECKED_CAST")
            (cb.model as javax.swing.DefaultComboBoxModel<Any>).addElement(long)
            val w = cb.minimumSize.width
            assertTrue("콤보 #$i 의 최소 폭이 ${w}px 다 — 긴 항목이 판의 바닥을 올렸다", w < 340)
        }
    }

    /**
     * **데몬이 준 긴 글이 판의 바닥을 올리지 않는다.**
     *
     * 사람이 잡았다(2026-09-01): 「설정창 크기 안 줄어드는 건 여전해」. 먼저 콤보를 고쳤는데
     * **그건 둘째 바닥이었다.** 진짜는 값을 적는 라벨이다 — 스윙 라벨의 최소 폭은 글자를 한 줄로
     * 편 길이라, 데몬이 준 404 한 줄이 앉는 순간 판 전체가 못 좁혀진다. 실측:
     *
     * - 쉴 때 **616px**
     * - 에러가 앉은 뒤 **2295px** (그 라벨 하나가 1256px)
     *
     * 그래서 이 시험은 **정적인 화면을 안 잰다.** 데몬이 글을 앉히는 칸에만 긴 글을 먹인다 —
     * 그 칸들은 [Look.DYN] 으로 표가 붙어 있다. 처음엔 「빈 라벨 전부」에 먹였는데 정적인 이름
     * 칸까지 물들어, 고친 뒤에도 숫자가 안 내려가는 것처럼 보였다(1003px 만큼). **계측이 제
     * 부작용을 같이 재고 있으면 고쳤는지 아닌지를 못 가른다.**
     */
    fun `test 데몬이 준 긴 글이 판을 안 벌린다`() {
        val c = MagiConfigurable(project)
        val panel = c.createComponent()!!
        val all = ArrayList<java.awt.Component>()
        fun walk(x: java.awt.Component) {
            all += x
            if (x is java.awt.Container) x.components.forEach(::walk)
        }
        walk(panel)
        val rest = panel.minimumSize.width
        val dyn = all.filterIsInstance<javax.swing.JComponent>()
            .filter { it.getClientProperty(Look.DYN) == true }
        assertTrue("표가 붙은 칸이 없다 — 이 시험이 아무것도 안 잰다", dyn.size >= 4)
        val err = "llm: not found \u2014 check -model and -base-url (model or endpoint missing) " +
            "(status 404): {\"error\":{\"message\":\"model 'opus' not found\"}}"
        for (x in dyn) when (x) {
            is javax.swing.text.JTextComponent -> x.text = err
            is javax.swing.JLabel -> x.text = err
        }
        for (x in dyn) assertTrue(
            "칸 하나가 ${x.minimumSize.width}px 를 요구한다 — 판이 그 아래로 못 좁혀진다",
            x.minimumSize.width < 520,
        )
        val after = panel.minimumSize.width
        assertTrue("긴 글이 판의 바닥을 $rest → $after 로 올렸다", after < rest + 120)
    }

    /**
     * **모델에게 가는 지시도 IDE 언어를 따른다.**
     *
     * 이 지시는 오래 한국어 한 줄이었다. 화면 글자는 언어팩을 따르는데 **모델에게 가는 지시만**
     * 안 따라서, 영문 IDE 에서도 답이 한국어로 왔다(사람 실측 2026-09-01). 번들 시험은 이걸
     * 못 잡는다 — 그 문장은 번들에 없다(일부러 그렇다: 번역을 다듬는 손이 모델 지시를 바꾸면
     * 안 된다). 그래서 여기서 따로 잰다.
     */
    fun `test 콘솔 질문은 답할 언어를 IDE 에서 가져온다`() {
        val en = AskConsoleAction.ask(java.util.Locale.ENGLISH)
        assertFalse("영문 IDE 의 지시에 한글이 섞였다: $en", en.any { it in '\uac00'..'\ud7a3' })
        assertFalse("영어인데 답할 언어를 덧붙였다 — 매번 한 줄이 더 실린다", en.contains("Answer in"))
        val ko = AskConsoleAction.ask(java.util.Locale.KOREAN)
        assertTrue("한국어 IDE 인데 답할 언어를 안 말한다: $ko", ko.contains("Answer in Korean"))
        // 지시 자체는 어느 쪽이든 영어다 — 모델이 가장 잘 알아듣고, 사람이 볼 문장이 아니다.
        assertTrue("지시 본문이 갈렸다: $ko", ko.startsWith(AskConsoleAction.ASK))
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
        // 그리고 아무것도 안 만졌으니 할 일이 없어야 한다 — 참이면 열자마자 값이 뒤집힌다.
        // 화면에서 이 값을 말하는 단추는 **Apply** 다. `OK` 는 IntelliJ 의 설정 대화상자에서 늘
        // 활성이라(누르면 닫는 단추다) 아무것도 안 가린다 — 사람이 눈으로 볼 때 OK 를 보면
        // 결함이 있어도 통과로 읽는다(2026-09-01, 내가 점검표에 OK 라고 적어 실제로 겪었다).
        assertFalse("연 것만으로 바뀐 것이 있다고 한다", c.isModified)
    }

    /**
     * **스위치를 바꾸면 「바뀐 것」으로 친다 — 넷 다.**
     *
     * 사람이 잡았다(2026-09-01): 「Start magi automatically 는 값을 바꿔도 Apply 가 활성화되지
     * 않는다」. 앞 시험은 **안 만졌을 때 false** 만 쟀다. 그 한쪽만 재면 `isModified` 가 **늘
     * false** 인 것과 구분이 안 간다 — 그리고 늘 false 면 플랫폼은 `apply` 를 아예 안 부르므로
     * 사람이 바꾼 값이 조용히 버려진다. 앞 결함(화면이 거짓을 보임)의 정확히 반대쪽이다.
     *
     * 넷을 하나씩 따로 뒤집는다. 묶어서 한 번에 뒤집으면 **하나만 배선돼 있어도 통과**한다.
     */
    fun `test 스위치를 바꾸면 바뀐 것으로 친다`() {
        val c = MagiConfigurable(project)
        c.createComponent()
        for (key in listOf("set.look.box", "set.complete.box", "set.suggest.box", "set.autostart.box")) {
            c.reset()
            assertFalse("연 것만으로 바뀜이 됐다 — 이 시험이 아무것도 못 잰다", c.isModified)
            val box = box(c, MagiBundle.msg(key))
            box.isSelected = !box.isSelected
            assertTrue("$key 를 뒤집었는데 바뀐 것이 없다고 한다 — Apply 가 안 살고 값이 버려진다", c.isModified)
        }
    }

    /** 화면에 실제로 선 그 체크박스를 글자로 집는다 — 필드가 아니라 **판에 붙은 것**을. */
    private fun box(c: com.intellij.openapi.options.Configurable, text: String): javax.swing.JCheckBox {
        val found = ArrayList<javax.swing.JCheckBox>()
        fun walk(comp: java.awt.Component) {
            if (comp is javax.swing.JCheckBox && comp.text == text) found += comp
            if (comp is java.awt.Container) comp.components.forEach(::walk)
        }
        c.createComponent()?.let(::walk)
        assertEquals("체크박스 「$text」 를 판에서 못 찾았거나 여럿이다", 1, found.size)
        return found[0]
    }

    /**
     * **시험은 데몬을 안 띄운다.**
     *
     * 이 픽스처는 프로젝트를 만들고 열므로 시동 활동이 그대로 돈다. 막기 전까지 `:intellij:test`
     * 한 번에 임시 워크스페이스마다 **진짜 데몬이 하나씩** 떴다(설정 디렉토리에 `daemon-unitTest_…`
     * 로그가 쌓인 것으로 드러났다). 스위치는 켜져 있는데도 안 띄우는 것이 맞는 모양이라, 둘을
     * **같이** 잰다 — 스위치만 보면 「꺼져 있어서 안 떴다」와 구분이 안 간다.
     */
    fun `test 시험 안에서는 데몬을 안 띄운다`() {
        assertTrue("스위치가 꺼져 있어 이 시험이 아무것도 안 잰다", LocalPrefs.autostart(project))
        assertFalse("시험 안에서 데몬을 띄우려 한다", StartDaemon.enabled(project))
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

    /**
     * **이 화면은 데몬에 없는 것을 있다고도, 있는 것을 없다고도 말하지 않는다.**
     *
     * 예전에 여기 「the daemon has no door for the rest yet」는 줄이 있었고, `settings` 문이
     * 생긴 뒤 거짓이 됐다 — 그 줄이 이름 대는 키 셋을 그 문이 이미 열고 있었다. 문장에는 타입이
     * 없어서 어떤 시험도 안 울었다. 그래서 그 문장이 돌아오는 것을 여기서 막는다.
     *
     * 지운 것을 재는 시험이라 약해 보이지만, 이 부류가 하루에 넷 나왔다: 화면이 시스템에 대해
     * 단언하면 그 단언은 늙는다.
     */
    fun `test 설정 화면이 문의 부재를 단언하지 않는다`() {
        // 번들을 **파일로** 읽는다. MagiBundle.msg 는 없는 열쇠에 로거 에러를 내고 열쇠를
        // 그대로 돌려주므로, 그것으로 「없음」을 재면 시험이 에러 한 줄을 남기며 통과한다.
        val props = java.util.Properties()
        MagiBundle::class.java.getResourceAsStream("/messages/MagiBundle.properties")!!
            .use { props.load(java.io.InputStreamReader(it, Charsets.UTF_8)) }
        assertNull("문이 생기고도 남아 거짓이 된 문장이 돌아왔다: " + props.getProperty("set.byfile.what"),
            props.getProperty("set.byfile.what"))
    }

    /**
     * **데몬이 없으면 문이 준 칸도 없고, 따라서 바뀐 것도 없다.**
     *
     * `isModified` 에 문 칸 비교를 더했다. 그 줄이 데몬 없는 화면에서 참이 되면 **연 것만으로
     * 바뀜**이 되고, OK 를 누르면 아무도 고르지 않은 값이 데몬으로 나간다 — 이 파일이 이미
     * 겪고 적어 둔 결함이고(모름을 사람이 고른 것으로 다루지 않는다), 새 줄이 그것을 되살릴 수
     * 있는 자리다.
     */
    fun `test 데몬이 없으면 문 칸이 화면을 바뀜으로 만들지 않는다`() {
        val c = MagiConfigurable(project)
        c.createComponent()
        c.reset()
        assertFalse("데몬이 없는데 문 칸이 바뀜을 만든다 — 아무도 안 고른 값이 저장된다", c.isModified)
    
}

    /**
     * 회의는 자식을 **둘** 연다 — 말하는 자리와 받아적는 자리 — 그리고 이 판이 둘 다 그린다.
     *
     * 둘이 갈리는 것은 `origin` 하나뿐이라, 판이 그것을 사람 말로 옮기지 않으면 회의마다 같은
     * 모양 두 줄이 서고 어느 쪽이 무엇인지 누를 때까지 모른다.
     *
     * 「와이어 낱말 그대로가 아니다」는 여기서 못 잰다: 영어판의 값이 실제로 meeting·minutes 라
     * 옮겨도 안 옮겨도 같은 글자다(처음 이렇게 썼다가 빨갛게 섰다). 그래서 재는 것은 셋이다 —
     * 둘이 갈리는가, 모르는 것을 안 건드리는가, 그리고 열쇠가 정말 있는가.
     */
    fun testKnownOriginsAreSaidInWords() {
        val meeting = Look.originWord("meeting")
        val minutes = Look.originWord("minutes")
        assertTrue("회의 방과 회의록이 같은 말로 선다 — 두 줄을 못 가른다", meeting != minutes)
        // 모르는 것은 지어내지 않는다. 없는 열쇠를 MagiBundle 에 물으면 열쇠 자체가 찍히므로,
        // 그 경로로 새면 판에 `plan.kid.scout` 같은 것이 선다.
        assertEquals("모르는 origin 을 손대면 안 된다", "scout", Look.originWord("scout"))
        for (w in listOf(meeting, minutes)) {
            assertTrue("열쇠가 없어 열쇠 이름이 그대로 찍힌다: $w", !w.startsWith("plan."))
        }
    }

    /**
     * ★ **설정 화면의 「못 붙었다」는 맨 위에 전폭으로, 눈에 띄는 색으로 선다.**
     *
     * 사용자 보고(2026-09-09): 그 문장이 판 셋 아래 좁은 칸에 회색으로 서 있어 눈에 안 들어왔다.
     * 그런데 이 화면에서 가장 중요한 사실이다 — 붙지 않았으면 **아래 칸들이 보여 주는 값이 데몬의
     * 값이 아니고**, 사람이 그것을 모른 채 OK 를 누르면 화면이 보인 대로 저장된다.
     *
     * 세 가지를 잰다: 그 컴포넌트가 **첫 줄**인가(gridy 0), **두 칸을 걸치고 폭을 받나**, 그리고
     * 색과 글자가 **한 사건**인가 — 색만 남고 문장이 지워지면 화면이 지난 사실을 계속 주장한다.
     */
    fun `test 설정 화면의 못 붙었다는 맨 위에 눈에 띄게 선다`() {
        val cfg = MagiConfigurable(project)
        val root = cfg.createComponent()!!
        val layout = root.layout as java.awt.GridBagLayout
        // 첫 줄에 있는 컴포넌트를 찾는다. 「맨 위」는 자리이지 이름이 아니라, 자리로 잰다.
        val first = root.components.first { c ->
            layout.getConstraints(c).gridy == 0
        }
        val c = layout.getConstraints(first)
        assertEquals("가장 중요한 문장이 첫 줄이 아니다", 0, c.gridy)
        assertEquals("두 칸을 안 걸친다 — 좁은 칸에 접힌다", 2, c.gridwidth)
        assertTrue("폭을 안 받는다 — 한 줄이 여러 줄로 접힌다", c.weightx > 0.0)
        assertEquals("폭을 안 채운다", java.awt.GridBagConstraints.HORIZONTAL, c.fill)

        // 색과 글자가 한 사건. 문장을 지우면 색도 돌아와야 한다.
        val said = first as javax.swing.JTextArea
        cfg.reset()
        com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
        // reset 은 데몬을 물어본다. 시험 환경에 데몬이 없으므로 못 붙었다는 문장이 서고, 그때
        // 색이 눈에 띄는 쪽이어야 한다 — 회색이면 이 커밋이 고친 것이 되돌아간 것이다.
        if (said.text.isNotBlank() && said.text != " ") {
            assertEquals("못 붙었다는데 색이 회색이다", Look.warn, said.foreground)
        }
    }

    /**
     * **빈 판이 「아직 안 온 대화」와 「정말 빈 대화」를 가르는가 — 판에 선 글자로.**
     *
     * 이 줄을 지키던 것은 `core` 의 `SourceTextTest` 였고, 그것이 재는 것은 「그리는 코드가 있나」다.
     * 코드가 있는 것과 **그 화면이 그렇게 서는 것**은 다른 사실이고, 이 트리는 그 차이로 여러 번
     * 값을 치렀다(안 뜨는 액션, 판을 벌리는 라벨). 여기서는 진짜 IntelliJ 위에 판을 세우고 글자를
     * 묻는다.
     *
     * ⚠ **먼저 없다는 것부터 잰다.** 그 문장이 처음부터 서 있으면 이 시험은 아무것도 안 재고 초록이다.
     */
    fun `test 재생이 끝나면 빈 판이 최신까지 받았다고 말한다`() {
        val view = MagiToolWindow.View(project)
        try {
            com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
            val caught = MagiBundle.msg("chat.link.live")
            assertFalse(
                "붙기 전부터 「최신까지 받았다」가 서 있다 — 그러면 이 규칙은 아무것도 안 재고 초록이다",
                shown(view.root).contains(caught),
            )

            // 데몬이 재생의 끝을 댔다. 스트림이 자기 이야기를 하는 그 프레임이고, 배선이 없으면
            // 조용히 버려진다(그 침묵이 이 커밋들의 주제였다).
            view.sink.caughtUp()
            com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()

            val text = shown(view.root)
            assertTrue(
                "재생이 끝났는데 빈 판이 아무 말도 안 한다 — 아직 안 온 대화와 정말 빈 대화가 같은 그림이다: $text",
                text.contains(caught),
            )
            // 그리고 그 자리가 환영 안내다 — 제목이 같이 서야 사람이 무엇을 보는지 안다.
            assertTrue("환영 안내가 아니라 다른 자리에 적혔다: $text", text.contains(MagiBundle.msg("chat.welcome.title")))
        } finally {
            com.intellij.openapi.util.Disposer.dispose(view)
        }
    }

    /** 세운 판에 실제로 보이는 글자를 전부 모은다 — 라벨이든 텍스트 영역이든. */
    private fun shown(c: java.awt.Component): String = buildString {
        fun walk(x: java.awt.Component) {
            when (x) {
                is javax.swing.JLabel -> append(x.text.orEmpty()).append('\n')
                is javax.swing.text.JTextComponent -> append(x.text.orEmpty()).append('\n')
                else -> {}
            }
            if (x is java.awt.Container) x.components.forEach { walk(it) }
        }
        walk(c)
    }

    /**
     * 환영 안내는 제목·상태와 함께 동일한 중앙 축으로 정렬되고,
     * 320px, 420px 사이드바와 1300px 하단 독의 최초 배치 및 리사이즈에서 설명 높이가 온전히 확보되며
     * 마지막 줄 텍스트가 잘림 없이 표시 영역 안에 렌더링된다.
     */
    fun `test 환영 안내는 320px 420px 1300px 최초 배치 및 리사이즈에서 설명 높이가 확보되고 마지막 줄이 표시된다`() {
        val title = "magi"
        val status = "Connected"
        val englishHint = "This is a welcome message that guides the user on how to use magi with enter, shift+enter, and files. " +
            "It should wrap properly without horizontal scrolling or text clipping across all widths."
        val koreanHint = "이것은 마기 사용을 안내하는 환영 메시지로 Enter 전송, Shift+Enter 줄바꿈, @ 파일 첨부를 설명합니다. " +
            "모든 화면 폭에서 가로 스크롤이나 텍스트 잘림 없이 자연스럽게 줄바꿈되어야 합니다."

        // 1. 단락 정렬 속성 및 HTML 미해석, 선택 가능 여부 검증
        val samplePanel = Look.welcome(title, status, Look.success, englishHint)
        val titleLabel = samplePanel.getComponent(0) as JLabel
        val statusLabel = samplePanel.getComponent(1) as JLabel
        val sampleNote = samplePanel.getComponent(2) as JTextPane

        assertEquals(SwingConstants.CENTER, titleLabel.horizontalAlignment)
        assertEquals(SwingConstants.CENTER, statusLabel.horizontalAlignment)
        val doc = sampleNote.styledDocument
        val style = doc.getParagraphElement(0).attributes
        assertEquals(StyleConstants.ALIGN_CENTER, StyleConstants.getAlignment(style))
        assertFalse(sampleNote.isEditable)
        assertFalse(sampleNote.isOpaque)

        val rawText = "<b>bold</b> & special <chars>"
        val htmlTestPane = Look.welcomeNote(rawText) as JTextPane
        assertEquals("HTML 태그가 파싱되지 않고 원문 그대로 유지되어야 한다", rawText, htmlTestPane.text)

        // 2. 신규 패널마다 320px, 420px, 1300px 최초 배치 검증 (영문 및 한국어)
        for (hint in listOf(englishHint, koreanHint)) {
            val widths = listOf(320, 420, 1300)
            val heights = mutableMapOf<Int, Int>()
            for (w in widths) {
                val panel = Look.welcome(title, status, Look.success, hint)
                panel.size = Dimension(w, 1000)
                panel.doLayout() // 자식 크기를 강제로 수동 설정하지 않고 실제 배치 수행

                val note = panel.getComponent(2) as JTextPane
                val targetW = w - 32 // margin 16 on each side
                assertEquals("최초 배치에서 자식 폭은 부모 가용 폭을 채워야 한다", targetW, note.width)
                assertTrue("최초 배치에서 자식 높이는 preferredSize.height 이상이어야 한다: ${note.height} vs ${note.preferredSize.height}",
                    note.height >= note.preferredSize.height)
                heights[w] = note.height

                // 부모 콘텐츠 영역 안의 위치 단언
                assertEquals(16, note.bounds.x)
                assertEquals(targetW, note.bounds.width)
                assertTrue(note.bounds.y >= 0)
                assertTrue(note.bounds.y + note.bounds.height <= panel.height)

                // 실제 마지막 줄 표시 확인: 마지막 글자의 뷰 좌표 하단이 컴포넌트 높이 이내여야 함
                val lastRect = note.modelToView2D(note.document.length - 1)
                assertNotNull("마지막 글자 뷰 렉트가 존재해야 한다", lastRect)
                assertTrue("마지막 글자가 잘리지 않고 표시 영역 안에 있어야 한다: bottom=${lastRect!!.y + lastRect.height}, noteH=${note.height}",
                    lastRect.y + lastRect.height <= note.height.toDouble())
            }
            assertTrue("320px 높이가 420px 높이 이상이어야 한다: ${heights[320]} vs ${heights[420]}", heights[320]!! >= heights[420]!!)
            assertTrue("420px 높이가 1300px 높이 이상이어야 한다: ${heights[420]} vs ${heights[1300]}", heights[420]!! >= heights[1300]!!)
        }

        // 3. 단일 패널 리사이즈 회귀: 1300 -> 320 -> 420 -> 1300
        val resizePanel = Look.welcome(title, status, Look.success, englishHint)
        val resizeNote = resizePanel.getComponent(2) as JTextPane

        // 1300px 최초
        resizePanel.size = Dimension(1300, 1000)
        resizePanel.doLayout()
        val h1300 = resizeNote.height
        assertEquals(1300 - 32, resizeNote.width)
        assertTrue(resizeNote.height >= resizeNote.preferredSize.height)

        // 1300 -> 320 (이전 1300 폭의 높이를 유지하지 않고 320 높이로 즉시 재배치)
        resizePanel.size = Dimension(320, 1000)
        resizePanel.doLayout()
        val h320 = resizeNote.height
        assertEquals(320 - 32, resizeNote.width)
        assertTrue("320 리사이즈 시 1300보다 높이가 커져야 한다: $h320 > $h1300", h320 > h1300)
        assertTrue(resizeNote.height >= resizeNote.preferredSize.height)
        val lastRect320 = resizeNote.modelToView2D(resizeNote.document.length - 1)
        assertTrue("320 리사이즈 후 마지막 글자가 잘리지 않아야 한다", lastRect320!!.y + lastRect320.height <= resizeNote.height.toDouble())

        // 320 -> 420
        resizePanel.size = Dimension(420, 1000)
        resizePanel.doLayout()
        val h420 = resizeNote.height
        assertEquals(420 - 32, resizeNote.width)
        assertTrue("420 리사이즈 높이는 1300과 320 사이여야 한다: $h420 in ($h1300..$h320)", h420 in (h1300 + 1)..h320)
        assertTrue(resizeNote.height >= resizeNote.preferredSize.height)

        // 420 -> 1300 복귀
        resizePanel.size = Dimension(1300, 1000)
        resizePanel.doLayout()
        assertEquals(1300 - 32, resizeNote.width)
        assertEquals(h1300, resizeNote.height)
        assertTrue(resizeNote.height >= resizeNote.preferredSize.height)

        // 4. 긴 상태 문구(1000px 초과)의 최소 폭 클램프 검증
        val longStatus = "Very long status string that would normally expand the window width to over a thousand pixels if unconstrained"
        val longStatusPanel = Look.welcome("magi", longStatus, Look.warn, englishHint)
        val longStatusLabel = longStatusPanel.getComponent(1) as JLabel
        assertTrue("긴 상태 문구라도 최소 폭은 90 이하로 제한되어야 한다", longStatusLabel.minimumSize.width <= 90)
    }

    /**
     * 컴포저 플레이스홀더와 접근 가능한 이름이 실제 View 액션과 상태 전이에 일치한다.
     * 일반 전송 A 대기 중 B 편집·전송, 답변 대기 중 직접 입력 재진입, 답변 모드에서 연결 종료·재연결, 세션 전환을 실제 View 액션으로 검증한다.
     */
    fun `test 컴포저 플레이스홀더와 접근 가능한 이름이 실제 View 액션과 상태 전이에 일치한다`() {
        class PendingSend(
            val id: Int,
            val session: String,
            val kind: String,
            private val errCb: (String) -> Unit,
            private val work: (Companion) -> Unit,
        ) {
            var completed = false
                private set
            val requests = mutableListOf<Request>()

            fun complete(ok: Boolean = true, error: String? = null, connectionError: Boolean = false) {
                check(!completed) { "PendingSend #$id ($kind on $session) already completed" }
                completed = true
                if (connectionError) {
                    errCb(error ?: "failed")
                } else {
                    work(Companion(object : Daemon {
                        override fun exchange(request: Request): Response {
                            requests.add(request)
                            return if (ok) Response(ok = true) else Response(ok = false, error = error ?: "failed")
                        }
                        override fun stream(request: Request, each: (Response) -> Boolean) {}
                        override fun close() {}
                    }, session))
                }
                UIUtil.dispatchAllInvocationEvents()
            }
        }

        val pendingSends = mutableListOf<PendingSend>()
        var currentSession = "session1"
        lateinit var view: MagiToolWindow.View
        view = MagiToolWindow.View(
            project,
            sendSession = { currentSession },
            sendConnection = { sid, error, work ->
                val answersRef = view.javaClass.getDeclaredField("answers").apply { isAccessible = true }.get(view) as dev.sayaya.magi.ide.usecase.AnswerDrafts
                val isAnswer = answersRef.question != null && answersRef.busy(answersRef.question) &&
                        pendingSends.none { it.session == sid && it.kind == "answer" && !it.completed }
                val kind = if (isAnswer) "answer" else "say"
                val handle = PendingSend(pendingSends.size + 1, sid, kind, error, work)
                pendingSends.add(handle)
            }
        )
        try {
            UIUtil.dispatchAllInvocationEvents()
            @Suppress("UNCHECKED_CAST")
            fun <T> field(name: String): T =
                view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view) as T

            val input: JBTextArea = field("input")
            val buttons: JPanel = field("buttons")
            val head: JPanel = field("head")
            val sendButton: JButton = field("sendButton")
            val answerBar: JPanel = field("answerBar")
            val answerCancel: JButton = field("answerCancel")

            val defaultMsg = MagiBundle.msg("chat.composer.hint.default")
            val answerMsg = MagiBundle.msg("chat.composer.hint.answer")
            val busyMsg = MagiBundle.msg("chat.composer.hint.busy")
            val disconnectedMsg = MagiBundle.msg("chat.composer.hint.disconnected")

            // 1. 초기 비연결 상태: chat.composer.hint.disconnected
            assertEquals(disconnectedMsg, input.emptyText.text)
            assertEquals(disconnectedMsg, input.accessibleContext.accessibleName)
            assertEquals("", input.text)

            // 2. 연결 완료: chat.composer.hint.default
            view.sink.caughtUp()
            UIUtil.dispatchAllInvocationEvents()
            assertEquals(defaultMsg, input.emptyText.text)
            assertEquals(defaultMsg, input.accessibleContext.accessibleName)
            assertEquals("", input.text)

            // 3. 일반 전송 A 대기 중 B 편집·전송
            input.text = "Message A"
            UIUtil.dispatchAllInvocationEvents()
            assertEquals("Message A", input.text)
            // Enter 입력으로 실제 일반 전송 A 실행
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            val sendA = pendingSends.last()
            assertEquals("session1", sendA.session)
            assertEquals("say", sendA.kind)
            // A 전송 중이며 추가 편집이 없으므로 hint는 busy
            assertEquals(busyMsg, input.emptyText.text)
            assertEquals(busyMsg, input.accessibleContext.accessibleName)

            // A 대기 중에 사용자가 B를 편집
            input.text = "Message B"
            UIUtil.dispatchAllInvocationEvents()
            // 새 revision이므로 전송 가능! hint는 default로 복귀해야 함
            assertEquals(defaultMsg, input.emptyText.text)
            assertEquals(defaultMsg, input.accessibleContext.accessibleName)

            // Enter 입력으로 실제 B 전송 실행
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            val sendB = pendingSends.last()
            assertEquals("session1", sendB.session)
            assertEquals("say", sendB.kind)
            assertEquals(busyMsg, input.emptyText.text)
            assertEquals(busyMsg, input.accessibleContext.accessibleName)

            // A 완료 (B는 아직 in-flight)
            sendA.complete(ok = true)
            assertEquals(busyMsg, input.emptyText.text)
            val sayReqA = sendA.requests.first { it.method in listOf("submit", "steer") }
            assertEquals("Message A", sayReqA.text)

            // B 완료
            sendB.complete(ok = true)
            assertEquals(defaultMsg, input.emptyText.text)
            assertEquals(defaultMsg, input.accessibleContext.accessibleName)
            val sayReqB = sendB.requests.first { it.method in listOf("submit", "steer") }
            assertEquals("Message B", sayReqB.text)

            // 4. 답변 대기 중 직접 입력 재진입
            val w = Waiting(id = "q1", kind = "question", what = "Confirm?", options = listOf("Alpha", "Beta"))
            val drawPromptMethod = view.javaClass.getDeclaredMethod("drawPrompt", Waiting::class.java, String::class.java).apply { isAccessible = true }
            drawPromptMethod.invoke(view, w, "session1")
            UIUtil.dispatchAllInvocationEvents()
            assertEquals(defaultMsg, input.emptyText.text) // 아직 질문 모드 진입 전

            val directBtnText = MagiBundle.msg("chat.answer.direct")
            val directBtn = buttons.components.filterIsInstance<JButton>().first { it.text == directBtnText }
            directBtn.doClick()
            UIUtil.dispatchAllInvocationEvents()
            assertTrue(answerBar.isVisible)
            assertEquals(answerMsg, input.emptyText.text)
            assertEquals(answerMsg, input.accessibleContext.accessibleName)

            // 답변 작성 후 취소
            input.text = "Answer Draft 1"
            UIUtil.dispatchAllInvocationEvents()
            answerCancel.doClick()
            UIUtil.dispatchAllInvocationEvents()
            assertFalse(answerBar.isVisible)
            assertEquals(defaultMsg, input.emptyText.text)

            // 직접 입력 재진입: 기존 초안 복원 및 answer 힌트 표출
            val directBtn2 = buttons.components.filterIsInstance<JButton>().first { it.text == directBtnText }
            directBtn2.doClick()
            UIUtil.dispatchAllInvocationEvents()
            assertTrue(answerBar.isVisible)
            assertEquals(answerMsg, input.emptyText.text)
            assertEquals("Answer Draft 1", input.text)

            // 5. 답변 모드에서 연결 종료 및 재연결
            view.sink.ended(End.ByUs)
            UIUtil.dispatchAllInvocationEvents()
            assertEquals(disconnectedMsg, input.emptyText.text)
            assertEquals(disconnectedMsg, input.accessibleContext.accessibleName)

            view.sink.caughtUp()
            UIUtil.dispatchAllInvocationEvents()
            assertEquals(answerMsg, input.emptyText.text)
            assertEquals(answerMsg, input.accessibleContext.accessibleName)

            // 6. 세션 전환
            currentSession = "session2"
            drawPromptMethod.invoke(view, null, "session2")
            UIUtil.dispatchAllInvocationEvents()
            assertFalse(answerBar.isVisible)
            assertEquals(defaultMsg, input.emptyText.text)

            currentSession = "session1"
            drawPromptMethod.invoke(view, w, "session1")
            UIUtil.dispatchAllInvocationEvents()
            assertFalse(answerBar.isVisible)
            assertEquals(defaultMsg, input.emptyText.text)

            val directBtn3 = buttons.components.filterIsInstance<JButton>().first { it.text == directBtnText }
            directBtn3.doClick()
            UIUtil.dispatchAllInvocationEvents()
            assertTrue(answerBar.isVisible)
            assertEquals(answerMsg, input.emptyText.text)
            assertEquals("Answer Draft 1", input.text)

            // 7. 답변 A 제출 및 RPC 대기(in-flight) 중 일반 메시지 G 실제 작성 및 Enter 전송 (§6.30 누락 경로 1)
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            val answerA = pendingSends.last()
            assertEquals("session1", answerA.session)
            assertEquals("answer", answerA.kind)

            // 제출 직후 일반 모드로 복귀하여 일반 입력 허용 및 default 힌트 표출
            assertFalse(answerBar.isVisible)
            assertEquals(defaultMsg, input.emptyText.text)
            assertEquals(defaultMsg, input.accessibleContext.accessibleName)
            assertEquals("", input.text)

            // 일반 메시지 G를 실제로 작성하고 Enter 키 액션으로 전송
            input.text = "General G"
            UIUtil.dispatchAllInvocationEvents()
            assertEquals("General G", input.text)
            assertEquals(defaultMsg, input.emptyText.text)
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            val sendG = pendingSends.last()
            assertEquals("session1", sendG.session)
            assertEquals("say", sendG.kind)
            // 일반 메시지 G가 in-flight 상태이므로 busy 힌트 표출 및 입력 텍스트 유지
            assertEquals(busyMsg, input.emptyText.text)
            assertEquals("General G", input.text)

            // 일반 메시지 G의 완료(성공) 전달
            sendG.complete(ok = true)
            // 일반 요청의 method(submit/steer)와 text("General G") 확인
            val gSayReq = sendG.requests.first { it.method in listOf("submit", "steer") }
            assertTrue(gSayReq.method in listOf("submit", "steer"))
            assertEquals("General G", gSayReq.text)

            // G의 완료(성공)로 인해 같은 revision의 입력창이 비워지고 default 힌트 복귀
            assertEquals(defaultMsg, input.emptyText.text)
            assertEquals("", input.text)

            // 답변 모드 재진입: 여전히 A가 in-flight 상태이므로 busy 힌트, 초안 A 보존, sendButton 및 선택지 비활성화
            val directBtnInFlight = buttons.components.filterIsInstance<JButton>().first { it.text == directBtnText }
            directBtnInFlight.doClick()
            UIUtil.dispatchAllInvocationEvents()
            assertTrue(answerBar.isVisible)
            assertEquals(busyMsg, input.emptyText.text)
            assertEquals(busyMsg, input.accessibleContext.accessibleName)
            assertEquals("Answer Draft 1", input.text)
            assertFalse(sendButton.isEnabled)
            val choiceBtns = buttons.components.filterIsInstance<JButton>().filter { it.getClientProperty("magi.answerChoice") == true }
            assertTrue(choiceBtns.isNotEmpty() && choiceBtns.all { !it.isEnabled })

            // in-flight 중 Enter 입력 시 중복 전송 차단 (새로운 pendingSend가 생성되지 않음)
            val sendCountBeforeDuplicate = pendingSends.size
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            assertEquals(sendCountBeforeDuplicate, pendingSends.size)

            // 8. 답변 A의 RPC 실패 결과(Timeout) 수신 및 재시도 가능 안내 복귀
            // 답변 요청의 method == "answer", callId == "q1", answer == "Answer Draft 1" 실측 검증
            answerA.complete(ok = false, error = "Timeout")
            val aAnswerReq = answerA.requests.first { it.method == "answer" }
            assertEquals("answer", aAnswerReq.method)
            assertEquals("q1", aAnswerReq.callId)
            assertEquals("Answer Draft 1", aAnswerReq.answer)

            // 실패 후: answer 힌트로 복귀, sendButton 재활성화, 기존 초안 A 보존
            assertTrue(answerBar.isVisible)
            assertEquals(answerMsg, input.emptyText.text)
            assertEquals(answerMsg, input.accessibleContext.accessibleName)
            assertTrue(sendButton.isEnabled)
            assertEquals("Answer Draft 1", input.text)

            // 9. session1 답변 재시도 보류 + session2 질문 q2 답변 B 잠금 생성 및 격리 검증 (§6.30 누락 경로 2)
            // session1에서 답변 재시도 A2 제출 후 보류
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            val s1AnswerRetry = pendingSends.last()
            assertEquals("session1", s1AnswerRetry.session)
            assertEquals("answer", s1AnswerRetry.kind)
            assertFalse(answerBar.isVisible)
            assertEquals(defaultMsg, input.emptyText.text)

            // session1에서 백그라운드 일반 메시지도 발송하여 보류 (실패 전달용)
            input.text = "Session 1 background note"
            UIUtil.dispatchAllInvocationEvents()
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            val s1General = pendingSends.last()
            assertEquals("session1", s1General.session)
            assertEquals("say", s1General.kind)

            // session2로 전환 및 별도 질문 q2 수신
            currentSession = "session2"
            val q2 = Waiting(id = "q2", kind = "question", what = "Proceed on S2?", options = listOf("Yes", "No"))
            drawPromptMethod.invoke(view, q2, "session2")
            UIUtil.dispatchAllInvocationEvents()
            assertFalse(answerBar.isVisible)
            assertEquals(defaultMsg, input.emptyText.text)

            // session2에서 직접 입력 클릭 -> 답변 B("Answer Draft 2") 작성 -> Enter 제출
            val directBtnS2 = buttons.components.filterIsInstance<JButton>().first { it.text == directBtnText }
            directBtnS2.doClick()
            UIUtil.dispatchAllInvocationEvents()
            assertTrue(answerBar.isVisible)
            assertEquals(answerMsg, input.emptyText.text)

            input.text = "Answer Draft 2"
            UIUtil.dispatchAllInvocationEvents()
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            val s2AnswerB = pendingSends.last()
            assertEquals("session2", s2AnswerB.session)
            assertEquals("answer", s2AnswerB.kind)
            assertFalse(answerBar.isVisible)
            assertEquals(defaultMsg, input.emptyText.text)

            // session2에서 동일 q2의 "직접 입력" 재진입하여 session2에 잠금 생성
            val directBtnS2Reenter = buttons.components.filterIsInstance<JButton>().first { it.text == directBtnText }
            directBtnS2Reenter.doClick()
            UIUtil.dispatchAllInvocationEvents()

            // session2 잠금 상태 단언: busy 힌트, 초안 B, sendButton 비활성화, 선택지 비활성화, Enter 중복 차단
            assertTrue(answerBar.isVisible)
            assertEquals(busyMsg, input.emptyText.text)
            assertEquals(busyMsg, input.accessibleContext.accessibleName)
            assertEquals("Answer Draft 2", input.text)
            assertFalse(sendButton.isEnabled)
            val choiceBtnsS2 = buttons.components.filterIsInstance<JButton>().filter { it.getClientProperty("magi.answerChoice") == true }
            assertTrue(choiceBtnsS2.isNotEmpty() && choiceBtnsS2.all { !it.isEnabled })

            val s2SendCountBefore = pendingSends.size
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            assertEquals(s2SendCountBefore, pendingSends.size)

            // session1의 옛 실패 결과 전달 시 session2 잠금 보존 확인
            s1General.complete(ok = false, error = "S1 network error")
            assertTrue(answerBar.isVisible)
            assertEquals(busyMsg, input.emptyText.text)
            assertEquals(busyMsg, input.accessibleContext.accessibleName)
            assertEquals("Answer Draft 2", input.text)
            assertFalse(sendButton.isEnabled)
            assertTrue(choiceBtnsS2.all { !it.isEnabled })
            val s2CountAfterS1Fail = pendingSends.size
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            assertEquals(s2CountAfterS1Fail, pendingSends.size)

            // session1의 옛 성공 결과 전달 시 session2 잠금 보존 확인
            s1AnswerRetry.complete(ok = true)
            val s1RetryReq = s1AnswerRetry.requests.first { it.method == "answer" }
            assertEquals("answer", s1RetryReq.method)
            assertEquals("q1", s1RetryReq.callId)
            assertEquals("Answer Draft 1", s1RetryReq.answer)

            // session2의 잠금(B, busy 안내, 전송 비활성화, 선택지 비활성화, Enter 중복 차단)이 온전히 유지됨을 단언
            assertTrue(answerBar.isVisible)
            assertEquals(busyMsg, input.emptyText.text)
            assertEquals(busyMsg, input.accessibleContext.accessibleName)
            assertEquals("Answer Draft 2", input.text)
            assertFalse(sendButton.isEnabled)
            assertTrue(choiceBtnsS2.all { !it.isEnabled })
            val s2CountAfterS1Success = pendingSends.size
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            assertEquals(s2CountAfterS1Success, pendingSends.size)

            // 대조군: session2 자신의 결과가 왔을 때 비로소 잠금이 해제됨을 검증
            // 실패 시: answerMsg로 복귀, sendButton 활성화, 초안 B 보존
            s2AnswerB.complete(ok = false, error = "S2 Timeout")
            val s2FailReq = s2AnswerB.requests.first { it.method == "answer" }
            assertEquals("answer", s2FailReq.method)
            assertEquals("q2", s2FailReq.callId)
            assertEquals("Answer Draft 2", s2FailReq.answer)

            assertTrue(answerBar.isVisible)
            assertEquals(answerMsg, input.emptyText.text)
            assertEquals(answerMsg, input.accessibleContext.accessibleName)
            assertTrue(sendButton.isEnabled)
            assertEquals("Answer Draft 2", input.text)

            // session2 재전송 후 성공 시: 답변 바 닫힘, defaultMsg 복귀, q2 종료
            input.actionMap.get("magi.send").actionPerformed(ActionEvent(input, 0, "magi.send"))
            UIUtil.dispatchAllInvocationEvents()
            val s2AnswerB2 = pendingSends.last()
            assertEquals("session2", s2AnswerB2.session)
            assertEquals("answer", s2AnswerB2.kind)
            s2AnswerB2.complete(ok = true)
            val s2SuccessReq = s2AnswerB2.requests.first { it.method == "answer" }
            assertEquals("answer", s2SuccessReq.method)
            assertEquals("q2", s2SuccessReq.callId)
            assertEquals("Answer Draft 2", s2SuccessReq.answer)

            assertFalse(answerBar.isVisible)
            assertEquals(defaultMsg, input.emptyText.text)
            assertEquals("", input.text)

            // session1 복귀 시 답변 완료에 따른 프롬프트 숨김 및 일반 모드 확인
            currentSession = "session1"
            drawPromptMethod.invoke(view, w, "session1")
            UIUtil.dispatchAllInvocationEvents()
            assertFalse(head.isVisible)
            assertFalse(answerBar.isVisible)
            assertEquals(defaultMsg, input.emptyText.text)

            // 10. 문서 텍스트 무유입 검증
            input.text = ""
            UIUtil.dispatchAllInvocationEvents()
            assertEquals(defaultMsg, input.emptyText.text)
            assertEquals("", input.text)
        } finally {
            Disposer.dispose(view)
        }
    }
}
