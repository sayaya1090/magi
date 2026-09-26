package dev.sayaya.magi.ide.ui

import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.DialogWrapper
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.components.JBTextArea
import com.intellij.util.ui.FormBuilder
import dev.sayaya.magi.ide.usecase.AnswerDrafts
import java.awt.Dimension
import java.awt.event.ActionEvent
import javax.swing.Action
import javax.swing.JComponent

class AnswerRecoveryDialog(
    private val project: Project,
    private val item: AnswerDrafts.Recovery,
    private val onCopy: (AnswerDrafts.Recovery) -> Unit,
    private val onDelete: (AnswerDrafts.Recovery) -> Unit,
) : DialogWrapper(project, true) {
    val fullText = JBTextArea(item.text).apply {
        isEditable = false
        lineWrap = true
        wrapStyleWord = true
    }

    init {
        title = MagiBundle.msg("chat.answer.recovery.detail.title")
        init()
    }

    override fun createCenterPanel(): JComponent {
        val reasonText = when (item.reason) {
            AnswerDrafts.REASON_SESSION_CHANGED ->
                MagiBundle.msg("chat.answer.recovery.reason.session_changed")
            AnswerDrafts.REASON_QUESTION_LEFT ->
                MagiBundle.msg("chat.answer.recovery.reason.question_left")
            AnswerDrafts.REASON_SUBMISSION_FAILED ->
                MagiBundle.msg("chat.answer.recovery.reason.submission_failed")
            else -> item.reason
        }
        val form = FormBuilder.createFormBuilder()
            .addLabeledComponent(MagiBundle.msg("chat.answer.recovery.workspace"), JBLabel(project.basePath ?: project.name))
            .addLabeledComponent(MagiBundle.msg("chat.answer.recovery.session"), JBLabel(item.session))
            .addLabeledComponent(MagiBundle.msg("chat.answer.recovery.callid"), JBLabel(item.callId))
        val qText = item.questionText
        if (!qText.isNullOrBlank()) {
            form.addLabeledComponent(MagiBundle.msg("chat.answer.recovery.question"), JBLabel(qText))
        }
        return form
            .addLabeledComponent(MagiBundle.msg("chat.answer.recovery.reason"), JBLabel(reasonText))
            .addLabeledComponent(MagiBundle.msg("chat.answer.recovery.fulltext"), JBScrollPane(fullText).apply {
                preferredSize = Dimension(450, 200)
            })
            .panel
    }

    override fun createActions(): Array<Action> = arrayOf(
        object : DialogWrapperAction(MagiBundle.msg("chat.answer.recovery.copy")) {
            override fun doAction(e: ActionEvent?) {
                onCopy(item)
            }
        },
        object : DialogWrapperAction(MagiBundle.msg("chat.answer.recovery.delete")) {
            override fun doAction(e: ActionEvent?) {
                onDelete(item)
                close(OK_EXIT_CODE)
            }
        },
        cancelAction.apply { putValue(Action.NAME, MagiBundle.msg("common.cancel")) }
    )
}
