package dev.sayaya.magi.ide.ui

import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent

/**
 * 사람이 데몬을 띄우는 자리.
 *
 * **왜 필요한가.** 실사용 보고(2026-09-09): 윈도우에서 데몬이 죽고 소켓 파일만 남으면 플러그인이
 * 아무것도 못 했다. 그 판정 결함은 따로 고쳤지만, 고치고 나서 남은 사실이 이것이다 — **이 플러그인에
 * 데몬을 띄우는 액션이 없었다.** 액션 열둘 중 하나도 그 일을 안 했고, 자동 기동은 설정의 체크박스뿐
 * 이었다. 그래서 자동 기동이 어떤 이유로든 안 되는 자리(꺼 두었거나, 예산을 다 썼거나, 판정이
 * 「모름」이거나, 바이너리를 미뤘거나)에서 사람이 할 수 있는 일이 **없었다.**
 *
 * VS Code 쪽에는 `magi.start` 와 판 안의 「Start one」 단추가 처음부터 있었다. 이것은 그 짝이다.
 *
 * 판정을 안 본다. 판정은 **말하는 데** 쓰고 되살리기는 그것에 묶지 않는다 — 「모르는 것을 아는 척
 * 하지 않는다」는 원칙을 지키면서도 사람이 빠져나올 길을 남기는 자리가 여기다.
 */
class StartDaemonAction : AnAction() {
    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        StartDaemon.byHand(project)
    }
}
