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
import dev.sayaya.magi.ide.transport.Published
import dev.sayaya.magi.ide.transport.SocketPath
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.Assist
import dev.sayaya.magi.ide.usecase.DaemonLifecycle
import java.awt.BorderLayout
import java.awt.FlowLayout
import java.nio.file.Paths
import javax.swing.JButton
import javax.swing.SwingUtilities

/**
 * 컴패니언에게 말을 걸고, 그가 묻는 것에 답하는 창.
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

    private class View(private val project: Project) {
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

        /** 이 프로젝트의 소켓. 심링크를 푸는 자리는 SocketPath 안이다(§2). */
        private fun socket() = project.basePath?.let { SocketPath.of(SocketPath.configDir(), Paths.get(it)) }

        /**
         * 데몬에 한 번 붙어 무언가 하고 끊는다. 연결을 들고 있지 않는 이유는 스트림이 아직
         * 없어서다 — 전사 문이 생기면 그때 스트림 하나를 usecase 가 단독으로 소유한다(§3).
         */
        private fun onDaemon(work: (Companion) -> Unit) {
            val sock = socket() ?: return say(state, "이 프로젝트에는 경로가 없어 워크스페이스를 정할 수 없다.")
            ApplicationManager.getApplication().executeOnPooledThread {
                val long = SocketPath.tooLong(sock)
                if (long != null) return@executeOnPooledThread say(state, long)
                try {
                    // 세션 id 는 데몬이 공표한 것을 그대로 쓴다. "이 워크스페이스의 최신"으로
                    // 고르면 며칠 도는 데몬에서 그사이 누가 연 대화를 연다(daemon.go 의 사유).
                    val sid = Published.of(sock)?.session
                    if (sid.isNullOrBlank()) return@executeOnPooledThread
                        say(state, "데몬이 어느 대화에 있는지 공표하지 않았다 — 붙을 자리를 넘겨짚지 않는다.")
                    DaemonClient.connect(sock).use { work(Companion(it, sid)) }
                } catch (e: Exception) {
                    // 못 붙은 것을 빈 화면으로 말하지 않는다(§0.5-7).
                    val v = DaemonLifecycle(sock, start = {}).verdict()
                    say(state, when (v) {
                        DaemonLifecycle.Verdict.LEFT -> "데몬이 없다 — 아직 안 켰거나 질서 있게 나갔다."
                        DaemonLifecycle.Verdict.KILLED -> "소켓은 있는데 아무도 안 듣는다 — 죽은 것으로 보인다."
                        DaemonLifecycle.Verdict.ALIVE -> "붙었다가 끊겼다: ${e.message}"
                    })
                }
            }
        }

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
