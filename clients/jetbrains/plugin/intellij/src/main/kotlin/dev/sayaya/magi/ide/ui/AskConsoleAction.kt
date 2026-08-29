package dev.sayaya.magi.ide.ui

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.wm.ToolWindowManager

/**
 * Run 콘솔 출력에서 「magi: 이 출력 물어보기」 — 스택트레이스와 빌드 에러가 사는 자리에
 * 진입점을 둔다(이웃 셋 전부의 표준 동선; SURVEY 요소요소 진입점).
 *
 * 선택한 출력은 파일이 아니라 **그 순간의 글**이라 refs 가 아니라 프롬프트 본문으로 실린다 —
 * 코드 펜스에 담아, 무엇에 대한 질문인지 에이전트가 알게. 보낸 것의 증거는 전사 행이다(성공
 * 혼잣말 금지 규칙 그대로); 대화 창을 같이 열어 답이 오는 자리를 보인다.
 */
class AskConsoleAction : AnAction() {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        val sel = e.getData(CommonDataKeys.EDITOR)?.selectionModel?.selectedText
        e.presentation.isEnabledAndVisible = e.project != null && !sel.isNullOrBlank()
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val raw = e.getData(CommonDataKeys.EDITOR)?.selectionModel?.selectedText ?: return
        // 콘솔 선택은 수 MB 도 된다 — 데몬 스캐너 줄 한도에 끊기면 사유가 "연결 끊김"으로
        // 부정확해진다. 자르고 잘랐다고 말한다.
        val sel = if (raw.length > 65_536) raw.take(65_536) + "\n…(선택이 길어 잘라 보냄)" else raw
        val ask = "이 출력이 무슨 뜻인지, 고쳐야 하면 어떻게 고칠지 설명해줘:\n```\n$sel\n```"
        // 컴포저에 서 있는 첨부 칩은 여기 안 실린다(say 의 refs 기본값 빈 목록) — 칩은 "다음
        // 수동 전송"의 상태이고, 이 질문이 그것을 소리 없이 소비하면 모르는 부수효과다.
        Workspace(project).onDaemon({ why -> tell(project, "안 갔다 — $why") }) { comp ->
            val r = comp.say(ask)
            if (!r.ok) tell(project, "안 갔다 — ${r.error ?: "사유 없음"}")
        }
        ToolWindowManager.getInstance(project).getToolWindow("magi")?.show()
    }

    private fun tell(project: com.intellij.openapi.project.Project, text: String) =
        NotificationGroupManager.getInstance().getNotificationGroup("magi")
            .createNotification(text, NotificationType.WARNING).notify(project)
}
