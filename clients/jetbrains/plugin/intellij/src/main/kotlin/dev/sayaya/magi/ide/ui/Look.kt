package dev.sayaya.magi.ide.ui

import com.intellij.openapi.editor.colors.EditorColorsManager
import com.intellij.openapi.editor.colors.EditorFontType
import com.intellij.ui.JBColor
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.util.ui.JBFont
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.usecase.Palette
import com.intellij.openapi.ui.VerticalFlowLayout
import java.awt.BorderLayout
import java.awt.Rectangle
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

    /**
     * 본문 — 그리고 아래 회색 셋. **IDE 의 대응 롤에서 오고, 팔레트는 폴백이다.**
     *
     * M3 의 색은 팔레트가 아니라 「배경 X 위에는 on-X」라는 **짝**이고, 짝이 대비를 보장한다
     * (스킬 §1 의 판정). 이 창의 배경은 IDE 테마가 칠하므로 — §6a 에 기록된 그 이탈 — 짝을
     * 보장할 수 있는 것도 테마뿐이다: 회색 계열을 팔레트 고정값으로 얹으면 낯선 테마에서
     * 대비가 미검증이 된다. 그래서 **뜻이 있는 색만** 팔레트에서 오고(아래 primary·자리색·
     * error 들 — 세 화면이 공유하는 약속), 뜻 없는 회색은 테마의 손에 맡긴다.
     */
    val body = JBColor.namedColor("Label.foreground", of(Palette.onSurface))

    /** 읽히되 앞에 안 나서는 것 — 일련번호, 시각, 창이 스스로 하는 말. */
    val faint = JBColor.namedColor("Label.infoForeground", of(Palette.onSurfaceVariant))

    /** 그보다 더 뒤로. */
    val muted = JBColor.namedColor("Component.infoForeground", of(Palette.muted))

    /** 구역을 가르는 실선. */
    val edge = JBColor.namedColor("Separator.separatorColor", of(Palette.outlineVariant))

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
     * 두고 색만 얹는다: 무엇을 적을지는 셰이퍼가 정하고 이 창은 붓만 잡는다는 규칙
     * (`MagiToolWindow.renderRow`)이 그대로다.
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

    // ── 전사 행의 붓들. 무엇을 적을지는 셰이퍼가 정하고(`MagiToolWindow.renderRow`), 여기는
    // 행 하나가 어떻게 서는지만 안다 — 텍스트 판 하나에 다 밀어 넣던 동안 전사가 여백 없는
    // 로그 덤프로 읽혔다(사용자 실측). 색·글꼴 규칙은 위 것들을 그대로 쓴다.

    /** 행들이 쌓이는 열. 뷰포트 폭을 따라가야 본문이 접힌다 — 전사에 가로 스크롤은 없다. */
    fun column(): JBPanel<JBPanel<*>> =
        object : JBPanel<JBPanel<*>>(VerticalFlowLayout(VerticalFlowLayout.TOP, 0, 0, true, false)),
            javax.swing.Scrollable {
            override fun getPreferredScrollableViewportSize(): Dimension = preferredSize
            override fun getScrollableUnitIncrement(v: Rectangle, o: Int, d: Int) = 16
            override fun getScrollableBlockIncrement(v: Rectangle, o: Int, d: Int) = v.height
            override fun getScrollableTracksViewportWidth() = true
            override fun getScrollableTracksViewportHeight() = false
        }

    /** 행 하나의 여백. 사이가 없으면 대화가 로그로 읽힌다. */
    fun row(): javax.swing.border.Border = JBUI.Borders.empty(8, 12)

    /** 답을 기다리는 행 — [pending] 의 왼쪽 막대를 행 판에 두른 것. */
    fun pendingRow(): javax.swing.border.Border = BorderFactory.createCompoundBorder(
        BorderFactory.createMatteBorder(0, 2, 0, 0, primary), JBUI.Borders.empty(6, 10, 6, 12),
    )

    /** 말 행의 머리 — 누가, 표시들, 오른끝에 시각. */
    fun rowHead(name: String, hue: Color, marks: List<Pair<String, Color>>, time: String): JComponent =
        JBPanel<JBPanel<*>>().apply {
            layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.X_AXIS)
            isOpaque = false
            add(JBLabel("● ").apply { font = JBFont.small(); foreground = hue })
            add(JBLabel(name).apply { font = JBFont.small().asBold(); foreground = hue })
            for ((t, c) in marks) {
                add(javax.swing.Box.createHorizontalStrut(8))
                add(JBLabel(t).apply { font = JBFont.small(); foreground = c })
            }
            add(javax.swing.Box.createHorizontalGlue())
            if (time.isNotEmpty()) add(JBLabel(time).apply { font = JBFont.small(); foreground = muted })
        }

    /** 도구 행의 머리 — `· 이름 ✓  인자…`. 인자는 뒤로 물러난 한 줄이다. */
    fun toolHead(name: String, glyph: String, hue: Color, args: String, time: String): JComponent =
        JBPanel<JBPanel<*>>().apply {
            layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.X_AXIS)
            isOpaque = false
            add(JBLabel("· $name").apply { font = mono().deriveFont(JBFont.small().size.toFloat()); foreground = body })
            add(javax.swing.Box.createHorizontalStrut(6))
            add(JBLabel(glyph).apply { font = JBFont.small(); foreground = hue })
            if (args.isNotEmpty()) {
                add(javax.swing.Box.createHorizontalStrut(10))
                add(JBLabel(args).apply { font = JBFont.small(); foreground = muted })
            }
            add(javax.swing.Box.createHorizontalGlue())
            if (time.isNotEmpty()) add(JBLabel(time).apply { font = JBFont.small(); foreground = muted })
        }

    /**
     * 산문 한 줄 상한 — M3 「모든 브레이크포인트에서 한 줄 40–60자」(guide-foundations §3).
     * 집 규칙 「새 화면에 max-width 금지」의 **명시된 예외 대상**이 정확히 이것이다: 상한은
     * 판이 아니라 **읽는 글**에 건다 — 행 판은 여전히 뷰포트 폭을 다 쓰고, 글만 왼쪽에서
     * 60ch 에 선다. 웹 콘솔이 74ch 로 어겨 재검토 대상에 올랐던 그 항목의 IDE 쪽 답이다.
     *
     * 스윙의 랩-높이 함정이 이걸 미루게 했던 이유다: JTextArea 의 선호 높이는 **지금 폭**
     * 기준 랩을 답하므로, 폭을 좁혀 놓고 높이를 물어야 한다. 그래서 선호 크기를 묻기 전에
     * 목표 폭으로 setSize 를 먼저 친다 — 레이아웃 전 컴포넌트에 크기를 심어 랩 높이를 세게
     * 하는 표준 요령이고, 이 순서가 없으면 접힌 글이 한 줄 높이로 잘린다.
     *
     * 자 셈은 ch(숫자 0 의 폭) 기준이다 — 한글은 전각이라 실제로는 ~30자 안팎에서 접힌다.
     * M3 의 40–60 이 라틴 셈이므로 이쪽이 원문에 충실하다.
     */
    private fun measured(area: javax.swing.JTextArea): JComponent =
        object : JBPanel<JBPanel<*>>(null) {
            init {
                isOpaque = false
                // 지금은 죽은 손잡이다(리뷰 F5 근거 정정): 아래 getMaximumSize 폭이 MAX 라
                // BoxLayout 은 래퍼를 항상 전폭으로 늘이고 정렬이 낄 자리가 없다. 최대폭을
                // 상한으로 줄이는 날 좌측 고정이 필요해지므로 그때를 위해 박아 둔다.
                alignmentX = LEFT_ALIGNMENT
                add(area)
            }
            private fun cap(): Int =
                area.getFontMetrics(area.font).charWidth('0') * 60 +
                    area.insets.left + area.insets.right
            // 「이 폭에서 글은 몇 픽셀인가」는 core 의 한 벌이 답한다 — 두 오버라이드가 각자
            // 셈하던 동안 폭 0(콜드 첫 패스)에서 갈라져 서로 되돌렸다(리뷰 F1; 시험은
            // core 의 MeasureTest — 이 모듈엔 시험 소스셋이 없다).
            private fun w(): Int = dev.sayaya.magi.ide.usecase.Measure.proseWidth(width, cap())
            override fun doLayout() {
                area.setBounds(0, 0, w(), height)
            }
            override fun getPreferredSize(): Dimension {
                val w = w()
                if (area.width != w) area.setSize(w, Integer.MAX_VALUE / 2)
                return Dimension(w, area.preferredSize.height)
            }
            override fun getMinimumSize(): Dimension = Dimension(0, preferredSize.height)
            override fun getMaximumSize(): Dimension =
                Dimension(Integer.MAX_VALUE, preferredSize.height)
        }

    /**
     * 본문. 접히고(줄 단위), 고르지 않고, **UI 글꼴**을 쓰고, 한 줄이 60ch 를 안 넘는다([measured]).
     *
     * §3.3(기계의 말은 고정폭)을 여기서 **일부러 어긴다**(§6a: 어긴 것은 적는다). 행 구조를
     * 컴포넌트로 바꾸고도 화면이 "예전 로그와 똑같다"고 읽힌 실측이 있었고, 남은 원인이
     * 이것이었다 — 한국어 산문이 고정폭 한 색이면 구조를 어떻게 짜도 덤프로 보인다. 고정폭이
     * 증거의 옷인 것은 **옮겨 적을 것**(도구 이름·인자·경로)의 이야기라 그쪽([toolHead],
     * [aside])에 남긴다. 본문은 사람이 읽는 글이다.
     */
    fun prose(text: String): JComponent = measured(javax.swing.JTextArea(text).apply {
        isEditable = false
        isOpaque = false
        lineWrap = true
        wrapStyleWord = true
        font = JBFont.regular()
        foreground = body
        // 머리줄 아래로 들여 — 이름 기둥과 본문 기둥이 갈리면 눈이 대화의 차례를 탄다.
        border = JBUI.Borders.empty(3, 14, 0, 0)
    })

    /** 곁말 — 생각의 첫 줄, keep, 실패의 첫 줄, 창이 하는 말. 앞에 안 나선다. */
    fun aside(text: String, hue: Color = faint): JComponent = measured(asideArea(text, hue))

    /**
     * 기계의 출력 원문이 입는 곁말 옷 — **산문 상한은 안 입는다**. 옮겨 적을 것(도구 결과·
     * 실패 원문, §3.3)에 60ch 를 걸면 스택트레이스·경로가 ~430px 에서 접힌다(리뷰 F2).
     * 곁말 옷이 고정폭이 아닌 것은 기왕의 모습 그대로 두고(승격은 별건), 여기선 상한만 벗는다.
     */
    fun outAside(text: String, hue: Color = faint): JComponent = asideArea(text, hue)

    private fun asideArea(text: String, hue: Color): javax.swing.JTextArea = javax.swing.JTextArea(text).apply {
        isEditable = false
        isOpaque = false
        lineWrap = true
        wrapStyleWord = true
        font = JBFont.small().deriveFont(Font.ITALIC)
        foreground = hue
        border = JBUI.Borders.empty(2, 14, 0, 0)
    }

    /** 펼친 도구 행의 본문 — 인자·출력 원문. 옮겨 적을 것이라 고정폭이다(§3.3). */
    fun code(text: String): JComponent = javax.swing.JTextArea(text).apply {
        isEditable = false
        isOpaque = false
        lineWrap = true
        wrapStyleWord = true
        font = mono().deriveFont(JBFont.small().size.toFloat())
        foreground = muted
        border = JBUI.Borders.empty(2, 14, 0, 0)
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
