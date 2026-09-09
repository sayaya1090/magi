package dev.sayaya.magi.ide.ui

import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent

/**
 * 사용자가 수동으로 magi 데몬 프로세스를 기동하는 액션 ([StartDaemon.byHand]).
 *
 * 자동 기동 설정이 비활성화되었거나 재시작 예산([Restarts]) 소진, 데몬 상태 판정 불확실 등 자동 복구가 불가능한 상황에서
 * 사용자가 명시적으로 프로세스를 기동할 수 있는 수동 진입점을 제공한다 (2026-09-09 실측 피드백 반영, VS Code `magi.start` 대응 구현).
 */
class StartDaemonAction : AnAction() {
    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        StartDaemon.byHand(project)
    }
}
