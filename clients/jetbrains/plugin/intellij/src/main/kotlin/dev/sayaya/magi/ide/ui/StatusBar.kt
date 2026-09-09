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
 * 상태 표시줄(Status Bar)에 magi 컴패니언의 현재 활동 상태, 권한 모드, 턴 경과 시간 등을 표시하는 위젯 팩토리 (설계 문서 §5-6).
 *
 * 도구 창은 사용자가 직접 클릭하여 열기 전까지 초기화되지 않는 지연 로딩(lazy loading) 특성을 가지므로,
 * 도구 창이 닫혀 있는 상태에서도 백엔드 컴패니언의 기동 및 작업 진행 여부를 상시 파악할 수 있도록 상태 표시줄 위젯을 항상 유지한다.
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
         * 상태 폴링 주기 (3,000ms).
         * 웹 콘솔 명단(700ms) 대비 주기를 길게 설정하여 백그라운드 소켓 통신 오버헤드를 최소화한다.
         */
        private val timer = javax.swing.Timer(3_000) { poll() }.apply { isRepeats = true }

        override fun ID() = MagiStatusBarFactory.ID
        override fun getPresentation() = this
        override fun getAlignment() = java.awt.Component.CENTER_ALIGNMENT
        override fun getText() = text

        /** 데몬 통보 카운슬 심의 활성화 여부 (null이면 미통보 상태로 표기 생략). */
        @Volatile private var council: Boolean? = null

        /** 상태 표시줄 텍스트 폭 제약을 고려하여 상세 부가 정보(외부 루트 경고, 카운슬 모드)는 툴팁으로 제공한다 (가이드라인 검토 G14). */
        override fun getTooltipText(): String {
            val n = unreachable()
            return MagiBundle.msg("status.tip") +
                (council?.let { " · " + MagiBundle.msg(if (it) "status.council.on" else "status.council.off") } ?: "") +
                (if (n > 0) " · " + MagiBundle.msg("status.outside", n) else "")
        }

        /** 위젯 클릭 시 하단 대화 도구 창을 활성화한다. */
        override fun getClickConsumer() = Consumer<MouseEvent> {
            com.intellij.openapi.wm.ToolWindowManager.getInstance(project)
                .getToolWindow("magi")?.activate(null)
        }

        /**
         * 작업 영역 외부 컨텐트 루트 수 조회.
         *
         * 도구 창이 열리지 않아도 경고를 확인할 수 있도록 상태 표시줄 툴팁을 통해 안내한다 (가이드라인 G14).
         * 프로젝트 구조 변경을 반영하기 위해 매 폴링마다 갱신하되, EDT 블로킹을 방지하기 위해 백그라운드 풀 스레드(폴링 주기)에서
         * 계산하여 volatile 필드([outside])에 저장하고 툴팁 렌더링 시에는 캐시된 값만 참조한다 (리뷰 R7).
         */
        private fun unreachable() = outside

        /** 최근 측정된 작업 영역 외부 컨텐트 루트 수 (쓰기: 풀 스레드, 읽기: EDT). */
        @Volatile private var outside: Int = 0

        override fun install(statusBar: StatusBar) {
            bar = statusBar
            timer.start()
            poll()
        }

        override fun dispose() = timer.stop()

        // 3초 주기 상태 폴링 (데몬 단기 타임아웃 PATIENCE_POLL 적용)
        private fun poll() = workspace.onDaemonPolling({
            outside = workspace.rootsOutsideWorkspace().size
            LOG.info("magi: 상태를 못 읽었다 — $it")
            // 데몬 프로세스 기동 직후 초기화 소요 시간(실측 약 7초) 동안 '기동 중' 상태를 명시적으로 표출한다 (2026-09-01 실측 반영).
            workspace.socket()?.let { sock ->
                if (StartDaemon.startingNow(sock)) return@onDaemonPolling say("magi: " + MagiBundle.msg("status.starting"))
            }
            say("magi: " + MagiBundle.msg("status.nodaemon"))
        }) { comp ->
            outside = workspace.rootsOutsideWorkspace().size
            val f = comp.facts()
            council = f.council
            say(label(f))
        }

        /**
         * 컴패니언 활동 상태 판정([Activity.of]) 및 상태 레이블 포맷팅.
         *
         * 데몬 미통보 상태([Activity.Unsaid])를 임의로 유휴 상태로 처리하지 않고 '연결됨'으로 정확히 표출한다 (§0.5-7).
         * 상태 표시줄의 한정된 가로 폭을 고려하여 상세 설명은 생략하고 핵심 상태만을 간결하게 구성한다.
         */
        private fun label(f: Companion.Facts): String {
            val what = when (Activity.of(f)) {
                is Activity.Doing -> MagiBundle.msg("status.doing")
                Activity.Waiting -> MagiBundle.msg("status.waiting")
                Activity.Unsaid -> MagiBundle.msg("status.attached")
            }
            return "magi: $what" + (f.permission?.let { " · " + Perms.label(it) } ?: "") + turn()
        }

        /**
         * 턴 경과 시간 및 카운슬 심의 라운드 정보 포맷팅 (docs/UI.ko.md §4.3).
         * 도구 창이 활성화되어 셰이퍼가 스트림을 수신 중일 때만 표시하며, 심의 중인 대상 위원을 함께 표기한다.
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
