package dev.sayaya.magi.ide.ui

import com.intellij.icons.AllIcons
import com.intellij.ui.components.JBLabel
import com.intellij.util.ui.ImageUtil
import com.intellij.util.ui.StartupUiUtil
import java.awt.AlphaComposite
import java.awt.Color
import java.awt.Component
import java.awt.Graphics
import java.awt.image.BufferedImage
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.model.ProgressStage
import java.awt.Dimension
import java.awt.FlowLayout
import java.awt.Font
import javax.swing.Icon
import javax.swing.JPanel

/**
 * 진행 단계 칸: 도구가 알린 단계 목록을 한 줄로 보인다(시작 → 단계 … → 끝).
 *
 * 단계마다 [ProgressStage.id] 로 노드를 지킨다. 새 알림이 오면 그 노드의 글자·색·아이콘만 바꾸고,
 * 단계가 늘거나 순서가 바뀔 때만 줄을 다시 엮는다 — 칸 전체를 새로 만들지 않으므로 깜빡이지 않는다.
 * 무엇의 단계인지는 모른다: 이름(label), 종류(kind), 상태(state), 센 값만 그린다.
 *
 * 한 줄의 노드는 모두 같은 높이다(가장 높은 노드에 맞춘다) — 표시가 있는 노드와 없는 노드의 글자가
 * 한 세로 중심에 놓인다. 종류 표시는 그 단계의 글자색으로 칠한다 — 같은 상태의 두 단계는 같은 색이다.
 */
internal class ProgressStages(
    /** 종류가 고르는 표시. 시험이 잉크 있는 아이콘을 넣을 수 있게 열어 둔다. */
    private val icons: (String?) -> Icon? = { iconOf(it) },
) : JPanel(FlowLayout(FlowLayout.LEFT, JBUI.scale(4), 0)) {
    /** 줄 높이를 함께 쓰는 라벨. [row] 보다 작으면 [row] 로 선다. */
    private class Node(text: String = "") : JBLabel(text) {
        var row = 0
        fun natural(): Int = super.getPreferredSize().height
        override fun getPreferredSize(): Dimension = super.getPreferredSize().let { Dimension(it.width, maxOf(it.height, row)) }
    }

    private val start = Node(MagiBundle.msg("progress.start"))
    private val end = Node(MagiBundle.msg("progress.end"))
    private val toEnd = Node("→")
    private val nodes = LinkedHashMap<String, Node>()
    private val arrows = HashMap<String, Node>()

    init {
        isOpaque = false
        start.foreground = Look.faint
        end.foreground = Look.faint
        toEnd.foreground = Look.faint
        relink(emptyList())
    }

    fun update(stages: List<ProgressStage>) {
        val order = stages.map { it.id }
        val reorder = nodes.keys.toList() != order
        nodes.keys.filter { it !in order }.toList().forEach { nodes.remove(it); arrows.remove(it) }
        for (s in stages) {
            val n = nodes.getOrPut(s.id) { Node().apply { putClientProperty(ID, s.id) } }
            arrows.getOrPut(s.id) { Node("→").apply { foreground = Look.faint } }
            paint(n, s)
        }
        start.foreground = if (stages.any { it.state != "waiting" }) Look.primary else Look.faint
        end.foreground = if (stages.isNotEmpty() && stages.all { it.state == "done" }) Look.primary else Look.faint
        val all = listOf(start, toEnd, end) + nodes.values + arrows.values
        val row = all.maxOf { it.natural() }
        val grew = all.any { it.row != row }
        all.forEach { it.row = row }
        if (reorder) relink(order) else if (grew) revalidate()
        repaint()
    }

    private fun relink(order: List<String>) {
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
        val fg = when (s.state) {
            "running" -> Look.primary
            "failed" -> Look.error
            "done" -> Look.body
            else -> Look.faint
        }
        n.foreground = fg
        // 종류 표시는 글자와 같은 색으로 칠한다. 같은 종류·같은 색이면 다시 칠하지 않는다.
        val key = s.kind + "|" + Integer.toHexString(fg.rgb)
        if (n.getClientProperty(INK) != key) {
            n.icon = icons(s.kind)?.let { Inked(it, fg) }
            n.putClientProperty(INK, key)
        }
        val style = if (s.state == "running") Font.BOLD else Font.PLAIN
        if (n.font.style != style) n.font = n.font.deriveFont(style)
        n.putClientProperty(STATE, s.state)
        n.putClientProperty(KIND, s.kind)
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
        private const val INK = "magi.stage.ink"

        /** 종류가 고르는 아이콘 — 같은 종류는 늘 같은 그림이다. 종류가 없으면 아이콘도 없다. */
        fun iconOf(kind: String?): Icon? = when (kind) {
            "tool" -> AllIcons.General.Settings
            "mcp" -> AllIcons.Nodes.Plugin
            "web" -> AllIcons.General.Web
            "harness" -> AllIcons.Actions.Lightning
            "subagent" -> AllIcons.Actions.Find
            else -> null
        }
    }
}
