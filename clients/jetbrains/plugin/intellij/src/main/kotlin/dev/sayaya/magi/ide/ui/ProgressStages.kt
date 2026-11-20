package dev.sayaya.magi.ide.ui

import com.intellij.icons.AllIcons
import com.intellij.ui.components.JBLabel
import com.intellij.util.IconUtil
import com.intellij.util.ui.ImageUtil
import com.intellij.util.ui.JBUI
import com.intellij.util.ui.StartupUiUtil
import dev.sayaya.magi.ide.model.ProgressStage
import java.awt.AlphaComposite
import java.awt.Color
import java.awt.Component
import java.awt.Dimension
import java.awt.FlowLayout
import java.awt.Font
import java.awt.Graphics
import java.awt.image.BufferedImage
import javax.swing.Icon
import javax.swing.JPanel
import javax.swing.SwingConstants

/**
 * 진행 단계 칸: 도구가 알린 단계 목록을 한 줄로 보인다(시작 → 단계 … → 끝).
 *
 * 노드마다 큰 표시([ICON]px) 아래에 이름을 단다. 단계마다 [ProgressStage.id] 로 노드를 지켜, 새 알림이
 * 오면 그 노드의 글자·색·표시만 바꾸고 단계가 늘거나 순서가 바뀔 때만 줄을 다시 엮는다 — 칸 전체를 새로
 * 만들지 않으므로 깜빡이지 않는다. 무엇의 단계인지는 모른다: 이름, 종류, 상태, 센 값만 그린다.
 *
 * 시작과 끝도 단계와 같은 상태를 갖고 같은 색 규칙으로 칠한다: 시작은 실행 중이면 running, 끝나면 done;
 * 끝은 모두 끝나야 done, 실패가 있으면 failed, 그 전에는 waiting. 노드는 모두 같은 높이로 위에 붙어
 * 표시의 윗선과 이름 줄이 서로 맞고, 화살표는 표시의 세로 가운데에 선다. 표시는 그 노드의 글자색으로
 * 칠한다 — 같은 상태의 두 노드는 같은 색이다.
 */
internal class ProgressStages(
    /** 표시를 고른다: 종류(tool·mcp·web·harness·subagent), 종류 없는 단계는 "step", 그리고 "start"·"end". */
    private val icons: (String) -> Icon? = { iconOf(it) },
) : JPanel(FlowLayout(FlowLayout.LEFT, JBUI.scale(6), 0)) {
    /** 줄 높이를 함께 쓰는 라벨. [row] 보다 작으면 [row] 로 선다. */
    private class Node(role: String, text: String = "") : JBLabel(text) {
        var row = 0
        init {
            putClientProperty(ROLE, role)
            verticalAlignment = SwingConstants.TOP
            horizontalAlignment = SwingConstants.CENTER
            if (role != "arrow") {
                verticalTextPosition = SwingConstants.BOTTOM
                horizontalTextPosition = SwingConstants.CENTER
                iconTextGap = JBUI.scale(2)
            }
        }
        fun natural(): Int = super.getPreferredSize().height
        override fun getPreferredSize(): Dimension = super.getPreferredSize().let { Dimension(it.width, maxOf(it.height, row)) }
    }

    private val start = Node("start", MagiBundle.msg("progress.start"))
    private val end = Node("end", MagiBundle.msg("progress.end"))
    private val toEnd = arrow()
    private val nodes = LinkedHashMap<String, Node>()
    private val arrows = HashMap<String, Node>()
    /** The step order the line is laid out in now — what a new report is compared with, not the map's insertion order. */
    private var laidOut: List<String> = emptyList()

    init {
        isOpaque = false
        mark(start, "start", "waiting")
        mark(end, "end", "waiting")
        relink(emptyList())
    }

    private fun arrow(): Node = Node("arrow", "→").apply {
        foreground = Look.faint
        val lift = (JBUI.scale(ICON) - getFontMetrics(font).height) / 2
        border = JBUI.Borders.emptyTop(maxOf(0, lift))
    }

    fun update(stages: List<ProgressStage>) {
        val order = stages.map { it.id }
        val reorder = laidOut != order
        nodes.keys.filter { it !in order }.toList().forEach { nodes.remove(it); arrows.remove(it) }
        for (s in stages) {
            val n = nodes.getOrPut(s.id) { Node("step").apply { putClientProperty(ID, s.id) } }
            arrows.getOrPut(s.id) { arrow() }
            paint(n, s)
        }
        val live = stages.any { it.state == "running" || it.state == "waiting" }
        mark(start, "start", when {
            stages.isEmpty() || stages.all { it.state == "waiting" } -> "waiting"
            live -> "running"
            else -> "done"
        })
        mark(end, "end", when {
            stages.isEmpty() || live -> "waiting"
            stages.any { it.state == "failed" } -> "failed"
            else -> "done"
        })
        val all = listOf(start, toEnd, end) + nodes.values + arrows.values
        val row = all.maxOf { it.natural() }
        val grew = all.any { it.row != row }
        all.forEach { it.row = row }
        if (reorder) relink(order) else if (grew) revalidate()
        repaint()
    }

    private fun relink(order: List<String>) {
        laidOut = order
        removeAll()
        add(start)
        for (id in order) { add(arrows.getValue(id)); add(nodes.getValue(id)) }
        add(toEnd)
        add(end)
        revalidate()
    }

    private fun paint(n: Node, s: ProgressStage) {
        val text = buildString {
            append(s.label)
            if (s.total > 0) append(" ").append(s.done).append('/').append(s.total)
            if (s.running > 0) append(" ·").append(s.running)
        }
        if (n.text != text) n.text = text
        mark(n, s.kind ?: "step", s.state)
        n.putClientProperty(KIND, s.kind)
    }

    /** 노드를 [state] 의 색으로 칠하고, [key] 가 고르는 표시를 그 색으로 다시 칠한다(같으면 그대로 둔다). */
    private fun mark(n: Node, key: String, state: String) {
        val fg = ink(state)
        n.foreground = fg
        val inked = key + "|" + Integer.toHexString(fg.rgb)
        if (n.getClientProperty(INK) != inked) {
            n.icon = icons(key)?.let { Inked(sized(it), fg) }
            n.putClientProperty(INK, inked)
        }
        val style = if (state == "running") Font.BOLD else Font.PLAIN
        if (n.font.style != style) n.font = n.font.deriveFont(style)
        n.putClientProperty(STATE, state)
    }

    /**
     * 표시를 [ink] 한 색으로 칠한다: 원본의 모양(알파)만 남기고 그 자리를 이 색으로 채운다. 원본의 밝기를
     * 남기는 칠하기(IconUtil.colorize)는 어두운 IDE 아이콘을 글자보다 늘 어둡게 칠한다.
     */
    private class Inked(private val src: Icon, private val ink: Color) : Icon {
        override fun getIconWidth() = src.iconWidth
        override fun getIconHeight() = src.iconHeight
        override fun paintIcon(c: Component?, g: Graphics, x: Int, y: Int) {
            if (iconWidth <= 0 || iconHeight <= 0) return
            val img = ImageUtil.createImage(g, iconWidth, iconHeight, BufferedImage.TYPE_INT_ARGB)
            val ig = img.createGraphics()
            try {
                src.paintIcon(c, ig, 0, 0)
                ig.composite = AlphaComposite.SrcIn
                ig.color = ink
                ig.fillRect(0, 0, iconWidth, iconHeight)
            } finally {
                ig.dispose()
            }
            StartupUiUtil.drawImage(g, img, x, y, null)
        }
    }

    companion object {
        const val ID = "magi.stage.id"
        const val STATE = "magi.stage.state"
        const val KIND = "magi.stage.kind"
        const val ROLE = "magi.stage.role"
        private const val INK = "magi.stage.ink"

        /** 표시의 크기(px, 배율 전). */
        const val ICON = 32

        /** 상태 하나에 색 하나 — 시작·단계·끝이 같이 쓴다. */
        fun ink(state: String): Color = when (state) {
            "running" -> Look.primary
            "failed" -> Look.error
            "done" -> Look.body
            else -> Look.faint
        }

        private fun sized(i: Icon): Icon {
            val want = JBUI.scale(ICON)
            return if (i.iconWidth <= 0 || i.iconWidth == want) i else IconUtil.scale(i, null, want.toFloat() / i.iconWidth)
        }

        /** 표시를 고르는 기본: 같은 종류는 늘 같은 그림이다. */
        fun iconOf(key: String): Icon? = when (key) {
            "tool" -> AllIcons.General.Settings
            "mcp" -> AllIcons.Nodes.Plugin
            "web" -> AllIcons.General.Web
            "harness" -> AllIcons.Actions.Lightning
            "subagent" -> AllIcons.Actions.Find
            "start" -> AllIcons.Actions.Execute
            "end" -> AllIcons.Actions.Checked
            else -> AllIcons.Nodes.Tag
        }
    }
}
