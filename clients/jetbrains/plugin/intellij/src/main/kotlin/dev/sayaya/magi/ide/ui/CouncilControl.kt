package dev.sayaya.magi.ide.ui

import com.intellij.ui.components.JBCheckBox

/** The daemon owns the value. A click requests a change after confirmation. */
internal class CouncilControl(
    private val confirm: (Boolean) -> Boolean,
    private val change: (String, Boolean, () -> Unit) -> Unit,
) : JBCheckBox(MagiBundle.msg("plan.council")) {
    private var session = ""
    private var value = false
    private var busy = false
    private var available = false
    init {
        isEnabled = false
        addActionListener {
            isSelected = value
            if (busy || session.isBlank()) return@addActionListener
            val target = session
            val enabled = !value
            if (!confirm(enabled) || target != session) return@addActionListener
            busy = true
            isEnabled = false
            change(target, enabled) { busy = false; isEnabled = available }
        }
    }
    fun update(session: String, enabled: Boolean?) {
        this.session = session
        if (enabled != null) { value = enabled; isSelected = enabled }
        available = session.isNotBlank() && enabled != null
        isEnabled = !busy && available
    }
}
