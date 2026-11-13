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
            val body = Look.rich(source) as JEditorPane
            if (index < componentCount) {
                val held = getComponent(index) as JPanel
                // Preserve both the block component and its selection for unchanged blocks.
                (held.getComponent(0) as JEditorPane).text = body.text
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
}
