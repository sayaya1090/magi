package dev.sayaya.magi.ide.ui

import com.intellij.openapi.Disposable
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Disposer
import com.intellij.openapi.wm.ToolWindow
import com.intellij.openapi.wm.ToolWindowFactory
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.components.JBTextArea
import com.intellij.ui.content.ContentFactory
import dev.sayaya.magi.ide.model.Ask
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.Waiting
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.transport.HandServer
import dev.sayaya.magi.ide.transport.Published
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.usecase.Assist
import dev.sayaya.magi.ide.usecase.End
import dev.sayaya.magi.ide.usecase.Authorship
import dev.sayaya.magi.ide.usecase.Hand
import dev.sayaya.magi.ide.usecase.Problems
import dev.sayaya.magi.ide.usecase.Transcript
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
        MagiWindows.put(project, view)
        // 창의 수명에 건다. 이걸 안 걸면 창이 닫혀도 스트림·손·등록이 그대로 남는다.
        Disposer.register(toolWindow.disposable, view)
        toolWindow.contentManager.addContent(
            ContentFactory.getInstance().createContent(view.root, null, false)
        )
        view.refresh()
    }

    internal class View(private val project: Project) : Disposable {
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

        /**
         * 전사. **연결을 단독으로 소유한다** — 스트림은 락스텝이 아니라 연결을 통째로 넘겨받으므로
         * 다른 교환과 겸할 수 없다(설계 문서 §3 「스트리밍」).
         */
        private val log = JBTextArea().apply { isEditable = false; lineWrap = true; wrapStyleWord = true }

        /**
         * 문제 판. IntelliJ 자신의 Problems 뷰에 넣지 않았다 — 거기는 IDE 가 자기 인스펙션으로
         * 채우는 자리이고, 컴패니언이 자기 실행에서 본 것을 섞으면 **누가 언제 말한 것인지**가
         * 사라진다. §5-4 가 요구하는 것이 정확히 그 두 가지라 따로 세운다.
         */
        private val problems = JBTextArea().apply { isEditable = false; lineWrap = true; wrapStyleWord = true }

        /**
         * 어느 턴이 무엇을 건드렸나. 전사에서 같이 쌓는다 — 두 번째 스트림을 열지 않는다.
         *
         * 이 창이 사는 동안만 안다. 데몬이 재생해 준 만큼이 전부이고, 그전 것은 모른다 —
         * 모르는 것을 아는 척하지 않는 것이 §5-5 의 규칙이다.
         */
        val authors = Authorship()
        private var following: java.io.Closeable? = null

        /** 창이 닫히는 중인가. 서면 재접속이 멈춘다 — 닫은 창이 스스로 되살아나면 안 된다. */
        private val closing = java.util.concurrent.atomic.AtomicBoolean(false)

        /**
         * 다음 프레임이 **새 스트림의 첫 프레임**인가. 그때 판을 비운다.
         *
         * 이 창은 커서를 안 보내므로 다시 붙을 때마다 재생이 **통째로** 다시 온다. 안 비우면 대화가
         * 두 벌 쌓이고 손댐 장부도 같이 부푼다. 붙자마자 비우지 않고 첫 프레임까지 미루는 이유는,
         * 붙기에 실패한 시도가 사람이 읽고 있던 전사를 지워 버리면 안 되기 때문이다.
         */
        private val fresh = java.util.concurrent.atomic.AtomicBoolean(false)

        /**
         * 손. 창이 서면 같이 서고, 창이 살아 있는 동안만 산다.
         *
         * **창에 매단 이유**가 있다. 손은 IDE 의 편집기를 움직이는데, 편집기가 없는 IDE(웰컴 화면)
         * 에서 서 있으면 데몬은 붙었다고 믿고 에이전트는 매번 거절을 받는다. 창이 있다는 것이
         * 곧 프로젝트가 열려 있다는 것이라 그 자리에 맨다.
         */
        private var hand: HandServer? = null

        /**
         * 전사를 화면으로 옮기는 자리. **연결에 안 매인다** — 다시 붙을 때마다 새로 만들면
         * 같은 규칙이 매번 다시 쓰이고, 그중 한 벌만 고치는 날이 온다.
         */
        private val sink = object : Transcript.Sink {
                /**
                 * 붙었다고 말한다. **[ended] 가 말을 하므로 이쪽도 해야 한다.**
                 *
                 * 없을 때 이랬다: 데몬이 재시작하면 새 세션의 전사가 비어 있어 프레임이 하나도 안
                 * 오고, 판을 비우는 것은 첫 프레임에 걸려 있으므로(아래 [fresh]) 화면의 마지막
                 * 말이 「전사가 끊겼다」로 **다시 붙은 뒤에도** 서 있었다. 사람은 살아 있는 창을
                 * 죽은 줄 알고 안 쓰고, 안 쓰면 프레임도 안 생겨서 그 화면이 스스로를 붙든다.
                 *
                 * 프레임이 오면 [fresh] 가 이 줄까지 같이 지운다. 그래도 맞다 — 그때는 재생분이
                 * 통째로 다시 오고, **그것이 붙었다는 것의 더 나은 증거**다.
                 */
                override fun began() = append("— 전사에 붙었다.")

                override fun frame(e: LogEvent) {
                    // 새 스트림의 첫 프레임이면 판을 비운다. 장부는 **여기서 바로** 비운다 —
                    // EDT 로 미루면 아래 `feed` 가 먼저 돌아서 방금 비운 것이 그것을 지운다.
                    if (fresh.compareAndSet(true, false)) {
                        authors.forget()
                        SwingUtilities.invokeLater { log.text = ""; problems.text = "" }
                    }
                    // 조각에는 줄을 안 준다. 같은 말이 `part.appended` 사실로 뒤따르고, 재생에는
                    // 그 사실만 실린다 — 안 가리면 붙어 있던 창과 나중에 다시 붙은 창이 같은
                    // 대화를 다르게 그린다(사유는 `Transcript.echoesFact`).
                    if (!Transcript.echoesFact(e)) append(render(e))
                    // 문제는 전사에서 갈라 나온다. 두 번째 스트림을 열지 않는 이유는 §3 의 "창 하나에
                    // 스트림 하나" 그대로다 — 같은 프레임을 두 번 파싱하게 된다.
                    authors.feed(e)
                    Problems.of(e)?.let { note(it) }
                    // 물음이 움직였으면 다시 묻는다. 프롬프트는 로그에 안 실려서(전이 이벤트)
                    // 이 신호가 없으면 창을 연 뒤에 올라온 물음은 단추가 영영 안 생긴다 — 로그에
                    // 줄 하나 뜨고 끝이었다(사유는 `Transcript.movesPrompt`).
                    if (Transcript.movesPrompt(e)) refresh()
                    Problems.dissentOf(e)?.let { d ->
                        problems.append("· 카운슬 ${d.member} 반대  #${d.seq}  ${d.at.orEmpty()}\n    ${d.why}\n")
                    }
                }
                // 데몬이 이벤트보다 **먼저** 보내는 말이다. 이미 그린 것을 지워야 한다는 뜻이라
                // 눈에 띄게 적는다 — 조용히 흘리면 화면이 거짓말을 한 채로 남는다.
                override fun note(why: String) = SwingUtilities.invokeLater {
                    log.text = ""
                    append("— $why")
                }
                /**
                 * **누가 끝냈는지로 갈린다.** 사람이 닫았으면 그걸로 끝이고, 데몬이 닫았거나
                 * 끊겼으면 다시 붙는다 — 안 그러면 창은 살아 보이는데 아무것도 안 오고, 물음을
                 * 다시 그리던 신호(`Transcript.movesPrompt`)가 그 스트림을 타고 오므로 **답할
                 * 단추가 같이 죽는다.**
                 */
                override fun ended(end: End) = when (end) {
                    End.ByUs -> append("— 전사를 끊었다.")
                    End.ByDaemon -> { append("— 전사가 끝났다(데몬이 닫았다). 다시 붙어 본다."); reattach() }
                    is End.Broken -> { append("— 전사가 끊겼다: ${end.why}. 다시 붙어 본다."); reattach() }
                }
            }

        init {
            val top = JBPanel<JBPanel<*>>(BorderLayout())
            top.add(state, BorderLayout.NORTH)
            top.add(prompt, BorderLayout.CENTER)
            top.add(buttons, BorderLayout.SOUTH)

            val send = JButton("보내기").apply { addActionListener { say() } }
            val stop = JButton("세우기").apply { addActionListener { interrupt() } }
            val acts = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.RIGHT)).apply { add(stop); add(send) }
            val bottom = JBPanel<JBPanel<*>>(BorderLayout())
            bottom.add(JBScrollPane(input), BorderLayout.CENTER)
            bottom.add(acts, BorderLayout.EAST)
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
            val split = javax.swing.JSplitPane(
                javax.swing.JSplitPane.HORIZONTAL_SPLIT, JBScrollPane(log), JBScrollPane(problems)
            ).apply { resizeWeight = 0.65 }
            root.add(split, BorderLayout.CENTER)
            root.add(bottom, BorderLayout.SOUTH)
            // 못 붙으면 **말하고 다시 붙어 본다.** 바로 아래 [offerHand] 는 못 세운 것을
            // 그대로 말하는데 이 줄은 안 했다 — 같은 init 의 두 줄이 실패를 다르게 다뤘다.
            //
            // 안 하면 이렇게 된다. IDE 를 먼저 열고 터미널에서 데몬을 나중에 띄우는 것이
            // **보통의 차례**인데, 그때 `.session` 이 아직 없어 [Published.of] 가 null 을
            // 주고 [follow] 는 false 로 돌아온다. 그 false 를 아무도 안 읽었다: 화면에
            // 한 줄도 안 나가고, [reattach] 는 [ended] 에서만 불리는데 스트림이 선 적이
            // 없으므로 [ended] 도 영영 안 온다. **창은 그대로 죽어 있고 데몬을 띄워도 안
            // 살아난다** — 사람이 툴윈도를 닫았다 여는 수밖에 없다.
            //
            // 빈 전사에 붙은 것과 못 붙은 것이 **똑같이 빈 화면**이었다. `began` 이 그
            // 둘을 갈랐고(위), 이 줄이 못 붙은 쪽에 말과 재시도를 준다.
            //
            // 사유는 안 댄다. [follow] 는 셋(basePath 없음·`.session` 없음·연결 실패)을
            // 불리언 하나로 접어서 돌려주므로, 여기서 「데몬이 없다」고 쓰면 모르는 것을
            // 아는 척하는 것이 된다. 되풀이되는 실패를 안 적는 것은 [reattach] 의 규칙 그대로다.
            if (!follow()) { append("— 전사에 못 붙었다. 다시 붙어 본다."); reattach() }
            offerHand()
        }

        /**
         * 창이 닫히면 내놓은 것을 도로 거둔다.
         *
         * 이 자리가 통째로 비어 있었다. 창은 전사 스트림 하나와 루프백 서버 하나를 세우고
         * **아무것도 안 거뒀다.** 창을 닫아도 스트림 스레드가 계속 돌고, 손 포트가 계속 열려
         * 있고, 무엇보다 **데몬은 손이 붙어 있다고 계속 믿는다** — 컴패니언이 편집을 죽은 창으로
         * 보낸다.
         *
         * 그 계약은 이미 두 곳에 적혀 있었다 — [hand] 필드 주석이 창이 사는 동안만 손이 산다고 하고,
         * `Companion.kt` 는 "창이 닫히거나 IDE 가 나갈 때 — 안 떼면 데몬이 죽은 주소를 계속 들고 있는다"
         * 고 적어 뒀다. 둘 다 적어 두기만 하고 **부르는 자리를 안 만들었다.** 주석이 약속한 것을 코드가
         * 안 지키면 다음 사람은 지켜지는 줄 알고 그 위에 쌓는다.
         *
         * **문을 먼저 닫고 그다음에 뗀다.** 떼는 것은 소켓 왕복이라 늦을 수 있고 그동안에도 편집이
         * 들어오면 안 된다. 못 떼도 포트는 이미 닫혔으니 죽은 창을 고치는 일은 없다 — 데몬이 죽은
         * 주소를 잠깐 들고 있을 뿐이고, 그건 다음 `mcp-attach` 가 정리한다.
         */
        override fun dispose() {
            // 먼저 세운다. 아래에서 스트림을 닫으면 `ended` 가 도는데, 그때 이미 서 있어야 안 되살아난다.
            closing.set(true)
            MagiWindows.remove(project)
            debounce.stop()
            runCatching { following?.close() }
            following = null
            val server = hand ?: return
            hand = null
            runCatching { server.close() }
            // 떼는 것은 best-effort 다. 사유를 화면에 안 싣는다 — 그 화면이 지금 사라지는 중이다.
            runCatching { workspace.onDaemon({ }, { it.detachHand() }) }
        }

        /**
         * 손을 세우고 컴패니언에게 준다.
         *
         * 거절을 **그대로 보인다.** 같은 워크스페이스를 IDE 둘로 열면 먼저 붙은 쪽만 손이 되고
         * 둘째는 거절을 받는데, 그때 조용하면 둘째 IDE 의 사람은 자기 편집 도구가 왜 안 쓰이는지
         * 알 길이 없다 — §7 의 다섯째 시나리오가 그것이다. 손이 아닌 것과 고장난 것은 다른 사건이다.
         */
        private fun offerHand() {
            val server = runCatching { HandServer.start(Hand(IdeHand(project))) }.getOrNull()
                ?: return say(state, "손을 못 세웠다 — 루프백 포트를 못 열었다.")
            hand = server
            onDaemon { comp ->
                val r = comp.attachHand(server.url, mapOf("X-Magi-Hand" to server.token))
                say(state, when {
                    r.ok -> "손을 붙였다: " + (r.tools?.joinToString(", ") ?: "도구 목록을 안 줬다")
                    else -> "손을 못 붙였다 — " + (r.error ?: "사유 없음")
                })
            }
        }

        /**
         * 전사에 붙는다. 붙는 순간부터 **재생 먼저, 그다음 라이브**다 — 데몬이 그 계약을 지키고
         * 이쪽은 온 차례대로 그린다.
         *
         * 커서를 안 준다. 창이 열릴 때마다 전량을 받는 것이 지금의 답이다 — IDE 가 사는 동안
         * 이어 받는 것은 §8 의 미결이고, 옛 커서를 새 대화로 들고 가면 그 대화의 앞을 못 본다.
         */
        private fun follow(): Boolean {
            val sock = socket() ?: return false
            val sid = runCatching { Published.of(sock)?.session }.getOrNull() ?: return false
            following?.let { runCatching { it.close() } }
            // 여는 것보다 **먼저** 세운다. 스트림은 자기 스레드를 바로 띄우므로 첫 프레임이
            // 이 줄보다 빨리 올 수 있다.
            fresh.set(true)
            val started = runCatching {
                Transcript({ DaemonClient.connect(sock) }, sid).follow(sink)
            }.getOrNull() ?: return false
            following = started
            return true
        }

        /**
         * 끊긴 전사에 다시 붙는다. **창이 닫혔으면 안 붙는다.**
         *
         * 스트림만 되살리는 것으로는 모자란다. 끊겨 있는 동안 올라온 물음은 이 창이 **못 본
         * 이벤트로 지나갔으므로**, 붙자마자 지금 무엇을 묻고 있는지 다시 물어야 한다. 창을 열 때
         * 한 번 묻는 것과 같은 사유가 재접속마다 있다 — 닿음이 돌아온 것 자체가 사건이다.
         *
         * 물러서며 기다린다(1초에서 30초까지 배로). 데몬이 오래 없으면 30초마다 유닉스 소켓에
         * 한 번 붙어 보는 값이고, 실패는 화면에 안 적는다 — 같은 줄을 무한히 쌓으면 사람이
         * 읽던 전사가 밀려난다.
         */
        private fun reattach() {
            if (closing.get()) return
            runCatching {
                ApplicationManager.getApplication().executeOnPooledThread {
                    var wait = 1_000L
                    while (!closing.get()) {
                        try { Thread.sleep(wait) } catch (e: InterruptedException) { return@executeOnPooledThread }
                        if (closing.get()) return@executeOnPooledThread
                        if (follow()) return@executeOnPooledThread refresh()
                        wait = (wait * 2).coerceAtMost(30_000L)
                    }
                }
            }
        }

        /**
         * 이벤트 하나를 한 줄로. **얕게 그린다.**
         *
         * 사람이 읽는 전사로 바꾸는 것은 파생이고, 콘솔이 `cmd/magi-web/main.go` 의 `line` 에서
         * 이미 하고 있다. 그것을 코틀린으로 다시 쓰면 **같은 규칙의 두 번째 표현**이 생긴다 —
         * §3 이 안 C 를 고른 바로 그 사유다. 그래서 여기서는 사실만 적고, 깊은 렌더를 어디서 할지는
         * §8 에 미결로 남긴다.
         */
        private fun render(e: LogEvent): String {
            val who = e.actor?.name?.takeIf { it.isNotBlank() } ?: e.actor?.kind.orEmpty()
            return "#${e.seq} ${e.type}" + if (who.isBlank()) "" else "  ($who)"
        }

        /**
         * 한 건을 적는다. **언제·어느 호출인지가 같이 간다** — 낡은 문제 목록은 없느니만 못하다는
         * 것이 §5-4 의 요구이고, 그 답이 목록을 지우는 것이 아니라 **출처를 적는 것**이다.
         *
         * `where` 가 없으면 그대로 둔다. 못 읽은 앵커를 지어내면 엉뚱한 줄을 가리키고, 그건 항목이
         * 안 눌리는 것보다 나쁘다.
         */
        private fun note(p: Problems.Problem) {
            val head = if (p.advisory) "· 했음(읽을 것 있음)" else "· 실패"
            val where = p.where?.let { "  ${it.path}:${it.line}" } ?: ""
            problems.append("$head ${p.tool.orEmpty()}  #${p.seq}  ${p.at.orEmpty()}$where\n")
            problems.append("    " + p.text.trim().lines().firstOrNull().orEmpty().take(160) + "\n")
            SwingUtilities.invokeLater { problems.caretPosition = problems.document.length }
        }

        private fun append(line: String) = SwingUtilities.invokeLater {
            log.append(line + "\n")
            log.caretPosition = log.document.length
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
                    hint.text = suggestion?.let { "<html><i>제안: $it &nbsp;<b>Tab</b></i></html>" } ?: " "
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

        fun refresh() = onDaemon { redraw(it) }

        /**
         * 프롬프트를 다시 묻고 다시 그린다. [note] 가 있으면 **그것이 상태 문구를 이긴다.**
         *
         * 이기는 쪽을 정해 둔 이유가 있다. 예전에는 단추가 답을 보낸 뒤 [refresh] 를 따로 불렀는데,
         * 그러면 리프레시가 세우는 "사람을 기다리는 중이다"가 방금 받은 거절을 덮었다 — 그것도
         * 연결을 새로 하나 더 열어 가면서, 어느 쪽이 먼저 EDT 에 닿을지는 운이었다. 지금은 한
         * 왕복 안에서 끝나고 순서가 코드에 박혀 있다.
         */
        private fun redraw(comp: Companion, note: String? = null) {
            val w = comp.waiting()
            SwingUtilities.invokeLater { drawPrompt(w) }
            say(state, note ?: if (w == null) "컴패니언이 붙어 있다." else "사람을 기다리는 중이다.")
        }

        /**
         * 돌고 있는 턴을 세운다.
         *
         * 이 단추가 없었다. 동사는 `Companion.interrupt` 로 있었는데 **부르는 자리가 없어서**,
         * 파일을 고치는 플러그인에 도는 턴을 멈출 방법이 하나도 없었다. 안 쓰는 동사라고 지우면
         * 지우는 것 자체가 결정이 된다 — 「멈출 수 없다」는 안전 속성이지 코드 정리 대상이 아니다.
         *
         * **"세웠다"고 안 한다.** 코어의 `app.go` 의 `Interrupt` 는 도는 턴이 없어도 `nil` 을
         * 돌려주므로, `ok` 는 요청이 닿았다는 뜻이지 무엇을 세웠다는 뜻이 아니다. 화면이 와이어가
         * 뒷받침 안 하는 말을 하면 사람은 안 멈춘 것을 멈춘 줄 안다. 실제로 무엇이 멈췄는지는
         * 전사에 나온다.
         *
         * 답은 안 버린다 — 같은 파일의 [add] 가 삼키다 고친 그 규칙이다.
         */
        private fun interrupt() = onDaemon { comp ->
            val r = comp.interrupt()
            say(state, if (r.ok) "세우라고 보냈다." else "안 갔다: ${r.error ?: "사유 없음"}")
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

        /**
         * 대기 중인 프롬프트를 그린다. **무엇을 그릴지는 [Waiting.ask] 가 정하고 여기는 그리기만 한다.**
         *
         * 갈래가 왜 셋인지는 그 주석에 있다. 여기서 지키는 것은 하나다 — **못 그릴 때 침묵하지 않는다.**
         * 단추 없는 물음만 떠 있으면 사람은 창이 고장 난 줄 모르고, 컴패니언은 답을 기다리며 막혀 있다.
         * 그 침묵이 바로 이전 판의 `else` 가 하던 일이었다.
         */
        private fun drawPrompt(w: Waiting?) {
            buttons.removeAll()
            if (w == null) {
                prompt.text = " "
            } else {
                val at = if (w.total > 1) " (${w.index}/${w.total})" else ""
                val ask = w.ask
                val why = (ask as? Ask.Undrawable)?.why?.let { "<br/><i>$it</i>" }.orEmpty()
                prompt.text = "<html><b>${w.what}</b>$at<br/>${w.reason ?: ""}$why</html>"
                when (ask) {
                    is Ask.Permission -> {
                        add("허용") { it.allow(w.id) }
                        add("거부") { it.deny(w.id) }
                        add("항상") { it.always(w.id) }
                    }
                    is Ask.Choose -> ask.options.forEach { opt -> add(opt) { it.answer(w.id, opt) } }
                    // 사유는 위 문구에 실었다. 단추는 안 만든다 — 지어낸 단추는 틀린 답을 보낸다.
                    is Ask.Undrawable -> Unit
                }
            }
            buttons.revalidate(); buttons.repaint()
        }

        /**
         * 프롬프트 단추 하나. [act] 의 답을 **버리지 않는다.**
         *
         * 버리고 있었다. `(Companion) -> Unit` 이라 `allow`·`deny`·`always`·`answer` 가 돌려주는
         * [Response] 가 통째로 사라졌고, 데몬이 거절해도 화면은 다시 그리고 말았다 — 사람이 누른
         * 것이 갔는지 안 갔는지 알 방법이 없는, **눌러도 아무 일도 안 나는 창**이었다. 같은 파일의
         * `say()` 는 처음부터 "안 갔다"를 보고했다. 한 창이 한 동사는 보고하고 나머지 넷은 삼켰다.
         *
         * 이게 지금 더 중요한 이유. 코어의 거절 문구가 **없음의 사유를 못 가른다** — 종류가 어긋난
         * 답도 "이미 답했거나 만료됐다"로 온다(`app.go` 의 `RespondQuestion`). 그 문장을 고치는 일이
         * 논의 중인데, 받는 쪽이 버리고 있으면 고쳐 봐야 아무 데도 안 닿는다.
         */
        private fun add(label: String, act: (Companion) -> Response) {
            buttons.add(JButton(label).apply {
                addActionListener {
                    onDaemon { c ->
                        val r = act(c)
                        redraw(c, if (r.ok) null else "안 갔다: ${r.error ?: "사유 없음"}")
                    }
                }
            })
        }

        private fun say(label: JBLabel, text: String) = SwingUtilities.invokeLater { label.text = text }
    }
}

/**
 * 살아 있는 창을 찾는 길. 액션이 전사에서 쌓인 것을 물어야 하는데, 그 자료는 창이 들고 있고
 * 창은 게으르게 만들어진다.
 *
 * **`companion object` 가 아니라 이름 있는 객체다.** 처음엔 `MagiToolWindow` 안에 companion 으로
 * 뒀는데, 그러면 코틀린이 만드는 `MagiToolWindow.Companion` 이 **usecase 의 `Companion` 클래스를
 * 가린다** — 같은 파일이 그 클래스를 쓰고 있어서 그 자리들이 통째로 컴파일을 못 했다. 도메인에
 * `Companion` 이라는 이름이 있는 한 이 파일에 companion object 를 두면 안 된다.
 *
 * `WeakHashMap` **만으로는 안 놓아준다.** 값인 [MagiToolWindow.View] 가 자기 키인 `Project` 를
 * 필드로 들고 있어서 키가 값에서 강하게 닿고, 그러면 엔트리가 영영 안 걷힌다 — WeakHashMap 의
 * 고전적인 오용이다. 여기 주석은 한때 그 반대를 적어 두고 있었다. 실제로 놓아주는 것은
 * [MagiToolWindow.View.dispose] 가 부르는 [remove] 이고, 약한 키는 그것이 못 돌았을 때를 위한
 * 둘째 줄이다.
 *
 * 그리고 **없으면 null 을 준다** — 액션이 "창이 아직 안 열렸다"고 말할 수 있어야 하고, 빈 답을
 * 내면 "이 파일은 아무도 안 건드렸다"와 구분이 안 된다.
 */
internal object MagiWindows {
    private val live = java.util.WeakHashMap<Project, MagiToolWindow.View>()
    fun put(project: Project, view: MagiToolWindow.View) = synchronized(live) { live[project] = view }
    fun of(project: Project): MagiToolWindow.View? = synchronized(live) { live[project] }
    fun remove(project: Project) = synchronized(live) { live.remove(project) }
}
