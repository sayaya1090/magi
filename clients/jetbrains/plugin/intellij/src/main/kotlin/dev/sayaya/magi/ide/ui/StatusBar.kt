package dev.sayaya.magi.ide.ui

import com.intellij.openapi.diagnostic.logger
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.StatusBar
import com.intellij.openapi.wm.StatusBarWidget
import com.intellij.openapi.wm.StatusBarWidgetFactory
import com.intellij.util.Consumer
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

    private class Widget(project: Project) : StatusBarWidget, StatusBarWidget.TextPresentation {
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
        override fun getTooltipText() = "magi — 이 워크스페이스의 컴패니언"
        override fun getClickConsumer(): Consumer<MouseEvent>? = null

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
         */
        private fun unreachable() = workspace.rootsOutsideWorkspace().size

        override fun install(statusBar: StatusBar) {
            bar = statusBar
            timer.start()
            poll()
        }

        override fun dispose() = timer.stop()

        private fun poll() = workspace.onDaemon({
            // 실패만 남긴다. 매 3초 성공을 찍으면 로그가 이것만으로 찬다. 그리고 이 한 줄이 없으면
            // 화면에는 "데몬 없음" 넉 자뿐이라 사람이 원인을 볼 길이 없다 — 이 위젯의 결함 하나가
            // 정확히 그 모양으로 숨어 있었다(사유는 Workspace.onDaemon 의 주석).
            LOG.info("magi: 상태를 못 읽었다 — $it")
            val n = unreachable()
            say("magi: 데몬 없음" + if (n > 0) " · 못 만지는 루트 $n" else "")
        }) { comp -> say(label(comp.facts())) }

        /**
         * 모름을 없음으로 그리지 않는다. 데몬이 `permission` 을 안 실어 보내면 모드를 안 적는다 —
         * 없는 것을 지어내느니 짧게 두는 편이 낫다(§0.5-7).
         */
        private fun label(f: Companion.Facts): String {
            val what = when {
                f.waiting != null -> "기다리는 중"
                f.doing != null -> "도는 중"
                else -> "쉬는 중"
            }
            val n = unreachable()
            val jail = if (n > 0) " · 못 만지는 루트 $n" else ""
            return "magi: $what" + (f.permission?.let { " · $it" } ?: "") + jail
        }

        private fun say(s: String) {
            text = s
            bar?.updateWidget(MagiStatusBarFactory.ID)
        }
    }
}
