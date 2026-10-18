package dev.sayaya.magi.ide.ui

import com.intellij.ui.JBColor
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.usecase.Palette
import java.awt.BasicStroke
import java.awt.Color
import java.awt.Component
import java.awt.Dimension
import java.awt.Graphics
import java.awt.Graphics2D
import java.awt.RenderingHints
import java.awt.geom.Path2D
import javax.swing.Box
import javax.swing.BoxLayout
import javax.swing.Icon
import javax.swing.JButton
import javax.swing.JComponent
import javax.swing.JPanel
import javax.swing.KeyStroke
import javax.swing.plaf.basic.BasicButtonUI

/** Compact schedule actions: 40dp visual surfaces inside 48dp keyboard/click targets. */
internal object ScheduleActions {
    fun row(canAdd: Boolean, canReload: Boolean, add: () -> Unit, reload: () -> Unit): JComponent =
        JPanel().apply {
            isOpaque = false
            layout = BoxLayout(this, BoxLayout.X_AXIS)
            alignmentX = Component.LEFT_ALIGNMENT
            border = JBUI.Borders.emptyTop(8)
            if (canAdd) add(Action(MagiBundle.msg("plan.schedule.add"), tonal = true, plus = true).apply {
                toolTipText = text
                addActionListener { add() }
            })
            if (canAdd && canReload) add(Box.createHorizontalStrut(JBUI.scale(8)))
            if (canReload) add(Action("", tonal = false, plus = false).apply {
                toolTipText = MagiBundle.msg("plan.schedule.reload.tip")
                accessibleContext.accessibleName = MagiBundle.msg("plan.schedule.reload")
                addActionListener { reload() }
            })
            maximumSize = Dimension(preferredSize.width, preferredSize.height)
            minimumSize = Dimension(0, preferredSize.height)
        }

    private fun ink(value: Palette.Ink): Color =
        JBColor(Color.decode(value.light), Color.decode(value.dark))

    private class Action(text: String, private val tonal: Boolean, plus: Boolean) : JButton(text) {
        init {
            isOpaque = false
            isContentAreaFilled = false
            isBorderPainted = false
            isFocusPainted = false
            isRolloverEnabled = true
            font = JBUI.Fonts.label(14f)
            foreground = if (tonal) ink(Palette.onPrimaryContainer) else Look.body
            border = JBUI.Borders.empty(12, if (tonal) 24 else 15)
            iconTextGap = JBUI.scale(8)
            icon = Glyph(plus)
            inputMap.put(KeyStroke.getKeyStroke("ENTER"), "pressed")
            inputMap.put(KeyStroke.getKeyStroke("released ENTER"), "released")
        }

        override fun updateUI() = setUI(BasicButtonUI())

        override fun getPreferredSize(): Dimension =
            Dimension(if (text.isEmpty()) JBUI.scale(48) else super.getPreferredSize().width, JBUI.scale(48))

        override fun getMinimumSize(): Dimension = preferredSize

        override fun getMaximumSize(): Dimension = preferredSize

        override fun paintComponent(g: Graphics) {
            val canvas = g.create() as Graphics2D
            try {
                canvas.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON)
                val inset = JBUI.scale(4)
                val h = height - inset * 2
                if (tonal) {
                    canvas.color = ink(Palette.primaryContainer)
                    canvas.fillRoundRect(inset, inset, width - inset * 2, h, h, h)
                }
                val alpha = when {
                    !isEnabled -> 0
                    model.isPressed -> 31
                    model.isRollover -> 20
                    hasFocus() -> 31
                    else -> 0
                }
                if (alpha > 0) {
                    val color = foreground
                    canvas.color = Color(color.red, color.green, color.blue, alpha)
                    canvas.fillRoundRect(inset, inset, width - inset * 2, h, h, h)
                }
                if (hasFocus()) {
                    canvas.color = Look.primary
                    canvas.stroke = BasicStroke(JBUI.scale(2).toFloat())
                    canvas.drawRoundRect(1, 1, width - 3, height - 3, height - 3, height - 3)
                }
            } finally { canvas.dispose() }
            super.paintComponent(g)
        }
    }

    private class Glyph(private val plus: Boolean) : Icon {
        override fun getIconWidth() = JBUI.scale(18)
        override fun getIconHeight() = JBUI.scale(18)
        override fun paintIcon(c: Component, g: Graphics, x: Int, y: Int) {
            val canvas = g.create() as Graphics2D
            try {
                canvas.translate(x, y)
                canvas.scale(iconWidth / 18.0, iconHeight / 18.0)
                canvas.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON)
                canvas.color = c.foreground
                canvas.stroke = BasicStroke(1.8f, BasicStroke.CAP_ROUND, BasicStroke.JOIN_ROUND)
                if (plus) {
                    canvas.drawLine(9, 3, 9, 15)
                    canvas.drawLine(3, 9, 15, 9)
                } else {
                    canvas.drawArc(3, 3, 12, 12, 40, 285)
                    canvas.fill(Path2D.Float().apply {
                        moveTo(16.0, 2.0); lineTo(16.0, 8.0); lineTo(10.0, 8.0); closePath()
                    })
                }
            } finally { canvas.dispose() }
        }
    }
}
