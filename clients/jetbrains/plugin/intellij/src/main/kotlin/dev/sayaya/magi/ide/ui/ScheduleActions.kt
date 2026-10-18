package dev.sayaya.magi.ide.ui

import com.intellij.util.ui.JBUI
import java.awt.Component
import java.awt.Dimension
import javax.swing.JButton
import javax.swing.JComponent
import javax.swing.JPanel

/** Native actions aligned with the inline description controls. */
internal object ScheduleActions {
    fun row(canAdd: Boolean, canReload: Boolean, add: () -> Unit, reload: () -> Unit): JComponent =
        JPanel(java.awt.GridLayout(0, 1, 0, JBUI.scale(8))).apply {
            isOpaque = false
            alignmentX = Component.LEFT_ALIGNMENT
            border = JBUI.Borders.emptyTop(8)
            if (canAdd) add(JButton(MagiBundle.msg("plan.schedule.add")).apply {
                toolTipText = text
                addActionListener { add() }
            })
            if (canReload) add(JButton(MagiBundle.msg("plan.schedule.reload")).apply {
                toolTipText = MagiBundle.msg("plan.schedule.reload.tip")
                addActionListener { reload() }
            })
            components.filterIsInstance<JButton>().forEach { button ->
                button.inputMap.put(javax.swing.KeyStroke.getKeyStroke("ENTER"), "pressed")
                button.inputMap.put(javax.swing.KeyStroke.getKeyStroke("released ENTER"), "released")
            }
            minimumSize = Dimension(0, preferredSize.height)
        }

}
