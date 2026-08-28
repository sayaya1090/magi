package dev.sayaya.magi.ide.ui

import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindow
import com.intellij.openapi.wm.ToolWindowFactory
import com.intellij.ui.components.JBLabel
import com.intellij.ui.content.ContentFactory
import dev.sayaya.magi.ide.transport.SocketPath
import dev.sayaya.magi.ide.usecase.DaemonLifecycle
import java.nio.file.Paths

/**
 * 골격이다. 지금 하는 일은 하나 — **어느 소켓을 보고 있고 거기 무엇이 있는지 말한다.**
 *
 * 빈 화면을 두지 않는 것이 이 창의 첫 규칙이라(설계 문서 §2 "끝내 못 붙으면 말한다"), 아직
 * 전사가 없어도 상태는 말한다. 전사는 데몬에 `transcript` 문이 생긴 뒤에 온다(§3).
 */
class MagiToolWindow : ToolWindowFactory {

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val label = JBLabel(describe(project))
        val content = ContentFactory.getInstance().createContent(label, null, false)
        toolWindow.contentManager.addContent(content)
    }

    private fun describe(project: Project): String {
        val base = project.basePath ?: return "이 프로젝트에는 경로가 없어 워크스페이스를 정할 수 없다."
        // basePath 는 심링크를 푼다는 보장이 없다. 푸는 자리는 SocketPath 안이다(§2).
        val socket = SocketPath.of(SocketPath.configDir(), Paths.get(base))
        SocketPath.tooLong(socket)?.let { return it }
        val verdict = DaemonLifecycle(socket, start = {}).verdict()
        return when (verdict) {
            DaemonLifecycle.Verdict.ALIVE -> "데몬이 산다: $socket"
            DaemonLifecycle.Verdict.LEFT -> "데몬이 없다(질서 있게 나갔거나 켠 적 없다): $socket"
            DaemonLifecycle.Verdict.KILLED -> "소켓은 있는데 아무도 안 듣는다: $socket"
        }
    }
}
