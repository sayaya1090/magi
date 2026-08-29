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
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.model.Ask
import dev.sayaya.magi.ide.model.Subject
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.Waiting
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.transport.HandServer
import dev.sayaya.magi.ide.transport.Published
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.Markup
import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.usecase.Assist
import dev.sayaya.magi.ide.usecase.End
import dev.sayaya.magi.ide.usecase.Authorship
import dev.sayaya.magi.ide.usecase.Hand
import dev.sayaya.magi.ide.usecase.Level
import dev.sayaya.magi.ide.usecase.Problems
import dev.sayaya.magi.ide.usecase.Row
import dev.sayaya.magi.ide.usecase.Rows
import dev.sayaya.magi.ide.usecase.Who
import dev.sayaya.magi.ide.usecase.Transcript
import java.awt.BorderLayout
import java.awt.Color
import java.awt.FlowLayout
import javax.swing.BorderFactory
import javax.swing.JButton
import javax.swing.JTextPane
import javax.swing.SwingUtilities
import javax.swing.text.SimpleAttributeSet
import javax.swing.text.StyleConstants

/**
 * 대화 — 컴패니언에게 말을 걸고, 그가 묻는 것에 답하는 창. **하단 독**에 산다.
 *
 * 콘솔에서는 대화가 가운데다(`docs/UI.md` §2.2). IDE 에서 가운데는 **고치는 것**의 자리라 그대로
 * 옮기면 §5 의 첫 규칙("IDE 와 겹치는 것은 만들지 않는다")을 배치로 어긴다. 그리고 계속 흘러내리는
 * 글을 IntelliJ 가 두는 자리는 아래다 — Run, Terminal, Build 가 전부 거기 있다. 사실 판은 우측으로
 * 갈렸다([FactsToolWindow]).
 *
 * 전사는 데몬의 `transcript` 문에서 이벤트로 오고, 셰이퍼([Rows])가 행으로 편다 — 무엇이
 * 행이 되는지는 `docs/TRANSCRIPT.ko.md` 의 표가 정하고, 이 창은 그 행을 붓질만 한다
 * ([renderRow]). 한동안 이 창은 `#seq type (actor)` 만 적었고 사람이 친 글도 답도 화면에
 * 없었다 — 살아 있는 샌드박스에서 실측한 구멍이라, 몸통이 행에 서는 것부터 골든이 붙든다.
 *
 * 소켓 입출력은 전부 풀 스레드에서 돈다. EDT 에서 소켓을 잡으면 데몬이 느린 동안 IDE 가 선다.
 */
class MagiToolWindow : ToolWindowFactory {

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val view = View(project)
        // 수준이 서는 자리. 라벨이었을 때는 거의 안 변하는 큰 한 줄이 전사 위 판 하나를 먹었다
        // (사용자 실측: "왜 이렇게 커? 변하는 데이터도 없네") — 창이 무엇인지 말하는 자리는
        // 제목표시줄이고, 항상 보여야 하는 쪽은 상태 표시줄이 이미 한다(docs/UI.ko.md §3.1).
        view.title = { t -> SwingUtilities.invokeLater { toolWindow.setTitle(t) } }
        // 행동의 동사들은 기어 메뉴로(설정 화면은 남는 상태의 자리다 — docs/UI.ko.md §5). 결과는
        // 전사로 보고된다: 사건은 라벨이 아니라 전사라는 그 규칙이 여기도 그대로다.
        toolWindow.setAdditionalGearActions(com.intellij.openapi.actionSystem.DefaultActionGroup(
            view.verb("대화 요약해 접기 (compact)") { it.compact() },
            view.verb("마지막 턴 되감기 (rewind)") { it.rewind(1) },
        ))
        MagiWindows.put(project, view)
        // 창의 수명에 건다. 이걸 안 걸면 창이 닫혀도 스트림·손·등록이 그대로 남는다.
        Disposer.register(toolWindow.disposable, view)
        // 두 판은 좌우가 아니라 **탭**이다. 분할이던 동안 문제 판이 거의 빈 채로 폭의 35%를
        // 먹었고(사용자 실측), 한 창에 이름 다른 글 두 벌을 두는 IDE 의 어휘가 탭이다 — Run 창이
        // 프로세스마다 탭이지 분할이 아니다(§0-5).
        val make = ContentFactory.getInstance()
        toolWindow.contentManager.addContent(make.createContent(view.root, "대화", false))
        toolWindow.contentManager.addContent(make.createContent(view.problemsView, "문제", false))
        view.refresh()
    }

    internal class View(private val project: Project) : Disposable {
        private val workspace = Workspace(project)
        val root = JBPanel<JBPanel<*>>(BorderLayout())
        /** 수준을 제목표시줄에 쓰는 손. 창을 만든 쪽이 채운다 — 여기서는 IDE 를 모른다. */
        var title: (String) -> Unit = {}
        private val prompt = JBLabel(" ").apply { border = Look.quiet }
        private val buttons = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.LEFT, 8, 4))
            .apply { border = JBUI.Borders.empty(0, 8, 6, 8) }
        private val input = JBTextArea(3, 40).apply { border = JBUI.Borders.empty(8, 10) }
        private val hint = JBLabel(" ").apply {
            foreground = Look.faint
            border = JBUI.Borders.empty(2, 12, 6, 12)
        }
        /** 마지막으로 받은 제안. 탭으로 받아들인다. */
        private var suggestion: String? = null
        private val debounce = javax.swing.Timer(400) { askSuggestion() }.apply { isRepeats = false }

        /**
         * 전사. **연결을 단독으로 소유한다** — 스트림은 락스텝이 아니라 연결을 통째로 넘겨받으므로
         * 다른 교환과 겸할 수 없다(설계 문서 §3 「스트리밍」).
         */
        private val column = Look.column()
        private val scroll = JBScrollPane(column).apply {
            border = JBUI.Borders.empty()
            horizontalScrollBarPolicy = javax.swing.ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER
        }

        /** 전사 셰이퍼. 이벤트는 워커 스레드에서 먹고, 그리는 것은 EDT 가 스냅샷으로 한다. */
        private val shaper = Rows()

        /**
         * 그릴 일이 밀려 있는가. 워커가 프레임 백 개를 밀어도 EDT 에는 스냅샷 한 번이다.
         *
         * **`init` 보다 위에 선다.** 코틀린은 선언 순서대로 초기화하고, `init` 의 못-붙음 보고가
         * [append]→[redrawLog] 로 이 값을 두드린다 — 아래 있던 동안 그 첫 보고가 NPE 로 죽어
         * **툴윈도 내용이 통째로 안 만들어졌다**(샌드박스 실측: 입력칸까지 같이 사라진다).
         */
        private val dirty = java.util.concurrent.atomic.AtomicBoolean(false)

        /**
         * 문제 판. IntelliJ 자신의 Problems 뷰에 넣지 않았다 — 거기는 IDE 가 자기 인스펙션으로
         * 채우는 자리이고, 컴패니언이 자기 실행에서 본 것을 섞으면 **누가 언제 말한 것인지**가
         * 사라진다. §5-4 가 요구하는 것이 정확히 그 두 가지라 따로 세운다.
         */
        private val problems = Look.pane()
        val problemsView: JBScrollPane by lazy { JBScrollPane(problems).apply { border = JBUI.Borders.empty() } }

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
                 * 오고, 판을 비우는 것이 첫 프레임에 걸려 있으므로 화면의 마지막 말이
                 * 「전사가 끊겼다」로 **다시 붙은 뒤에도** 서 있었다. 사람은 살아 있는 창을 죽은 줄
                 * 알고 안 쓰고, 안 쓰면 프레임도 안 생겨서 그 화면이 스스로를 붙든다.
                 *
                 * **판도 여기서 비운다.** 이 창은 커서를 안 보내므로 다시 붙을 때마다 재생이 통째로
                 * 다시 온다 — 안 비우면 대화가 두 벌 쌓이고 손댐 장부도 같이 부푼다. 예전에는 그
                 * 비움이 **첫 프레임**에 걸려 있었다(플래그 하나로). 붙자마자 비우면 붙기에 실패한
                 * 시도가 사람이 읽던 전사를 지운다는 것이 사유였는데, 이 자리는 애초에 **못 붙으면
                 * 안 온다** — 그 사유가 여기서는 이미 지켜진다(`TranscriptTest` 의
                 * 「연결을 못 열면 붙었다고 하지 않는다」).
                 *
                 * 프레임에 걸어 두면 남는 것이 셋이었다. **(1)** 프레임이 안 오는 전사 — 즉 위에
                 * 적은 바로 그 기본 경로 — 에서는 비울 기회가 영영 없어 **지난 세션의 대화가 그대로
                 * 서 있었다.** 위의 고침이 말만 고치고 판은 안 고쳤던 것이다. **(2)** 이 줄 자신이
                 * 나중에 지워졌다. **(3)** 붙은 뒤 [offerHand] 가 전사에 적는 손의 결과가 뒤늦게 온
                 * 첫 프레임에 같이 지워졌다 — 재생이 다시 실어다 주지 않는 말이라 그대로 없어진다.
                 *
                 * 순서는 스트림이 보장한다. `Transcript.follow` 는 연결이 열린 뒤 **워커를 띄우기
                 * 전에** 이 줄을 부르므로(같은 시험의 「붙었다는 말은 첫 프레임보다 먼저 정확히 한 번
                 * 온다」), 여기서 큐에 넣은 비움이 어떤 프레임보다 먼저 EDT 에 닿는다. 장부는 EDT 를
                 * 안 거치고 여기서 바로 비운다 — 미루면 워커의 `feed` 가 먼저 돌아서 방금 비운 것이
                 * 그것을 지운다.
                 */
                override fun began() {
                    authors.forget()
                    shaper.clear() // 커서를 안 보내므로 재생이 통째로 다시 온다 — 여기서 비워야 두 벌이 안 쌓인다
                    SwingUtilities.invokeLater { problems.text = "" }
                    append("— 전사에 붙었다.")
                }

                override fun frame(e: LogEvent) {
                    // 판을 비우는 것은 여기가 아니라 [began] 이다. 사유는 그쪽에 적었다.
                    // 조각에는 줄을 안 준다. 같은 말이 `part.appended` 사실로 뒤따르고, 재생에는
                    // 그 사실만 실린다 — 안 가리면 붙어 있던 창과 나중에 다시 붙은 창이 같은
                    // 대화를 다르게 그린다(사유는 `Transcript.echoesFact`).
                    if (!Transcript.echoesFact(e) && shaper.feed(e)) redrawLog()
                    // 문제는 전사에서 갈라 나온다. 두 번째 스트림을 열지 않는 이유는 §3 의 "창 하나에
                    // 스트림 하나" 그대로다 — 같은 프레임을 두 번 파싱하게 된다.
                    authors.feed(e)
                    Problems.of(e)?.let { note(it) }
                    // 물음이 움직였으면 다시 묻는다. 물음 자체(`*.requested`)는 전이라 로그에 안
                    // 실려서, 이 신호가 없으면 창을 연 뒤에 올라온 물음은 단추가 영영 안 생긴다 —
                    // 로그에 줄 하나 뜨고 끝이었다(사유는 `Transcript.movesPrompt`).
                    //
                    // **여기서 `e` 를 읽지 않는다.** 넷이 다 전이인 것은 아니다 —
                    // `permission.decided` 는 사실이라 저장되고, 다시 붙을 때마다 재생으로 또 온다
                    // (실측도 그쪽에 적었다). 신호로만 쓰고 그릴 값은 데몬에게 새로 물으니 옛
                    // 프레임이 불러도 지금 값이 그려진다. 이 줄이 `e` 를 보기 시작하면 재생이
                    // 지나간 물음을 지금 것으로 그린다.
                    if (Transcript.movesPrompt(e)) refresh()
                    Problems.dissentOf(e)?.let { dissent(it) }
                }
                // 데몬이 이벤트보다 **먼저** 보내는 말이다. 이미 그린 것을 지워야 한다는 뜻이라
                // 눈에 띄게 적는다 — 조용히 흘리면 화면이 거짓말을 한 채로 남는다.
                override fun note(why: String) {
                    shaper.clear() // 커서를 못 믿겠다는 통보 — 이미 그린 것을 지우라는 뜻이다
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
            top.add(prompt, BorderLayout.CENTER)
            top.add(buttons, BorderLayout.SOUTH)
            // 윗단과 전사를 실선으로 가른다. **지금 상태**와 **지나간 것**은 다른 종류의 글이라
            // 눈이 한 번은 걸려야 한다(§3.1a 의 도랑이 하는 일을 좁은 판에서 선 하나가 한다).
            val head = JBPanel<JBPanel<*>>(BorderLayout()).apply {
                add(top, BorderLayout.CENTER)
                add(Look.rule(), BorderLayout.SOUTH)
            }

            val send = JButton("보내기").apply { addActionListener { say() } }
            val stop = JButton("세우기").apply { addActionListener { interrupt() } }
            val acts = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.RIGHT, 8, 8)).apply { add(stop); add(send) }
            val writing = JBPanel<JBPanel<*>>(BorderLayout()).apply {
                border = JBUI.Borders.empty(8, 12, 0, 8)
                add(JBScrollPane(input), BorderLayout.CENTER)
                add(acts, BorderLayout.EAST)
            }
            val bottom = JBPanel<JBPanel<*>>(BorderLayout())
            bottom.add(Look.rule(), BorderLayout.NORTH)
            bottom.add(writing, BorderLayout.CENTER)
            bottom.add(hint, BorderLayout.SOUTH)

            // 치는 동안 제안을 묻는다. 매 글자마다가 아니라 멈추면 — 모델 호출이라 값이 있다.
            //
            // **다시 묻는 것과 지금 답을 거두는 것은 한 사건이다.** 전에는 치면 타이머만 다시 돌고
            // 화면의 제안은 그대로 서 있었다. 그 제안은 **한 글자 전의 앞머리로 만든 것**인데 라벨은
            // `Tab` 이라고 적혀 있으니, 시킨 대로 누르면 지금 안 맞는 글자가 붙는다("git" 의 제안
            // " status" 가 "gith" 뒤에 붙어 "gith status"). 낡은 **답을 안 붙이는** 문지기는 아래
            // `askSuggestion` 에 있었지만 그건 늦게 온 답을 막을 뿐, **이미 화면에 선 제안**은 아무도
            // 안 거뒀다. 물음을 다시 여는 자리에서 같이 거둔다.
            input.document.addDocumentListener(object : javax.swing.event.DocumentListener {
                override fun insertUpdate(e: javax.swing.event.DocumentEvent) = retract()
                override fun removeUpdate(e: javax.swing.event.DocumentEvent) = retract()
                override fun changedUpdate(e: javax.swing.event.DocumentEvent) {}
                private fun retract() { dropSuggestion(); debounce.restart() }
            })
            // 탭으로 받아들인다. 제안이 없으면 탭은 원래 하던 일을 한다.
            input.registerKeyboardAction({ acceptSuggestion() },
                javax.swing.KeyStroke.getKeyStroke("TAB"), javax.swing.JComponent.WHEN_FOCUSED)
            // Enter 는 보낸다 — 웹도 터미널도 그렇다. 줄바꿈은 Shift+Enter 로 남긴다.
            // registerKeyboardAction 이 아니라 inputMap 인 이유: JTextArea 의 insert-break 가
            // ENTER 에 앉아 있어서, 그 자리를 바꿔 앉혀야 눌림과 줄바꿈이 같이 안 난다.
            input.getInputMap(javax.swing.JComponent.WHEN_FOCUSED)
                .put(javax.swing.KeyStroke.getKeyStroke("ENTER"), "magi.send")
            input.getInputMap(javax.swing.JComponent.WHEN_FOCUSED)
                .put(javax.swing.KeyStroke.getKeyStroke("shift ENTER"), "insert-break")
            input.actionMap.put("magi.send", object : javax.swing.AbstractAction() {
                override fun actionPerformed(e: java.awt.event.ActionEvent) = say()
            })

            root.add(head, BorderLayout.NORTH)
            // 대화만 이 판이다. 문제는 출처가 다른 글이라(왼쪽은 전부, 저쪽은 사람이 손댈 것만)
            // 같은 창의 **다른 탭**으로 간다 — 이름은 탭이 단다.
            root.add(scroll, BorderLayout.CENTER)
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
            // **사유를 댄다.** 예전엔 안 댔고 그게 맞기도 했다 — [follow] 가 셋을 불리언 하나로
            // 접어 돌려줬으니 여기서 「데몬이 없다」고 쓰면 모르는 것을 아는 척하는 것이었다.
            // 접힌 사유를 화면에서 펴지 않는 것은 지금도 규칙이고, 그래서 편 것이 아니라
            // **안 접게 했다**([Attach]). 셋은 사람이 할 일이 서로 다르다: 자리가 없는 것은
            // 프로젝트 문제고, `.session` 이 없는 것은 데몬을 띄우면 되고, 던진 것은 데몬이
            // 있는데 말이 안 통하는 것이다. 가장 흔한 차례(IDE 를 먼저 열고 데몬을 나중에)가
            // 하필 가운데다.
            //
            // `else` 를 안 쓴다. 넷째 갈래가 생기는 날 컴파일러가 울어야 한다 — 안 그러면 새
            // 사유가 옛 문장 뒤에 조용히 숨는다.
            when (val a = follow()) {
                Attach.Ok -> {}
                Attach.NoWorkspace -> lost("이 프로젝트에 붙일 자리가 없다(작업공간 경로를 못 찾았다)")
                Attach.NoSession -> lost("데몬이 아직 없다")
                is Attach.Failed -> lost("데몬에 말을 못 걸었다: ${a.why}")
            }
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
                ?: return report("손을 못 세웠다 — 루프백 포트를 못 열었다.")
            hand = server
            onDaemon { comp ->
                val r = comp.attachHand(server.url, mapOf("X-Magi-Hand" to server.token))
                report(when {
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
        /**
         * 전사에 붙는 시도의 결과. **불리언이 아니다.**
         *
         * 예전엔 셋을 `false` 하나로 접어 돌려줬다. 그러면 화면은 둘 중 하나만 할 수 있다 —
         * 아무 사유도 안 대거나(사람은 왜 안 되는지 모른 채 창을 닫았다 연다), 아니면 「데몬이
         * 없다」고 **지어내거나**. 둘 다 접은 자리가 만든 것이지 화면의 잘못이 아니다.
         *
         * 셋인 이유는 사람이 할 일이 셋이라서다. 붙일 자리가 없는 것은 프로젝트 쪽 문제고,
         * `.session` 이 없는 것은 데몬을 띄우면 되고, 던진 것은 데몬이 있는데 말이 안 통하는
         * 것이다. `End` 의 갈래 셋과 같은 사유다 — **받는 쪽이 할 일이 다르면 갈래다.**
         *
         * `when` 에 `else` 를 안 쓴다. 넷째가 생기면 컴파일러가 울어야 한다 — 안 그러면 새
         * 사유가 옛 문장 뒤에 조용히 숨고, 그건 접어 뒀던 때와 같은 상태다.
         */
        private sealed interface Attach {
            data object Ok : Attach

            /** 붙일 자리가 없다. 작업공간 경로를 못 찾았다 — 데몬 유무와 무관하다. */
            data object NoWorkspace : Attach

            /** 자리는 아는데 `.session` 이 없다. **데몬이 아직 안 떴다** — 가장 흔한 차례다. */
            data object NoSession : Attach

            /** 열다 실패했다. 데몬이 있는데 말이 안 통한다. [Failed.why] 는 던진 것이 한 말 그대로다. */
            data class Failed(val why: String) : Attach
        }

        private fun follow(): Attach {
            val sock = socket() ?: return Attach.NoWorkspace
            val sid = runCatching { Published.of(sock)?.session }.getOrNull() ?: return Attach.NoSession
            following?.let { runCatching { it.close() } }
            // 던진 것을 **그대로** 싣는다. `getOrNull` 로 버리고 여기서 문장을 지으면 「데몬이
            // 이렇게 말했다」 자리에 내가 만든 낱말이 앉는다 — 접어 두던 때와 같은 거짓이고,
            // 사유가 하나뿐이라 더 그럴듯해서 더 나쁘다.
            val started = runCatching {
                Transcript({ DaemonClient.connect(sock) }, sid).follow(sink)
            }.getOrElse { return Attach.Failed(it.message ?: it.toString()) }
            following = started
            return Attach.Ok
        }

        /**
         * 못 붙었다고 화면에 한 번 적고 다시 붙어 본다. **되풀이되는 실패는 안 적는다** —
         * 같은 줄을 무한히 쌓으면 사람이 읽던 전사가 밀려난다([reattach] 의 규칙).
         */
        private fun lost(why: String) {
            append("— 전사에 못 붙었다: $why. 다시 붙어 본다.")
            reattach()
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
                        if (follow() == Attach.Ok) return@executeOnPooledThread refresh()
                        wait = (wait * 2).coerceAtMost(30_000L)
                    }
                }
            }
        }

        /**
         * 전사를 통째로 다시 그린다. **덧붙이기가 아니라 재생이다** — 셰이퍼의 변이에 재배치가
         * 있어서(재부상, 인라인 답) 붙이기만 하는 판은 순서를 잃는다. 지우는 사건은 없으므로
         * (docs/TRANSCRIPT.ko.md §4) 이 재생은 언제나 같은 것을 더 그릴 뿐이다.
         *
         * 행이 글자가 아니라 **컴포넌트**인 이유: 텍스트 판 하나에 다 밀어 넣던 동안 전사가
         * 여백 없는 로그 덤프로 읽혔다(사용자 실측). 행마다 판을 주면 사이 여백·대기 막대·접힌
         * 인자 펼치기가 전부 스윙의 보통 물건이 된다.
         */
        private fun redrawLog() {
            if (!dirty.compareAndSet(false, true)) return
            SwingUtilities.invokeLater {
                dirty.set(false)
                column.removeAll()
                shaper.list().forEach { column.add(rowPanel(it)) }
                column.revalidate()
                column.repaint()
                SwingUtilities.invokeLater {
                    scroll.verticalScrollBar.value = scroll.verticalScrollBar.maximum
                }
            }
        }

        /**
         * 행 하나를 붓질한다. **무엇을 적을지는 셰이퍼가 정했고 여기는 자리와 색만 안다** —
         * 반대로 하면 행 규칙이 판 수만큼 생긴다(`docs/TRANSCRIPT.ko.md` §0).
         */
        private fun renderRow(r: Row): JBPanel<JBPanel<*>> = rowPanel(r)

        private fun rowPanel(r: Row): JBPanel<JBPanel<*>> {
            val p = JBPanel<JBPanel<*>>(BorderLayout(0, 2))
            p.border = if (r.pending) Look.pendingRow() else Look.row()
            when (r.who) {
                Who.User, Who.Agent -> {
                    val marks = buildList {
                        if (r.queued) add("⌛ 대기" to Look.faint)
                        if (r.abandoned) add("✕ 버려짐" to Look.muted)
                        if (r.pending) add("… 처리 중" to Look.faint)
                    }
                    val name = if (r.who == Who.User) "사람" else "magi"
                    val hue = if (r.who == Who.User) Look.primary else Look.accent
                    p.add(Look.rowHead(name, hue, marks, clock(r.at)), BorderLayout.NORTH)
                    p.add(Look.prose(r.text), BorderLayout.CENTER)
                }
                Who.Thinking -> p.add(
                    Look.aside("(생각) " + r.text.lineSequence().firstOrNull().orEmpty()),
                    BorderLayout.CENTER,
                )
                Who.Tool -> {
                    val (glyph, hue) = when {
                        r.ok == null -> "…" to Look.faint
                        r.note -> "✓ 읽을 것 있음" to Look.warn
                        r.ok == true -> "✓" to Look.success
                        else -> "✗" to Look.error
                    }
                    p.add(Look.toolHead(r.tool.orEmpty(), glyph, hue, oneLine(r.args.orEmpty(), 100), clock(r.at)),
                        BorderLayout.NORTH)
                    // 실패가 말한 것의 첫 줄. 전문과 파일:줄 앵커는 문제 탭의 몫이다.
                    r.out?.let { p.add(Look.aside("↳ " + it.lineSequence().firstOrNull().orEmpty(), Look.error),
                        BorderLayout.CENTER) }
                }
                Who.Council -> {
                    val name = r.member ?: "합의"
                    val marks = buildList {
                        r.decision?.let {
                            add(it to when (it) { "done" -> Look.success; "continue" -> Look.warn; else -> Look.faint })
                        }
                    }
                    p.add(Look.rowHead("⚖ $name", Look.seat(name) ?: Look.body, marks, clock(r.at)),
                        BorderLayout.NORTH)
                    val body = JBPanel<JBPanel<*>>().apply {
                        layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.Y_AXIS)
                        isOpaque = false
                        if (r.text.isNotBlank()) add(Look.prose(r.text))
                        r.keep?.takeIf { it.isNotBlank() }?.let { add(Look.aside("keep: $it")) }
                        r.why?.takeIf { it.isNotBlank() }?.let { add(Look.aside(it)) }
                    }
                    p.add(body, BorderLayout.CENTER)
                }
                Who.Info -> p.add(Look.aside(r.text), BorderLayout.CENTER)
            }
            return p
        }

        /** 이벤트 ts 를 이 자리의 시각으로. 못 읽으면 빈칸 — 지어내지 않는다. */
        private fun clock(at: String?): String = at?.let {
            runCatching {
                java.time.Instant.parse(it).atZone(java.time.ZoneId.systemDefault())
                    .toLocalTime().withNano(0).toString()
            }.getOrNull()
        }.orEmpty()

        /** 한 줄로 줄인다. 인자가 길면 전사가 그 인자만으로 화면을 다 먹는다. */
        private fun oneLine(s: String, max: Int): String {
            val one = s.lineSequence().joinToString(" ")
            return if (one.length <= max) one else one.take(max) + "…"
        }

        /**
         * 한 건을 적는다. **언제·어느 호출인지가 같이 간다** — 낡은 문제 목록은 없느니만 못하다는
         * 것이 §5-4 의 요구이고, 그 답이 목록을 지우는 것이 아니라 **출처를 적는 것**이다.
         *
         * `where` 가 없으면 그대로 둔다. 못 읽은 앵커를 지어내면 엉뚱한 줄을 가리키고, 그건 항목이
         * 안 눌리는 것보다 나쁘다.
         */
        private fun note(p: Problems.Problem) = SwingUtilities.invokeLater {
            val head = if (p.advisory) "· 했음(읽을 것 있음)" else "· 실패"
            push(problems, head, if (p.advisory) Look.warn else Look.error, bold = true)
            push(problems, " ${p.tool.orEmpty()}", Look.body)
            push(problems, "  #${p.seq}  ${p.at.orEmpty()}", Look.muted)
            p.where?.let { push(problems, "  ${it.path}:${it.line}", Look.accent) }
            push(problems, "\n    " + p.text.trim().lines().firstOrNull().orEmpty().take(160) + "\n",
                Look.faint)
            problems.caretPosition = problems.document.length
        }

        /**
         * 카운슬의 반대. 문제 판에 같이 서지만 **[Problems.of] 가 고른 것과 섞이지 않게** 자리
         * 색으로 누가 말했는지를 적는다 — 실패는 붉고, 반대는 그 자리의 색이다. 콘솔이 긋는 선과
         * 같다: 판정은 판정의 색이고 이름은 누구인지의 색이다(`console.css`).
         */
        private fun dissent(d: Problems.Dissent) = SwingUtilities.invokeLater {
            push(problems, "· 카운슬 ", Look.faint)
            push(problems, d.member, Look.seat(d.member) ?: Look.faint, bold = true)
            push(problems, " 반대", Look.body)
            push(problems, "  #${d.seq}  ${d.at.orEmpty()}", Look.muted)
            push(problems, "\n    ${d.why}\n", Look.faint)
            problems.caretPosition = problems.document.length
        }

        /**
         * 창이 하는 말. **판에 직접 쓰지 않고 셰이퍼에 넣는다** — 전사가 통째 다시 그리기라
         * ([redrawLog]), 흐름 밖에 쓴 줄은 다음 프레임에 지워진다. 같은 흐름에 서면 "전사는
         * 덧붙이기만 하므로 덮일 자리가 없다"([report] 의 계약)가 그대로 산다.
         */
        private fun append(line: String) {
            shaper.info(line)
            redrawLog()
        }

        /**
         * 판에 글자 한 토막을 얹는다. **여기가 색이 붙는 유일한 자리다.**
         *
         * 글자를 정하지 않는다는 것이 규칙이다 — 무엇을 적을지는 부르는 쪽이 이미 정했고 여기는
         * 그것을 어떻게 보이게 할지만 안다. 반대로 하면(여기서 줄을 조립하면) 같은 서식이 두 군데
         * 생기고, 그중 한쪽만 고치는 날이 온다.
         *
         * 전부 EDT 에서 부른다. 예전 `problems.append` 는 스트림 스레드에서 문서를 고치고 캐럿만
         * EDT 로 옮겼는데, 그건 Swing 이 금지하는 것을 두 줄 중 한 줄만 지킨 것이다.
         */
        private fun push(
            pane: JTextPane, text: String, colour: Color?,
            italic: Boolean = false, bold: Boolean = false,
        ) {
            val a = SimpleAttributeSet()
            colour?.let { StyleConstants.setForeground(a, it) }
            if (italic) StyleConstants.setItalic(a, true)
            if (bold) StyleConstants.setBold(a, true)
            pane.styledDocument.insertString(pane.styledDocument.length, text, a)
        }

        /**
         * 내가 방금 한 것이 어떻게 됐는지 알린다. **윗줄 라벨이 아니라 전사로 나간다.**
         *
         * 보고와 수준은 다른 말이다. "안 갔다: 사유"는 한 번 일어난 사건이고 "컴패니언이 붙어
         * 있다"는 지금 계속 참인 상태다. 둘을 같은 자리에 쓰면 뒤엣것이 앞엣것을 지우는데,
         * **뒤엣것이 이겨도 앞엣것이 거짓이 되지 않는다** — 그냥 사유가 사라진다. 실제로 그랬다:
         * [interrupt] 와 [say] 가 세운 거절은 전사에 `movesPrompt` 프레임이 하나만 들어와도
         * [refresh] 가 돌면서 "컴패니언이 붙어 있다"로 덮였다.
         *
         * 순서로는 못 막는다. 그 프레임이 언제 올지는 저쪽이 정하고, 사람이 라벨을 언제 볼지는
         * 아무도 안 정한다. 그래서 이기는 쪽을 고르는 대신 **자리를 갈랐다** — 사건은 여기로,
         * 수준은 [redraw] 가 라벨로. 전사는 덧붙이기만 하므로 덮일 자리가 아예 없고, 사유가 그
         * 사건이 난 자리 옆에 남는다. [Transcript.Sink.ended] 가 처음부터 그러고 있었다.
         *
         * 앞머리 `—` 는 그 줄들과 같은 표시다: 전사가 아니라 **창이 하는 말**.
         *
         * 되돌아오지 않게 `SourceTextTest` 가 라벨에 쓰는 자리 수를 붙들고 있다.
         */
        private fun report(text: String) = append("— $text")

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
                    // 보이는 것과 **Tab 이 붙이는 것**이 같아야 한다. 여긴 모델이 지은 글자라
                    // 코드가 섞여 오고, 안 거르면 `<T>` 같은 조각이 태그로 먹혀 사라진다 —
                    // 사람은 짧아진 제안을 보고 Tab 을 누르고, 입력창에는 안 보이던 것이 들어간다.
                    hint.text = suggestion?.let { "<html><i>제안: ${Markup.text(it)} &nbsp;<b>Tab</b></i></html>" } ?: " "
                }
            }
        }

        private fun acceptSuggestion() {
            val s = suggestion ?: return
            input.text = input.text + s
            dropSuggestion()
        }

        /** 선 제안을 거둔다. 값과 그 값을 광고하는 줄이 **같이** 없어져야 한다. */
        private fun dropSuggestion() {
            suggestion = null
            hint.text = " "
        }

        private fun socket() = workspace.socket()

        /**
         * 상태 표시줄이 묻는 두 사실 — 턴이 언제 열렸고 카운슬이 몇 라운드인가. 원천은 전사
         * 셰이퍼라 이 창이 살아 있는 동안만 답이 있다. 셰이퍼 자체를 내주지 않는 것은 문을
         * 좁게 두는 것이다 — 넓은 문은 언젠가 딴것도 지나간다.
         */
        fun turnOpenedAt(): String? = if (shaper.open) shaper.openedAt else null
        fun councilRound(): Int? = shaper.councilRound

        /**
         * 기어 메뉴의 동사 하나. 답을 버리지 않는 것은 [add] 와 같은 규칙이고, 보고가 전사로
         * 가는 것은 [report] 의 규칙이다 — 단추마다 한 벌씩 다시 적지 않으려고 여기로 접는다.
         */
        fun verb(label: String, act: (Companion) -> Response): com.intellij.openapi.actionSystem.AnAction =
            object : com.intellij.openapi.actionSystem.AnAction(label) {
                override fun actionPerformed(e: com.intellij.openapi.actionSystem.AnActionEvent) =
                    onDaemon { comp ->
                        val r = act(comp)
                        report(if (r.ok) "$label — 보냈다." else "$label — 안 갔다: ${r.error ?: "사유 없음"}")
                    }
            }

        /** 데몬에 한 번 붙어 무언가 하고 끊는다. 배선은 [Workspace] 가 갖는다 — 창 둘이 같이 쓴다. */
        private fun onDaemon(work: (Companion) -> Unit) =
            workspace.onDaemon({ say(Level.Unreachable(it)) }, work)

        fun refresh() = onDaemon { redraw(it) }

        /**
         * 프롬프트를 다시 묻고 다시 그린다. **이 라벨에는 수준만 쓴다.**
         *
         * 예전에는 보고를 여기로 받았다(`note` 인자). 단추가 답을 보낸 뒤 [refresh] 를 따로
         * 불렀고, 그러면 리프레시가 세우는 "사람을 기다리는 중이다"가 방금 받은 거절을 덮었기
         * 때문이다 — 그것도 연결을 새로 하나 더 열어 가면서, 어느 쪽이 먼저 EDT 에 닿을지는
         * 운이었다. 그래서 한 왕복 안으로 넣고 이기는 쪽을 코드에 박았다.
         *
         * 그건 **단추 경로만** 막았다. [interrupt] 와 [say] 가 세운 "안 갔다: 사유"는, 전사에
         * `movesPrompt` 프레임이 하나 들어와 [refresh] 가 돌면 그대로 덮였다 — 단추가 만든
         * 창은 닫았는데 **이벤트가 만드는 창은 그대로였다.** 순서를 더 박아도 안 닫힌다: 그
         * 프레임이 언제 올지는 이 창이 안 정한다.
         *
         * 지금은 [report] 가 사건을 전사로 내보내고 여기는 수준만 쓴다. 덮일 것이 없으니 이기는
         * 쪽을 정할 일도 없어서 `note` 가 없어졌다.
         *
         * 이 라벨에 남은 나머지 한 자리는 [onDaemon] 의 못 붙은 사유인데, **그것도 수준이다** —
         * 방금 일어난 일이 아니라 지금 데몬이 없다는 말이다.
         *
         * 「수준만 쓴다」는 이제 주석이 아니라 **타입**이다. 라벨은 [Level] 만 받으므로 사건은
         * 여기 들어올 이름이 없다 — 세는 것으로 붙들던 때는 자리 수가 그대로인 채 하나가 조용히
         * 사건으로 바뀔 수 있었다.
         */
        private fun redraw(comp: Companion) {
            val w = comp.waiting()
            SwingUtilities.invokeLater { drawPrompt(w) }
            say(if (w == null) Level.Attached else Level.Waiting)
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
            report(if (r.ok) "세우라고 보냈다." else "안 갔다: ${r.error ?: "사유 없음"}")
        }

        private fun say() {
            val text = input.text.trim()
            if (text.isEmpty()) return
            onDaemon { comp ->
                val r = comp.say(text)
                report(if (r.ok) "보냈다." else "안 갔다: ${r.error ?: "사유 없음"}")
                if (r.ok) SwingUtilities.invokeLater { input.text = ""; dropSuggestion() }
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
            // 물음이 서 있는 동안은 그 자리에 막대를 하나 세운다. 콘솔이 답 없는 물음에 긋는 것과
            // 같은 선이고(`.row.pending .txt`), 터미널도 같은 자리에 긋는다. 색으로만 말하지
            // 않는다 — 글자는 그대로 있고 막대는 **어디를 보라**는 표시다.
            prompt.border = if (w == null) Look.quiet else Look.pending()
            if (w == null) {
                prompt.text = " "
            } else {
                val at = if (w.total > 1) " (${w.index}/${w.total})" else ""
                val ask = w.ask
                val why = (ask as? Ask.Undrawable)?.why?.let { "<br/><i>${Markup.text(it)}</i>" }.orEmpty()
                // **무엇을 정하는지를 보인다.** 도구 이름은 요청의 설명이지 요청이 아니다 —
                // 판정과 사유는 `Waiting.subject` 에 있다.
                val subject = when (val sub = w.subject) {
                    is Subject.Stated -> listOfNotNull(
                        sub.args?.let { "<tt>${Markup.text(it)}</tt>" },
                        sub.reason?.let { Markup.text(it) },
                    ).joinToString("<br/>")
                    // 못 받은 것을 못 받았다고 적는다. 이 줄이 없으면 사람은 아는 것(도구 이름)만
                    // 보고 누르고, 창이 무엇을 덜 받았는지는 영영 안 나온다.
                    Subject.Unstated -> "<i>이 물음에 정해지는 것이 안 실려 왔다 — " +
                        "무엇을 허가하는지 이 창은 모른다.</i>"
                }
                prompt.text = "<html><b>${Markup.text(w.what)}</b>$at<br/>$subject$why</html>"
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
                        if (!r.ok) report("안 갔다: ${r.error ?: "사유 없음"}")
                        redraw(c)
                    }
                }
            })
        }

        /**
         * 수준을 쓰는 **하나뿐인 문**. 자리가 라벨에서 제목표시줄로 갔고([title]), 문이 하나에
         * [Level] 만 받는 규칙은 그대로다 — 문이 여럿이면 타입은 그중 하나만 지킨다
         * (`SourceTextTest`). 라벨 시절의 수준 색은 여기서 끝났다: 제목은 IDE 의 글자라 색을
         * 못 받는데, 색은 원래 글자를 대신하지 않는 보조였으니 잃는 것은 보조뿐이다.
         */
        private fun say(l: Level) = title(l.text)
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
