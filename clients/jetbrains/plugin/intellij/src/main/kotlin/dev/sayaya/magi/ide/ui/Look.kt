package dev.sayaya.magi.ide.ui

import com.intellij.openapi.editor.colors.EditorColorsManager
import com.intellij.openapi.editor.colors.EditorFontType
import com.intellij.ui.JBColor
import com.intellij.ui.components.JBLabel
import dev.sayaya.magi.ide.usecase.Markup
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

    /**
     * 드롭다운의 폭을 **항목에서 떼어낸다.**
     *
     * 스윙 콤보는 선호 폭을 가장 긴 항목에서 뽑는다. 대화 제목처럼 긴 값이 들어오면 그 한
     * 줄이 판 전체를 벌리고, 우측 독이 쓸데없이 넓어진다(사용자 실측: "걔 때문에 패널 폭이
     * 넓어진다"). 그래서 견본 값을 하나 박아 폭을 고정하고, 긴 값은 잘라 그리되 **툴팁에
     * 원문을 준다** — 줄여 보이는 것과 감추는 것은 다르다.
     */
    /**
     * 말풍선 하나를 옮겨 적는 단추. **늘 보인다** — 마우스를 얹어야 나타나는 단추는 있는 줄을
     * 모르면 영영 안 쓰인다(이 집이 스트라이프 아이콘에서 이미 겪었다). 대신 흐리게 둬서
     * 글을 읽는 눈을 안 뺏는다.
     */
    fun copyButton(tip: String, onClick: () -> Unit): JComponent =
        JBLabel(com.intellij.icons.AllIcons.Actions.Copy).apply {
            toolTipText = tip
            border = JBUI.Borders.empty(2, 6, 0, 0)
            alignmentY = 0f
            cursor = java.awt.Cursor.getPredefinedCursor(java.awt.Cursor.HAND_CURSOR)
            addMouseListener(object : java.awt.event.MouseAdapter() {
                override fun mouseClicked(e: java.awt.event.MouseEvent) = onClick()
            })
        }

    /**
     * 고른 칸의 바탕. **플랫폼 목록의 선택색을 그대로 쓴다** — 우리가 색을 하나 더 정하면
     * 테마를 바꾼 날 이 칸만 남의 색으로 남는다. 포커스 없는 쪽(false)을 쓰는 이유는 전사에서
     * 고른 것은 「지금 키보드가 있는 자리」가 아니라 **표시**라서다.
     */
    val selection: Color get() = com.intellij.util.ui.UIUtil.getListSelectionBackground(false)

    /** 글자 수를 픽셀 상한으로 — 폰트에서 **매번 다시 잰다**(테마·글꼴이 바뀌면 같이 바뀐다). */
    private fun cap(c: java.awt.Component, chars: Int) =
        c.getFontMetrics(c.font).charWidth('M') * chars + JBUI.scale(8)

    /**
     * **폭을 요구하지 않는 라벨.**
     *
     * [note] 가 설명문에서 막은 것과 같은 기전이 **값을 적는 라벨**에서 무방비였다. 스윙 라벨의
     * 최소 폭은 글자 전체를 한 줄로 편 길이라, 긴 값 하나가 판 전체의 바닥을 올린다 — 그리고
     * 그 바닥은 창을 다시 좁힐 때까지 안 내려간다.
     *
     * 실측(2026-09-01, 설정 판): 쉴 때 **616px**. 여기에 데몬이 준 에러 한 줄이 앉으니
     * **2295px** 가 됐다(그 라벨 하나가 1256px). 사람이 「설정창 크기가 안 줄어든다」로 잡은
     * 그 자리다. 콤보를 먼저 고쳤는데 그건 둘째 바닥이었다.
     *
     * [note] 처럼 접지 않고 **자르는** 이유는 이것이 값이기 때문이다 — 상태 한 줄이 세 줄로
     * 늘면 아래가 다 밀린다. 스윙이 잘린 라벨에 「…」를 붙여 주므로, 잘렸다는 것은 보인다.
     * 그리고 **원문은 툴팁으로 준다** — 줄여 보이는 것과 감추는 것은 다르다([narrow] 와 같은 손).
     */
    fun wide(chars: Int = 36): JBLabel = object : JBLabel(" ") {
        override fun getMinimumSize(): Dimension {
            val d = super.getMinimumSize()
            return Dimension(minOf(d.width, cap(this, chars)), d.height)
        }
        override fun setText(text: String?) {
            super.setText(text)
            // HTML 도 그대로 준다 — 툴팁은 HTML 을 그린다.
            toolTipText = text?.takeIf { it.isNotBlank() && it != " " }
        }
    }.apply { putClientProperty(DYN, true) }

    /**
     * 「데몬이 글을 앉히는 칸」 표. 시험이 이 표만 보고 긴 글을 먹인다 — 판을 훑어 **빈 라벨
     * 전부**에 먹이면 정적인 자리(빈 이름 칸)까지 물들어 재는 값이 1003px 만큼 부풀었다.
     * 계측이 자기 부작용을 재고 있으면 고친 뒤에도 숫자가 안 내려간다.
     */
    const val DYN = "magi.dynamicText"

    /**
     * **접히는 메시지 칸** — [wide] 의 짝.
     *
     * 둘 다 폭을 안 요구하지만 **자르는 것과 접는 것**은 다른 자리에 쓴다. 값(무엇을 하는 중,
     * 권한, 대화 id)은 자른다 — 한 줄이 세 줄로 늘면 아래가 다 밀린다. 메시지(에러, 워크스페이스
     * 밖 경로 목록)는 접는다 — 자르면 정작 읽어야 할 사유가 「…」 뒤로 숨는다. 404 한 줄을
     * 36자로 자르면 남는 것은 `llm: not found — check -model and -ba…` 뿐이다.
     *
     * 접어도 되는 이유는 이 둘이 **아래를 안 미는 자리**라서다: 사유는 판의 맨 끝이고, 밖 경로는
     * 원래도 여러 줄이었다.
     */
    fun flow(hue: Color = faint): javax.swing.JTextArea =
        javax.swing.JTextArea().apply {
            isEditable = false
            isOpaque = false
            lineWrap = true
            wrapStyleWord = true
            border = null
            foreground = hue
            font = JBUI.Fonts.label()
            // 최소 폭을 글자에서 떼어 낸다 — 접히는 칸이라도 한 줄 폭을 요구하면 소용없다.
            minimumSize = Dimension(JBUI.scale(80), 0)
            putClientProperty(DYN, true)
        }

    /**
     * **폭을 요구하지 않는 체크박스.** 라벨과 같은 기전이고, 이 집의 체크박스는 글자가 길다
     * (「Start the magi engine for this project when it is not running」). 실측: 407px.
     */
    fun check(text: String, chars: Int = 44): javax.swing.JCheckBox =
        object : javax.swing.JCheckBox(text) {
            override fun getMinimumSize(): Dimension {
                val d = super.getMinimumSize()
                return Dimension(minOf(d.width, cap(this, chars)), d.height)
            }
        }.apply { toolTipText = text }

    /**
     * **폭을 요구하지 않는 콤보를 만든다.**
     *
     * [narrow] 만으로는 모자랐다. 프로토타입은 콤보가 **그리는** 폭을 정하지만, 판이 못 좁혀지게
     * 막는 것은 **최소 폭**이고 그쪽은 안 잡힌다. 실측(2026-09-01, 긴 모델 이름 하나를 넣고
     * 최소 폭을 다시 잼):
     *
     * - 편집 불가·프로토타입 없음(권한): **95 → 451**
     * - 편집 가능·프로토타입 있음(모델): **300 → 428** — 프로토타입이 있는데도 커진다.
     *   편집 가능한 콤보의 최소 폭은 항목이 아니라 **편집칸**에서 나온다.
     *
     * 그래서 사람이 본 것이 「한번 커진 드롭다운은 다시 작아지지 않는다」였다. `fill=HORIZONTAL`
     * 이라 넓어지는 것은 늘 되지만, 되돌아올 바닥이 같이 올라가 있었다.
     *
     * 상한은 글자 수로 정하고 **폰트에서 매번 다시 잰다** — 값을 한 번 박아 두면 테마나 IDE
     * 글꼴이 바뀐 날 그 상한만 옛 글꼴로 남는다.
     *
     * [prototype] 은 **평소에도** 그 폭을 요구할지다. 항목이 으레 긴 것(모델 이름, 대화 제목)은
     * 켠다 — 그래야 목록이 늦게 도착해도 판이 안 흔들린다. 항목이 짧고 **드물게만** 긴 것(권한
     * 토큰: 아는 넷은 다 짧고, 데몬이 모르는 값을 줄 때만 길어진다)은 끈다. 켜면 쉬는 폭까지
     * 프로토타입만큼 벌어져서, 상한을 씌우려다 도리어 넓히게 된다(실측: 95 → 164).
     */
    fun <T> narrowCombo(chars: Int = 18, prototype: Boolean = true): javax.swing.JComboBox<T> =
        object : javax.swing.JComboBox<T>() {
            override fun getMinimumSize(): Dimension {
                val d = super.getMinimumSize()
                val cap = getFontMetrics(font).charWidth('M') * chars + JBUI.scale(32)
                return Dimension(minOf(d.width, cap), d.height)
            }
        }.also { if (prototype) narrow(it, chars) }

    @Suppress("UNCHECKED_CAST")
    fun <T> narrow(combo: javax.swing.JComboBox<T>, chars: Int = 18) {
        combo.prototypeDisplayValue = "M".repeat(chars) as T
        val base = combo.renderer
        combo.renderer = javax.swing.ListCellRenderer<Any?> { list, value, index, sel, focus ->
            val c = (base as javax.swing.ListCellRenderer<Any?>)
                .getListCellRendererComponent(list, value, index, sel, focus)
            val full = value?.toString().orEmpty()
            if (c is javax.swing.JLabel) {
                c.toolTipText = full.ifBlank { null }
                if (full.length > chars + 2) c.text = full.take(chars) + "…"
            }
            c
        }
    }

    /**
     * 설명문 라벨 — **폭을 요구하지 않는다.**
     *
     * [narrow] 가 드롭다운에서 막은 것과 같은 기전이 라벨에서 무방비였다: 스윙 라벨의 선호
     * 폭은 글자 전체를 한 줄로 편 길이라, 긴 설명 한 줄이 설정 판을 통째로 벌린다. 폭 상한을
     * 박는 대신(그건 이 집에서 반려된 손이다) **접히게** 만든다 — 그러면 폭을 정하는 것이
     * 글자가 아니라 판이 된다.
     */
    fun note(text: String, hue: Color = faint): JComponent =
        javax.swing.JTextArea(text).apply {
            isEditable = false
            isOpaque = false
            lineWrap = true
            wrapStyleWord = true
            // **폭을 여기서 정한다.** 접히게만 만들었더니 판이 좁혀질 수는 있는데 처음 열릴 때의
            // 폭은 그대로 글자 길이였다 — 사용자 실측: "가로로 쭉 늘어남". 접힘은 판이 이미
            // 좁아졌을 때만 발동하니, 접히는 것과 좁게 서는 것은 다른 일이다.
            //
            // 스윙 라벨의 HTML 접힘 대신 텍스트 영역을 쓴다: `columns` 가 선호 폭을 **글자 수로**
            // 못박고 높이는 줄 수로 자란다 — [narrow] 가 콤보에 쓰는 그 손이고, 이 집이 이미
            // 받아들인 규칙이다("견본 값으로 폭 고정"). 덤으로 마크업이 아예 없으니 남의 글자가
            // 태그로 먹힐 자리도 사라진다.
            columns = 46
            font = JBFont.small().deriveFont(Font.ITALIC)
            foreground = hue
        }

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
     * 본문. 접히고(줄 단위), 고르지 않고, **UI 글꼴**을 쓴다. 폭은 판을 따른다 —
     * M3 의 40–60자 상한을 입혀 봤다가 하루 만에 걷었다(사용자 실측: "하단 슬롯이라 칸이
     * 넓은데 중간에 지멋대로 개행함"). 집 규칙 「새 화면에 max-width 금지」가 이 자리에선
     * M3 의 measure 예외를 이긴다 — 전사는 문서가 아니라 대화고, 대화는 칸을 쓴다.
     *
     * §3.3(기계의 말은 고정폭)을 여기서 **일부러 어긴다**(§6a: 어긴 것은 적는다). 행 구조를
     * 컴포넌트로 바꾸고도 화면이 "예전 로그와 똑같다"고 읽힌 실측이 있었고, 남은 원인이
     * 이것이었다 — 한국어 산문이 고정폭 한 색이면 구조를 어떻게 짜도 덤프로 보인다. 고정폭이
     * 증거의 옷인 것은 **옮겨 적을 것**(도구 이름·인자·경로)의 이야기라 그쪽([toolHead],
     * [code])에 남긴다. 본문은 사람이 읽는 글이다.
     */
    fun prose(text: String): JComponent = javax.swing.JTextArea(text).apply {
        isEditable = false
        isOpaque = false
        lineWrap = true
        wrapStyleWord = true
        font = JBFont.regular()
        foreground = body
        // 머리줄 아래로 들여 — 이름 기둥과 본문 기둥이 갈리면 눈이 대화의 차례를 탄다.
        border = JBUI.Borders.empty(3, 14, 0, 0)
    }

    /** 곁말 — 생각의 첫 줄, keep, 창이 하는 말. 앞에 안 나선다. */
    fun aside(text: String, hue: Color = faint): JComponent = asideArea(text, hue)

    private fun asideArea(text: String, hue: Color): javax.swing.JTextArea = javax.swing.JTextArea(text).apply {
        isEditable = false
        isOpaque = false
        lineWrap = true
        wrapStyleWord = true
        font = JBFont.small().deriveFont(Font.ITALIC)
        foreground = hue
        border = JBUI.Borders.empty(2, 14, 0, 0)
    }

    /**
     * 모델 답의 리치 본문 — 마크다운 **원문**을 받아 [Markup.markdown] 이 편 것을 IDE
     * 글꼴로 그린다. 편 HTML 을 받지 않고 원문을 받는 이유가 있다: 라벨에 남의 글자를 잇는
     * 자리는 거르는 함수를 거쳐야 하고(소스 글자 시험이 이 규칙을 잰다), 거르기를 콜사이트로
     * 올리면 새 콜사이트가 생기는 날 그물 밖으로 샌다. 원문은 사실로 남고 이것은 붓이다 —
     * 제대로 된 렌더(머메이드까지)는 행의 「md ↗」 가 IDE 마크다운 에디터로 연다.
     */
    fun rich(md: String): JComponent = javax.swing.JEditorPane(
        "text/html", "<html><body>" + Markup.markdown(md) + "</body></html>",
    ).apply {
        isEditable = false
        isOpaque = false
        putClientProperty(javax.swing.JEditorPane.HONOR_DISPLAY_PROPERTIES, true)
        font = JBFont.regular()
        foreground = body
        border = JBUI.Borders.empty(3, 14, 0, 0)
    }

    /**
     * 펼친 도구 행의 본문 — 인자·출력 원문. 옮겨 적을 것이라 고정폭이고(§3.3) **산문 상한도
     * 안 입는다**(60ch 에서 접힌 스택트레이스·경로는 다친 증거다). 실패 원문은 [error] 색을
     * 얹는다 — 한동안 출력이 이탤릭 곁말 옷을 입어 「고정폭이다」 주석이 거짓말을 하고
     * 있었다(리뷰 2회 적발).
     */
    fun code(text: String, hue: Color = muted): JComponent = javax.swing.JTextArea(text).apply {
        isEditable = false
        isOpaque = false
        lineWrap = true
        wrapStyleWord = true
        font = mono().deriveFont(JBFont.small().size.toFloat())
        foreground = hue
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
