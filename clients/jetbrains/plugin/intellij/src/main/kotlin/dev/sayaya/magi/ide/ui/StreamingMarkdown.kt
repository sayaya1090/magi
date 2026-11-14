package dev.sayaya.magi.ide.ui

import org.intellij.markdown.flavours.gfm.GFMFlavourDescriptor
import org.intellij.markdown.parser.MarkdownParser
import java.awt.BorderLayout
import java.awt.Dimension
import javax.swing.BoxLayout
import javax.swing.JEditorPane
import javax.swing.JPanel

/** Keeps completed Markdown blocks attached while only the current block changes. No browser reloads. */
internal class StreamingMarkdown(text: String) : JPanel() {
    private var blocks = emptyList<String>()
    init {
        isOpaque = false
        layout = BoxLayout(this, BoxLayout.Y_AXIS)
        update(text)
    }

    fun update(text: String) {
        val next = MarkdownParser(GFMFlavourDescriptor()).buildMarkdownTreeFromString(text).children
            .map { text.substring(it.startOffset, it.endOffset) }.filter { it.isNotBlank() }
        next.forEachIndexed { index, source ->
            if (blocks.getOrNull(index) == source) return@forEachIndexed
            val body = view(source)
            if (index < componentCount) {
                val held = getComponent(index) as JPanel
                val now = held.getComponent(0)
                // Preserve both the block component and its selection when the kind is unchanged.
                if (now is JEditorPane && body is JEditorPane) now.text = body.text
                else if (!(isPending(now) && isPending(body))) { held.removeAll(); held.add(body, BorderLayout.CENTER) }
            } else {
                add(object : JPanel(BorderLayout()) {
                    init { isOpaque = false; alignmentX = LEFT_ALIGNMENT; add(body, BorderLayout.CENTER) }
                    override fun getMaximumSize() = Dimension(Int.MAX_VALUE, preferredSize.height)
                })
            }
        }
        while (componentCount > next.size) remove(componentCount - 1)
        blocks = next
        revalidate()
    }

    /**
     * A Mermaid fence is not shown as its source while the answer streams: a box holds its place
     * and the finished answer draws the diagram (RichAnswer). Raw diagram text mid-stream read as
     * something gone wrong.
     */
    private fun view(source: String): javax.swing.JComponent =
        if (Regex("^```\\s*mermaid\\b", RegexOption.IGNORE_CASE).containsMatchIn(source.trimStart())) pending()
        else Look.rich(source)

    private fun isPending(c: java.awt.Component) = (c as? javax.swing.JComponent)?.getClientProperty(PENDING) == true

    private fun pending(): javax.swing.JComponent = javax.swing.JLabel("\u25C7 " + MagiBundle.msg("chat.diagram.pending")).apply {
        putClientProperty(PENDING, true)
        foreground = Look.faint
        border = javax.swing.BorderFactory.createCompoundBorder(
            com.intellij.util.ui.JBUI.Borders.empty(3, 14, 3, 0),
            javax.swing.BorderFactory.createCompoundBorder(
                javax.swing.BorderFactory.createDashedBorder(Look.faint, 1f, 4f, 3f, true),
                com.intellij.util.ui.JBUI.Borders.empty(16, 12)))
    }

    private companion object { const val PENDING = "magi.diagram.pending" }
}
