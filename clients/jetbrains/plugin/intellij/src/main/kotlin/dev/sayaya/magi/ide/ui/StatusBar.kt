package dev.sayaya.magi.ide.ui

import com.intellij.openapi.diagnostic.logger
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.StatusBar
import com.intellij.openapi.wm.StatusBarWidget
import com.intellij.openapi.wm.StatusBarWidgetFactory
import com.intellij.util.Consumer
import dev.sayaya.magi.ide.usecase.Activity
import dev.sayaya.magi.ide.usecase.Companion
import java.awt.event.MouseEvent

/**
 * 상태 표시줄 — 컴패니언이 도는지, 사람을 기다리는지, 어떤 승인 모드인지(설계 문서 §5-6).
 *
 * 툴윈도와 다른 자리인 이유가 있다. **툴윈도는 게으르다** — 사람이 열어야 생성된다. 그래서 창을
 * 닫아 둔 동안에는 컴패니언이 무엇을 하는지 볼 방법이 아예 없었다. 상태 표시줄은 프로젝트가
 * 열려 있는 한 서 있으므로, "지금 뭔가 도는가"를 **묻지 않아도** 답한다.
 *
 * 실측으로 알게 된 것이라 적어 둔다: 샌드박스 IDE 를 띄워 보니 플러그인은 로드되는데
 * (`Loaded custom plugins: magi`) 툴윈도는 클릭 전까지 만들어지지 않아 데몬에 접속조차 하지
 * 않았다. 화면이 둘뿐이면 그 상태가 **아무 표시 없는 상태**와 같아 보인다.
 */
class MagiStatusBarFactory : StatusBarWidgetFactory {
    override fun getId() = ID
    override fun getDisplayName() = "magi"
    override fun createWidget(project: Project): StatusBarWidget = Widget(project)

    companion object {
        const val ID = "magi.status"
    }

    private class Widget(private val project: Project) : StatusBarWidget, StatusBarWidget.TextPresentation {
        private val LOG = logger<Widget>()
        private val workspace = Workspace(project)
        private var bar: StatusBar? = null
        private var text = "magi: …"

        /**
         * 폴링 간격. 콘솔이 명단에 쓰는 값(700ms)보다 훨씬 느리게 잡는다 — 저쪽은 사람이 보고 있는
         * 표이고 이쪽은 눈꼬리에 있는 한 줄이다. 그리고 매 틱이 소켓 왕복 하나다.
         */
        private val timer = javax.swing.Timer(3_000) { poll() }.apply { isRepeats = true }

        override fun ID() = MagiStatusBarFactory.ID
        override fun getPresentation() = this
        override fun getAlignment() = java.awt.Component.CENTER_ALIGNMENT
        override fun getText() = text
        /**
         * 툴팁이 **위젯 글자에 안 들어간 것**을 마저 말한다 — 상태 표시줄은 눈꼬리 한 줄이고
         * 그 폭을 옆 위젯들이 나눠 쓴다. 그래서 워크스페이스 밖 폴더 경고는 글자가 아니라
         * 여기 산다(가이드라인 검토 G14).
         */
        override fun getTooltipText(): String {
            val n = unreachable()
            return MagiBundle.msg("status.tip") +
                (if (n > 0) " · " + MagiBundle.msg("status.outside", n) else "")
        }

        /** 눌리는 위젯이다 — 자세한 것이 있는 자리로 데려간다(안 그러면 막다른 글자다). */
        override fun getClickConsumer() = Consumer<MouseEvent> {
            com.intellij.openapi.wm.ToolWindowManager.getInstance(project)
                .getToolWindow("magi")?.activate(null)
        }

        /**
         * 컴패니언이 못 만지는 컨텐트 루트가 몇 개인가. 데몬이 아니라 IDE 가 아는 사실이라 붙기
         * 전에도 답이 있다 — 그래서 이 줄만은 데몬을 안 기다린다.
         *
         * **우측 판에만 두면 안 보인다.** 그 판은 툴윈도라 게으르고, 사람이 안 열면 만들어지지도
         * 않는다(실측: 루트가 밖에 있는 프로젝트를 열었는데 판이 안 만들어져 경고가 계산조차 안
         * 됐다). 경고를 게으른 자리에만 두면 **경고가 필요한 사람이 제일 못 본다.**
         *
         * ⚠ 여기 `val` 로 세어 두면 **같은 잘못을 시간 축에서 반복한다.** 「데몬을 안 기다린다」의
         * 근거가 「한 번만 센다」의 근거로 미끄러졌던 자리다. 루트를 세션 중에 더하면 경고가 영영
         * 안 뜨고, 사람이 루트를 고쳐도 경고가 안 사라진다 — 시킨 대로 했는데 문장이 안 변한다.
         * 게으른 자리에서 경고를 옮겨 온 이유가 그대로 여기에도 적용된다: **경고가 필요해지는
         * 순간에 만들어진 사람이 제일 못 본다.** 매 틱 그리는 줄이니 매 틱 센다.
         *
         * **세는 것은 풀 스레드에서만 한다.** 모듈 × 컨텐트 루트를 전부 돌며 경로를 정규화하는
         * 일이라, 툴팁 경로(EDT)에서 부르면 큰 저장소에서 3초마다 EDT 를 문다 — 「매 틱 센다」의
         * 근거는 폴이 풀 스레드에 있다는 전제 위에 쓴 것인데, 자리를 옮기면서 그 전제가 같이
         * 안 옮겨졌다(리뷰 R7). 그래서 폴이 세어 [outside] 에 담고, 그리는 쪽은 읽기만 한다.
         */
        private fun unreachable() = outside

        /** 마지막으로 센 「밖의 루트」 수 — 쓰는 쪽은 폴(풀 스레드), 읽는 쪽은 EDT. */
        @Volatile private var outside: Int = 0

        override fun install(statusBar: StatusBar) {
            bar = statusBar
            timer.start()
            poll()
        }

        override fun dispose() = timer.stop()

        private fun poll() = workspace.onDaemon({
            outside = workspace.rootsOutsideWorkspace().size
            // 실패만 남긴다. 매 3초 성공을 찍으면 로그가 이것만으로 찬다. 그리고 이 한 줄이 없으면
            // 화면에는 "데몬 없음" 넉 자뿐이라 사람이 원인을 볼 길이 없다 — 이 위젯의 결함 하나가
            // 정확히 그 모양으로 숨어 있었다(사유는 Workspace.onDaemon 의 주석).
            LOG.info("magi: 상태를 못 읽었다 — $it")
            // **띄운 직후엔 그렇게 말한다.** 데몬이 서는 데 몇 초가 걸리고(실측 7초, 진짜 설정의
            // 첫 기동), 그동안 「실행되지 않음」만 서 있으면 사람은 아무 일도 안 일어났다고 읽는다
            // — 실제로 그렇게 읽혔다(2026-09-01). 우리가 방금 뭘 했는지는 우리가 말해야 한다.
            workspace.socket()?.let { sock ->
                if (StartDaemon.startingNow(sock)) return@onDaemon say("magi: " + MagiBundle.msg("status.starting"))
            }
            // 접두는 **코드 한 곳에서만** 붙인다 — 넷 중 하나만 값 안에 품고 있어서 같은 자리의
            // 규칙이 두 벌이었다(G15). 밖의 폴더 수는 툴팁의 몫이다.
            say("magi: " + MagiBundle.msg("status.nodaemon"))
        }) { comp ->
            outside = workspace.rootsOutsideWorkspace().size
            say(label(comp.facts()))
        }

        /**
         * 모름을 없음으로 그리지 않는다. 데몬이 `permission` 을 안 실어 보내면 모드를 안 적는다 —
         * 없는 것을 지어내느니 짧게 두는 편이 낫다(§0.5-7).
         *
         * **`doing` 에는 그 규칙을 안 지키고 있었다.** 여기 `else` 가 "쉬는 중"이었는데, 그 갈래에
         * 오는 것은 「안 도는 중」이 아니라 **데몬이 안 말한 것**이다(`Facts` 주석이 그 둘을 같은
         * null 로 받는다고 이름 대어 적어 뒀다). 우측 판은 같은 자리를 "도는 것 없음"으로 그리고
         * 있었으니, 한 규칙을 두 화면이 한 벌씩 적어 두고 그중 안 재지는 쪽이 갈라진 것이다.
         * 판정은 [Activity] 로 내렸고 여기 남은 일은 **폭에 맞는 글자를 고르는 것**뿐이다.
         */
        private fun label(f: Companion.Facts): String {
            val what = when (Activity.of(f)) {
                // 표시줄은 폭이 없어 무엇을 도는지는 안 적는다 — 그건 우측 판이 그린다.
                is Activity.Doing -> MagiBundle.msg("status.doing")
                Activity.Waiting -> MagiBundle.msg("status.waiting")
                // 아는 것만 말한다: 답을 받았으니 닿긴 닿았고, 그 이상은 데몬이 안 말했다.
                Activity.Unsaid -> MagiBundle.msg("status.attached")
            }
            // 밖에 있는 폴더 수는 **툴팁으로 내렸다**(G14) — 글자는 짧을수록 이 자리에 맞다.
            return "magi: $what" + (f.permission?.let { " · " + Perms.label(it) } ?: "") + turn()
        }

        /**
         * 턴 경과와 카운슬 라운드 — 터미널이 발치 미터와 머리 칩으로 그리는 그 둘이다
         * (docs/UI.ko.md §4.3). 원천은 전사 셰이퍼라 **툴윈도가 살아 있을 때만 안다**:
         * 툴윈도는 게으르고, 셰이퍼는 그 창의 스트림에 산다. 창이 없으면 이 조각은 비고
         * 수준은 그대로 선다 — 모름을 0초로 그리지 않는다(§0.5-7).
         *
         * 경과는 연 시각(이벤트 ts)에서 이 기계의 시계로 센다. 두 기계의 시계를 비교하는
         * 것이 맞나 싶지만 반대다 — ts 를 한 번 받고 그 뒤로 여기 시계로 **이어** 세는 것이라,
         * 시계가 어긋나면 첫 값이 어긋날 뿐 흐르는 속도는 맞는다.
         */
        private fun turn(): String {
            val v = MagiWindows.of(project) ?: return ""
            val bits = mutableListOf<String>()
            v.turnOpenedAt()?.let { ts ->
                runCatching { java.time.Instant.parse(ts) }.getOrNull()?.let { t0 ->
                    val s = java.time.Duration.between(t0, java.time.Instant.now()).seconds.coerceAtLeast(0)
                    bits += "턴 " + if (s >= 60) "${s / 60}m${s % 60}s" else "${s}s"
                }
            }
            v.councilRound()?.let { r ->
                // 「지금 누구에게 묻는 중」이 붙으면 멈춘 것과 기다리는 것이 갈린다 — 심의는
                // 멤버마다 모델을 한 번씩 부르므로 그 침묵이 수십 초다.
                bits += "⚖ r$r" + (v.councilAsking()?.let { " · $it" } ?: "")
            }
            return bits.joinToString("") { " · $it" }
        }

        private fun say(s: String) {
            text = s
            bar?.updateWidget(MagiStatusBarFactory.ID)
        }
    }
}
