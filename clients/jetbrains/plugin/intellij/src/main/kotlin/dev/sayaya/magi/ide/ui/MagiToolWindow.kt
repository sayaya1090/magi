package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindow
import com.intellij.openapi.wm.ToolWindowFactory
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.components.JBTextArea
import com.intellij.ui.content.ContentFactory
import dev.sayaya.magi.ide.model.Waiting
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.Assist
import java.awt.BorderLayout
import java.awt.FlowLayout
import javax.swing.JButton
import javax.swing.SwingUtilities

/**
 * 대화 — 컴패니언에게 말을 걸고, 그가 묻는 것에 답하는 창. **하단 독**에 산다.
 *
 * 콘솔에서는 대화가 가운데다(`docs/UI.md` §2.2). IDE 에서 가운데는 **고치는 것**의 자리라 그대로
 * 옮기면 §5 의 첫 규칙("IDE 와 겹치는 것은 만들지 않는다")을 배치로 어긴다. 그리고 계속 흘러내리는
 * 글을 IntelliJ 가 두는 자리는 아래다 — Run, Terminal, Build 가 전부 거기 있다. 사실 판은 우측으로
 * 갈렸다([FactsToolWindow]).
 *
 * 전사는 아직 없다 — 데몬에 `transcript` 문이 생겨야 온다(설계 문서 §3). 그때까지도 이 창은
 * 비어 있으면 안 되므로(§0.5 불변식 7) 지금 할 수 있는 둘을 먼저 한다: 지시를 보내는 것과
 * 대기 중인 프롬프트에 답하는 것. 둘 다 이미 있는 메서드로 된다.
 *
 * 소켓 입출력은 전부 풀 스레드에서 돈다. EDT 에서 소켓을 잡으면 데몬이 느린 동안 IDE 가 선다.
 */
class MagiToolWindow : ToolWindowFactory {

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val view = View(project)
        toolWindow.contentManager.addContent(
            ContentFactory.getInstance().createContent(view.root, null, false)
        )
        view.refresh()
    }

    private class View(project: Project) {
        private val workspace = Workspace(project)
        val root = JBPanel<JBPanel<*>>(BorderLayout())
        private val state = JBLabel(" ")
        private val prompt = JBLabel(" ")
        private val buttons = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.LEFT))
        private val input = JBTextArea(3, 40)
        private val hint = JBLabel(" ")
        /** 마지막으로 받은 제안. 탭으로 받아들인다. */
        private var suggestion: String? = null
        private val debounce = javax.swing.Timer(400) { askSuggestion() }.apply { isRepeats = false }

        init {
            val top = JBPanel<JBPanel<*>>(BorderLayout())
            top.add(state, BorderLayout.NORTH)
            top.add(prompt, BorderLayout.CENTER)
            top.add(buttons, BorderLayout.SOUTH)

            val send = JButton("보내기").apply { addActionListener { say() } }
            val bottom = JBPanel<JBPanel<*>>(BorderLayout())
            bottom.add(JBScrollPane(input), BorderLayout.CENTER)
            bottom.add(send, BorderLayout.EAST)
            bottom.add(hint, BorderLayout.SOUTH)

            // 치는 동안 제안을 묻는다. 매 글자마다가 아니라 멈추면 — 모델 호출이라 값이 있다.
            input.document.addDocumentListener(object : javax.swing.event.DocumentListener {
                override fun insertUpdate(e: javax.swing.event.DocumentEvent) = debounce.restart()
                override fun removeUpdate(e: javax.swing.event.DocumentEvent) = debounce.restart()
                override fun changedUpdate(e: javax.swing.event.DocumentEvent) {}
            })
            // 탭으로 받아들인다. 제안이 없으면 탭은 원래 하던 일을 한다.
            input.registerKeyboardAction({ acceptSuggestion() },
                javax.swing.KeyStroke.getKeyStroke("TAB"), javax.swing.JComponent.WHEN_FOCUSED)

            root.add(top, BorderLayout.NORTH)
            root.add(bottom, BorderLayout.SOUTH)
        }

        /** 거들기는 연결을 따로 판다 — 모델 호출이 락스텝 연결을 물면 그동안 다른 교환이 선다. */
        private fun assist() = socket()?.let { s -> Assist({ DaemonClient.connect(s) }) }

        private fun askSuggestion() {
            val prefix = input.text
            val a = assist() ?: return
            ApplicationManager.getApplication().executeOnPooledThread {
                val said = a.suggest(prefix)
                SwingUtilities.invokeLater {
                    // 그새 사람이 더 쳤으면 낡은 제안이다. 붙이지 않는다.
                    if (input.text != prefix) return@invokeLater
                    suggestion = said?.takeIf { it.isNotBlank() }
                    hint.text = suggestion?.let { "<html><i>제안: ${'$'}it &nbsp;<b>Tab</b></i></html>" } ?: " "
                }
            }
        }

        private fun acceptSuggestion() {
            val s = suggestion ?: return
            input.text = input.text + s
            suggestion = null
            hint.text = " "
        }

        private fun socket() = workspace.socket()

        /** 데몬에 한 번 붙어 무언가 하고 끊는다. 배선은 [Workspace] 가 갖는다 — 창 둘이 같이 쓴다. */
        private fun onDaemon(work: (Companion) -> Unit) =
            workspace.onDaemon({ say(state, it) }, work)

        fun refresh() = onDaemon { comp ->
            val w = comp.waiting()
            say(state, if (w == null) "컴패니언이 붙어 있다." else "사람을 기다리는 중이다.")
            SwingUtilities.invokeLater { drawPrompt(w) }
        }

        private fun say() {
            val text = input.text.trim()
            if (text.isEmpty()) return
            onDaemon { comp ->
                val r = comp.say(text)
                say(state, if (r.ok) "보냈다." else "안 갔다: ${r.error ?: "사유 없음"}")
                if (r.ok) SwingUtilities.invokeLater { input.text = ""; suggestion = null; hint.text = " " }
            }
        }

        /** 대기 중인 프롬프트를 그린다. 퍼미션이면 세 갈래, 질문이면 선택지 그대로. */
        private fun drawPrompt(w: Waiting?) {
            buttons.removeAll()
            if (w == null) {
                prompt.text = " "
            } else {
                val at = if (w.total > 1) " (${w.index}/${w.total})" else ""
                prompt.text = "<html><b>${w.what}</b>$at<br/>${w.reason ?: ""}</html>"
                if (w.isPermission) {
                    add("허용") { it.allow(w.id) }
                    add("거부") { it.deny(w.id) }
                    add("항상") { it.always(w.id) }
                } else {
                    w.options.orEmpty().forEach { opt -> add(opt) { it.answer(w.id, opt) } }
                }
            }
            buttons.revalidate(); buttons.repaint()
        }

        private fun add(label: String, act: (Companion) -> Unit) {
            buttons.add(JButton(label).apply {
                addActionListener { onDaemon { c -> act(c); refresh() } }
            })
        }

        private fun say(label: JBLabel, text: String) = SwingUtilities.invokeLater { label.text = text }
    }
}
