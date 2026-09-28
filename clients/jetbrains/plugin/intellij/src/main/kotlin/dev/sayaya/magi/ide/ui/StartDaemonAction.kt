package dev.sayaya.magi.ide.ui

import com.intellij.icons.AllIcons
import com.intellij.openapi.actionSystem.ActionPlaces
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.project.DumbAware
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.usecase.Reach

/**
 * 사용자가 수동으로 magi 데몬 프로세스를 기동하는 액션 ([StartDaemon.byHand]).
 *
 * 자동 기동 설정이 비활성화되었거나 재시작 예산([dev.sayaya.magi.ide.usecase.Launches]) 소진, 데몬 상태 판정 불확실 등 자동 복구가 불가능한 상황에서
 * 사용자가 명시적으로 프로세스를 기동할 수 있는 수동 진입점을 제공한다 (2026-09-09 실측 피드백 반영, VS Code `magi.start` 대응 구현).
 *
 * 이미 데몬이 듣고 있으면 우클릭 하위 메뉴에서는 숨기고, 다른 자리(Find Action 등)에서는 비활성화한다.
 * 연결된 상태에서도 「띄우기」가 늘 서 있어 누르면 「이미 실행 중」 알림만 나던 것을 스크린샷에서 봤다(2026-09-28).
 * 소켓 확인은 연결 후 바로 닫는 한 번이라 BGT update 에서 싸다([DaemonClient.reach]).
 */
class StartDaemonAction : AnAction(), DumbAware {
    init { templatePresentation.icon = AllIcons.Actions.Execute }

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        e.presentation.text = MagiEditorMenu.item(e, "action.magi.startDaemon.text")
        val project = e.project
        val sock = project?.let { Workspace(it).socket() }
        val listening = sock != null && DaemonClient.reach(sock) is Reach.Listening
        e.presentation.isEnabled = project != null && !listening
        e.presentation.isVisible = project != null && !(listening && e.place == ActionPlaces.EDITOR_POPUP)
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        StartDaemon.byHand(project)
    }
}
