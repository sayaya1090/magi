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
 * 플러그인 UI 테마 및 스타일링 유틸리티. 콘솔 디자인 시스템(`docs/UI.md` §3)을 IDE 환경에 맞추어 통합합니다.
 *
 * ### IDE 테마 통합 원칙 (`docs/UI.md` §6a)
 *
 * **배경은 IDE 플랫폼 테마를 따르고, 텍스트 및 시맨틱 강조는 전용 팔레트를 따릅니다.**
 * 콘솔과 달리 IDE 플러그인에서는 패널 배경색을 커스텀 팔레트로 강제할 경우 사용자가 설정한 에디터 테마와 충돌하여
 * §5의 첫 번째 원칙("IDE 고유 환경과 겹치거나 이질적인 UI를 만들지 않는다")을 위배하게 됩니다.
 * Run, Terminal 등 인접한 플랫폼 도구 창들과 동일한 배경 톤을 유지하여 자연스러운 통합감을 제공합니다.
 *
 * 반면 의미론적(Semantic) 색상은 일관성을 유지합니다:
 * 실행 에러의 적색, 카운슬 3인의 심의 좌석 색상, 탐색 가능한 링크(청록)는 웹 콘솔 및 CLI와 동일한 시각적 약속을 공유해야 하므로
 * [Palette]의 기준 색상을 공유합니다(`PaletteTest`로 정합성 검증).
 *
 * 따라서 이 모듈에는 인위적인 배경색 지정 함수를 두지 않고 IDE 테마에 위임합니다.
 */
internal object Look {

    private fun of(ink: Palette.Ink) = JBColor(Color.decode(ink.light), Color.decode(ink.dark))

    /**
     * 서브에이전트 생성 출처(origin)를 사용자 친화적 텍스트로 변환합니다. 미지의 식별자는 원문을 유지합니다.
     * 회의 세션의 경우 발화 룸(meeting)과 서기 룸(minutes)을 명확히 구분하여 표기합니다.
     */
    fun originWord(origin: String): String = when (origin) {
        "meeting" -> MagiBundle.msg("plan.kid.meeting")
        "minutes" -> MagiBundle.msg("plan.kid.minutes")
        else -> origin
    }

    /** 사용자 입력 또는 승인을 대기 중인 상태를 강조하는 기본 색상. */
    val primary = of(Palette.primary)

    /** 클릭 가능한 파일 경로 및 라인 링크 강조 색상. */
    val accent = of(Palette.accent)

    /**
     * 일반 본문 텍스트 색상.
     * Material Design 3의 On-Surface 대비 규칙을 준수하기 위해 IDE 플랫폼 테마 롤(`Label.foreground`)을 우선 사용하고 전용 팔레트를 폴백으로 지정합니다.
     */
    val body = JBColor.namedColor("Label.foreground", of(Palette.onSurface))

    /** 보조 안내 텍스트 (시각, 인덱스, 시스템 메시지 등). */
    val faint = JBColor.namedColor("Label.infoForeground", of(Palette.onSurfaceVariant))

    /** 비활성 또는 배경 수준의 약한 텍스트. */
    val muted = JBColor.namedColor("Component.infoForeground", of(Palette.muted))

    /** 영역 구분선 색상. */
    val edge = JBColor.namedColor("Separator.separatorColor", of(Palette.outlineVariant))

    val error = of(Palette.error)
    val warn = of(Palette.warn)
    val success = of(Palette.success)

    /**
     * 카운슬 3인의 고유 심의 좌석 색상을 반환합니다.
     * 멜키오르, 발타자르, 카스퍼 3인에게만 고유 색상을 부여하며, 카스퍼는 거절/에러 붉은색과의 혼동을 방지하기 위해 보라색 톤을 사용합니다(`console.css`).
     */
    fun seat(who: String?): JBColor? = when (who?.lowercase()) {
        "melchior" -> of(Palette.melchior)
        "balthasar" -> of(Palette.balthasar)
        "casper" -> of(Palette.casper)
        else -> null
    }

    /**
     * 코드 및 트랜스크립트 출력용 모노스페이스 글꼴을 반환합니다(`docs/UI.md` §3.3).
     * 사용자가 에디터에 설정한 글꼴 구성을 동적으로 반영합니다.
     */
    fun mono(): Font = EditorColorsManager.getInstance().globalScheme.getFont(EditorFontType.PLAIN)

    /** 섹션 헤더 라벨(`docs/UI.md` §3.1a 도랑 표기). */
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

    /** 답변 대기 중인 항목 좌측에 표시하는 인디케이터 테두리. 웹 콘솔의 `.row.pending .txt` 스타일과 동일하게 좌측 2px primary 테두리를 부여한다. */
    fun pending(): javax.swing.border.Border = BorderFactory.createCompoundBorder(
        BorderFactory.createMatteBorder(0, 2, 0, 0, primary), JBUI.Borders.empty(6, 10)
    )

    /** 구역 분할을 위한 1px 구분선 컴포넌트. */
    fun rule(): JComponent = JBPanel<JBPanel<*>>().apply {
        background = edge
        preferredSize = Dimension(1, 1)
        maximumSize = Dimension(Int.MAX_VALUE, 1)
        minimumSize = Dimension(1, 1)
    }

    // ── 트랜스크립트 행 렌더링 컴포넌트 팩토리.
    // 출력 내용은 MagiToolWindow.renderRow에서 결정하며, 본 객체는 행 단위 레이아웃과 서식을 담당한다.
    // 단일 텍스트 패널에 모든 내용을 집적할 경우 발생하는 시인성 저하(로그 덤프화)를 방지하기 위해
    // 개별 행 컴포넌트 분리 및 여백/색상/글꼴 규칙을 적용한다.

    /**
     * 말풍선 내용 복사 버튼.
     * 마우스 호버 시에만 노출할 경우 발견 가능성(discoverability)이 저하되므로 항상 노출하되,
     * 본문 가독성을 방해하지 않도록 절제된 스타일을 적용한다.
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
     * 선택 항목 배경색.
     * 테마 전환 시 색상 불일치를 방지하기 위해 플랫폼 UI의 목록 선택 배경색(`UIUtil.getListSelectionBackground(false)`)을 직접 참조한다.
     * 트랜스크립트 선택은 입력 포커스가 아닌 상태 표시 목적이므로 비포커스(false) 상태의 배경색을 사용한다.
     */
    val selection: Color get() = com.intellij.util.ui.UIUtil.getListSelectionBackground(false)

    /** 글자 수 기준 픽셀 상한 계산. 테마 및 글꼴 변경에 동적으로 대응하기 위해 현재 폰트 메트릭(`charWidth('M')`)을 기준으로 매번 계산한다. */
    private fun cap(c: java.awt.Component, chars: Int) =
        c.getFontMetrics(c.font).charWidth('M') * chars + JBUI.scale(8)

    /**
     * 레이아웃 폭을 강제 확장하지 않는 텍스트 라벨.
     *
     * Swing JLabel의 기본 최소 폭은 텍스트 전체를 한 줄로 펼친 길이이므로, 긴 텍스트가 표시될 경우 패널 전체의 최소 폭이 불필요하게 확장된다.
     *
     * 실측(2026-09-01, 설정 패널): 유휴 시 616px이었으나 데몬 오류 메시지(1256px) 표시 시 2295px까지 확장되어
     * 사용자가 창 크기를 줄일 수 없는 결함이 확인되었다.
     *
     * 상태나 식별자 등 단일 행 값은 여러 줄로 줄바꿈될 경우 하단 레이아웃을 밀어내므로 텍스트를 절단(ellipsis) 처리하고,
     * 전문은 툴팁으로 제공한다.
     */
    fun wide(chars: Int = 36): JBLabel = object : JBLabel(" ") {
        @Suppress("UNUSED_PARAMETER")
        override fun getMinimumSize(): Dimension {
            val d = super.getMinimumSize()
            return Dimension(minOf(d.width, FLOOR), d.height)
        }
        override fun setText(text: String?) {
            super.setText(text)
            // 툴팁에는 원본 텍스트(HTML 포함)를 전달한다.
            toolTipText = text?.takeIf { it.isNotBlank() && it != " " }
        }
    }.apply { putClientProperty(DYN, true) }

    /**
     * 데몬 동적 텍스트 주입 대상 식별 프로퍼티 키.
     * UI 스트레스 테스트 시 정적 레이블(빈 라벨 등)을 제외하고 동적 변경 필드만을 정확히 식별하기 위해 사용한다.
     */
    const val DYN = "magi.dynamicText"

    /**
     * 자동 줄바꿈을 지원하는 가변 메시지 영역 ([wide]와 상호 보완).
     *
     * 상태값이나 식별자는 절단 처리([wide])하지만, 에러 메시지나 작업 영역 외부 경로 목록과 같은 메시지는
     * 생략 없이 전체 내용을 전달해야 하므로 자동 줄바꿈을 적용한다.
     * 메시지 영역은 주로 패널 최하단이나 목록 말단에 위치하므로 줄바꿈으로 인한 높이 확장이 전체 레이아웃을 교란하지 않는다.
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
            // 패널 폭 축소를 차단하지 않도록 최소 폭을 글자 수와 분리하여 최소치로 지정한다.
            minimumSize = Dimension(JBUI.scale(80), 0)
            putClientProperty(DYN, true)
        }

    /**
     * 긴 레이블로 인한 레이아웃 확장을 방지하는 체크박스.
     * 최소 폭을 [FLOOR]로 제한하며, 전문은 툴팁으로 제공한다 (실측: 407px 길이 레이블 등 대응).
     */
    fun check(text: String, chars: Int = 44): javax.swing.JCheckBox =
        object : javax.swing.JCheckBox(text) {
            override fun getMinimumSize(): Dimension {
                val d = super.getMinimumSize()
                return Dimension(minOf(d.width, FLOOR), d.height)
            }
        }.apply { toolTipText = text }

    /**
     * 레이아웃 폭을 강제 확장하지 않는 콤보박스를 생성한다.
     *
     * 콤보박스의 프로토타입 값은 렌더링 기준 폭을 정하지만, Swing의 기본 getMinimumSize()는 항목 길이에 따라 확장될 수 있다.
     * 실측(2026-09-01, 긴 모델 식별자 설정 시 최소 폭 재측정):
     * - 편집 불가/프로토타입 없음(권한): 95px → 451px
     * - 편집 가능/프로토타입 있음(모델): 300px → 428px (편집 가능 콤보박스의 최소 폭은 에디터 컴포넌트 기준)
     *
     * 최소 폭이 비대해지면 창을 다시 좁힐 수 없는 현상이 발생하므로 최소 폭을 [FLOOR]로 제한한다.
     *
     * [prototype] 설정은 유휴 상태에서도 지정 글자 폭을 확보할지 여부를 결정한다. 모델명이나 세션 제목처럼 일반적으로 긴 항목은
     * 목록 로딩 지연 시 UI 흔들림을 방지하기 위해 true로 두고, 권한 토큰처럼 평소에는 짧은 항목은 기본 폭 확장(95px → 164px)을 막기 위해 false로 둔다.
     */
    fun <T> narrowCombo(chars: Int = 18, prototype: Boolean = true): javax.swing.JComboBox<T> =
        object : javax.swing.JComboBox<T>() {
            override fun getMinimumSize(): Dimension {
                val d = super.getMinimumSize()
                return Dimension(minOf(d.width, FLOOR), d.height)
            }
        }.also { if (prototype) narrow(it, chars) }

    /**
     * 컴포넌트 축소 한계 기본 최소 폭 (90px).
     *
     * 선호 폭(chars 기준)과 최소 폭(FLOOR)을 분리하여, 사용자가 패널을 좁힐 때 콤보박스 및 텍스트 필드가
     * 레이아웃 축소를 차단하지 않고 유연하게 축소될 수 있도록 보장한다 (2026-09-01 실측 피드백 반영).
     */
    private val FLOOR: Int get() = JBUI.scale(90)

    /**
     * 긴 텍스트 입력 시 패널 최소 폭이 확장되는 현상을 방지하는 텍스트 입력 필드.
     * Swing JTextField의 columns가 0일 경우 입력 텍스트 전체 길이가 최소 폭이 되는 특성을 방어한다.
     */
    fun narrowField(): com.intellij.ui.components.JBTextField =
        object : com.intellij.ui.components.JBTextField() {
            override fun getMinimumSize(): Dimension {
                val d = super.getMinimumSize()
                return Dimension(minOf(d.width, FLOOR), d.height)
            }
        }

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
     * 자동 줄바꿈 안내문 텍스트 영역.
     *
     * JLabel의 기본 가로 확장 문제를 방지하기 위해 JTextArea의 자동 줄바꿈(lineWrap)을 활용한다.
     * columns=46을 지정하여 초기 선호 폭을 글자 수 기준으로 제한하고, 세로 방향으로 자연스럽게 확장되도록 한다.
     */
    fun note(text: String, hue: Color = faint): JComponent =
        javax.swing.JTextArea(text).apply {
            minimumSize = Dimension(FLOOR, 0)
            isEditable = false
            isOpaque = false
            lineWrap = true
            wrapStyleWord = true
            // columns 지정을 통해 초기 렌더링 선호 폭을 글자 수 기준으로 고정하고 패널 가로 확장을 방지한다.
            columns = 46
            font = JBFont.small().deriveFont(Font.ITALIC)
            foreground = hue
        }

    /** 트랜스크립트 행 배치용 수직 패널. 가로 스크롤 발생을 방지하고 본문 자동 줄바꿈을 유도하기 위해 Scrollable.tracksViewportWidth를 true로 설정한다. */
    fun column(): JBPanel<JBPanel<*>> =
        object : JBPanel<JBPanel<*>>(VerticalFlowLayout(VerticalFlowLayout.TOP, 0, 0, true, false)),
            javax.swing.Scrollable {
            override fun getPreferredScrollableViewportSize(): Dimension = preferredSize
            override fun getScrollableUnitIncrement(v: Rectangle, o: Int, d: Int) = 16
            override fun getScrollableBlockIncrement(v: Rectangle, o: Int, d: Int) = v.height
            override fun getScrollableTracksViewportWidth() = true
            override fun getScrollableTracksViewportHeight() = false
        }

    /** 트랜스크립트 행 기본 여백 (상하 8px, 좌우 12px). */
    fun row(): javax.swing.border.Border = JBUI.Borders.empty(8, 12)

    /** 답변 대기 중인 트랜스크립트 행 테두리. 좌측에 [pending] 인디케이터를 포함한다. */
    fun pendingRow(): javax.swing.border.Border = BorderFactory.createCompoundBorder(
        BorderFactory.createMatteBorder(0, 2, 0, 0, primary), JBUI.Borders.empty(6, 10, 6, 12),
    )

    /** 메시지 발신 헤더 컴포넌트 (발신자명, 마크/배지 목록, 타임스탬프). */
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

    /** 도구 호출 헤더 컴포넌트 (도구명, 실행 상태 글리프, 인자 요약, 타임스탬프). */
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
     * 산문 본문 텍스트 컴포넌트.
     *
     * UI 기본 글꼴을 사용하며, 패널 폭에 맞춰 자연스럽게 줄바꿈된다.
     * M3의 40~60자 너비 제한은 하단 독 슬롯 특성상 임의 개행으로 인한 가독성 저하를 유발하므로 적용하지 않는다.
     *
     * 또한 §3.3 규정의 고정폭 글꼴 원칙에 대해, 산문 본문 영역은 예외(§6a)로 일반 UI 글꼴을 채택한다.
     * 고정폭 글꼴은 도구 식별자·경로·인자 등 기술적 증거 영역([toolHead], [code])에 집중 적용하여 가독성을 최적화한다.
     */
    fun prose(text: String): JComponent = javax.swing.JTextArea(text).apply {
        isEditable = false
        isOpaque = false
        lineWrap = true
        wrapStyleWord = true
        font = JBFont.regular()
        foreground = body
        // 헤더 발신자명과의 시각적 구분을 위해 좌측 14px 들여쓰기 적용
        border = JBUI.Borders.empty(3, 14, 0, 0)
    }

    /** 보조 안내 텍스트 컴포넌트 (사고 과정 첫 줄, keep 알림, 시스템 안내 등 이탤릭 서식 적용). */
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
     * 마크다운 서식 본문 컴포넌트.
     * 원본 마크다운 텍스트를 전달받아 [Markup.markdown] 변환 후 HTML 에디터 패널로 렌더링한다.
     * HTML 파싱 단계에서 XSS 및 불필요한 태그 주입을 방어하기 위해 원본 텍스트 유효성 검증을 거친다.
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
     * 도구 호출 상세 본문 (인자 및 출력 원본 데이터).
     * 증거 데이터의 정확한 전달을 위해 고정폭 폰트(§3.3)를 적용하며, 임의의 너비 상한을 두지 않는다.
     * 실행 실패 시에는 [error] 색상을 적용하여 실패 맥락을 명확히 구분한다.
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

    /** 헤더 구분선과 거터 레이블이 포함된 섹션 패널 래퍼. */
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
