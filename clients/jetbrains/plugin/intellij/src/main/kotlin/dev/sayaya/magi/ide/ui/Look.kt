package dev.sayaya.magi.ide.ui

import com.intellij.openapi.editor.colors.EditorColorsManager
import com.intellij.openapi.editor.colors.EditorFontType
import com.intellij.ui.JBColor
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.util.ui.JBFont
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.usecase.Palette
import java.awt.BorderLayout
import java.awt.Color
import java.awt.Dimension
import java.awt.Font
import javax.swing.BorderFactory
import javax.swing.JComponent

/**
 * 이 플러그인이 칠하는 것. 콘솔의 설계 언어(`docs/UI.md` §3)를 IDE 로 옮긴 자리다.
 *
 * ### 한 가지를 일부러 다르게 한다 (§6a 는 어긴 것을 적으라고 한다)
 *
 * **판은 IDE 테마가 칠하고, 글자는 팔레트가 칠한다.** 콘솔은 배경까지 자기 팔레트로 그리는데
 * 여기서 그러면 사람이 고른 테마 한가운데에 남의 색 판이 하나 서고, §5 의 첫 규칙("IDE 와
 * 겹치는 것은 만들지 않는다")을 색으로 어기게 된다. 툴윈도는 Run·Terminal 옆에 서는 자리라
 * 거기만 다른 회색이면 그건 예쁜 것이 아니라 **덜 붙은 것**으로 보인다.
 *
 * 뜻이 있는 색은 반대다. 실패의 붉음, 카운슬 세 자리, 「눌러서 갈 수 있음」의 청록은 **같은
 * 물건에 대한 같은 약속**이라 터미널·웹과 갈리면 안 된다. 그래서 그 색만 [Palette] 에서
 * 그대로 온다(그 값이 원본과 같다는 것은 `PaletteTest` 가 붙든다).
 *
 * 그러니 여기서 배경을 칠하는 함수는 없다. 있으면 다음 사람이 쓴다.
 */
internal object Look {

    private fun of(ink: Palette.Ink) = JBColor(Color.decode(ink.light), Color.decode(ink.dark))

    /** 답을 기다리는 것. 지금 사람이 손대야 하는 자리에만 쓴다. */
    val primary = of(Palette.primary)

    /** 눌러서 갈 수 있는 것 — 경로와 줄 번호. */
    val accent = of(Palette.accent)

    /** 본문. */
    val body = of(Palette.onSurface)

    /** 읽히되 앞에 안 나서는 것 — 일련번호, 시각, 창이 스스로 하는 말. */
    val faint = of(Palette.onSurfaceVariant)

    /** 그보다 더 뒤로. */
    val muted = of(Palette.muted)

    /** 구역을 가르는 실선. */
    val edge = of(Palette.outlineVariant)

    val error = of(Palette.error)
    val warn = of(Palette.warn)
    val success = of(Palette.success)

    /**
     * 카운슬 자리 셋의 색. **그 셋 말고는 아무도 색을 못 받는다.**
     *
     * 색은 뜻이다. 아무 이름에나 색을 돌려 주면 화면이 「이 둘은 다른 종류다」라고 말하게 되는데,
     * 이 창은 그 사실을 모른다 — 전사에 실린 것은 이름뿐이다. 콘솔도 같은 선을 긋는다
     * (`Rows.java` 의 `m-melchior`/`m-balthasar`/`m-casper`, 그 밖은 없음).
     *
     * 카스퍼가 보라인 사유는 `console.css` 에 실측으로 적혀 있다 — 붉은 계열이면 「누가 말했나」를
     * 적는 자리가 거절과 구별이 안 된다.
     */
    fun seat(who: String?): JBColor? = when (who?.lowercase()) {
        "melchior" -> of(Palette.melchior)
        "balthasar" -> of(Palette.balthasar)
        "casper" -> of(Palette.casper)
        else -> null
    }

    /**
     * 기계가 말하고 한 것을 적는 글꼴. §3.3: "거기 한 줄 한 줄이 기계가 말했거나 한 것이고,
     * 세리프는 증거에 옷을 입히는 것이다."
     *
     * **사람이 고른 편집기 글꼴을 그대로 쓴다.** 우리가 이름으로 고르면 IDE 안에서 코드와 전사가
     * 서로 다른 고정폭이 되고, 그건 같은 창에서 두 벌을 배우게 하는 것이다. 게으르게 묻는다 —
     * 테마를 바꾸면 다음에 그리는 것부터 따라간다.
     */
    fun mono(): Font = EditorColorsManager.getInstance().globalScheme.getFont(EditorFontType.PLAIN)

    /**
     * 구역 이름표. §3.1a 의 도랑에 서는 작은 대문자 라벨을 옮긴 것 — 한국어에 작은 대문자가
     * 없으므로 크기와 흐림으로만 나타낸다.
     */
    fun gutter(text: String): JBLabel = JBLabel(text).apply {
        font = JBFont.small()
        foreground = faint
        border = JBUI.Borders.empty(6, 10, 4, 10)
    }

    /**
     * 글자가 판 가장자리에 붙지 않게 하는 여백. §3.1a 의 편집 지면을 옮긴 최소치다 — 콘솔의
     * 74ch 지면은 여기서 못 쓴다(툴윈도 폭은 사람이 정한다). 대신 **어디에도 0 여백이 없다**
     * 는 것만 지킨다. 고치기 전 이 창은 라벨과 판이 전부 0 이었다.
     */
    val quiet = JBUI.Borders.empty(6, 12)

    /**
     * 기계가 한 말이 쌓이는 판.
     *
     * `JBTextArea` 가 아니라 [javax.swing.JTextPane] 인 이유는 **한 줄 안에서 색이 갈려야**
     * 해서다 — 일련번호는 뒤로, 이름은 자리 색으로, 창이 스스로 하는 말은 기울여서. 글자는 그대로
     * 두고 색만 얹는다: 무엇을 적을지는 데몬이 정하고 이 창은 얕게 그린다는 규칙
     * (`MagiToolWindow.entry`)이 그대로다.
     */
    fun pane(): javax.swing.JTextPane = javax.swing.JTextPane().apply {
        isEditable = false
        font = mono()
        border = JBUI.Borders.empty(8, 10)
    }

    /** 답을 기다리는 물음에 세우는 왼쪽 막대. 콘솔의 `.row.pending .txt` 를 그대로 옮긴 것이다. */
    fun pending(): javax.swing.border.Border = BorderFactory.createCompoundBorder(
        BorderFactory.createMatteBorder(0, 2, 0, 0, primary), JBUI.Borders.empty(6, 10)
    )

    /** 구역을 가르는 실선 한 줄. */
    fun rule(): JComponent = JBPanel<JBPanel<*>>().apply {
        background = edge
        preferredSize = Dimension(1, 1)
        maximumSize = Dimension(Int.MAX_VALUE, 1)
        minimumSize = Dimension(1, 1)
    }

    /** 이름표를 이고 있는 구역. 전사와 문제 판이 각자 무엇인지 말하게 한다. */
    fun titled(name: String, content: JComponent): JBPanel<JBPanel<*>> =
        JBPanel<JBPanel<*>>(BorderLayout()).apply {
            val head = JBPanel<JBPanel<*>>(BorderLayout()).apply {
                isOpaque = false
                add(gutter(name), BorderLayout.CENTER)
                add(rule(), BorderLayout.SOUTH)
            }
            add(head, BorderLayout.NORTH)
            add(content, BorderLayout.CENTER)
        }
}
