package dev.sayaya.magi.ide.ui

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.vcs.VcsDataKeys
import javax.swing.SwingUtilities

/**
 * 커밋 메시지 칸의 「magi: 초안」 — 이웃들이 전부 가진 그 단추다(SURVEY: 요소요소 진입점).
 *
 * 부르는 것은 데몬의 `git-msg`(`answerGitMsg` → `DraftCommit`): 스테이지된 변경에서, 워크스페이스
 * 템플릿의 하우스 스타일까지 얹어 짓는다 — 콘솔의 커밋 카드가 부르는 **같은 문**이라 두 화면이
 * 같은 초안을 본다. **칸의 글은 안 싣는다** — 와이어의 text 는 힌트가 아니라 저장된 템플릿을
 * 밀어내는 일회용 규칙 자리다(리뷰 실측 — `Companion.draftCommit` 의 사유).
 *
 * 실패는 커밋 칸에 안 쓴다 — 칸에 앉은 글은 커밋될 글이고, 에러 문장이 커밋 메시지가 되는 사고는
 * 화면이 만드는 최악의 거짓이다. 실패는 알림 풍선으로.
 */
class DraftCommitAction : AnAction() {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    /** 켜짐과 실행이 같은 증거를 본다 — 커밋 칸이 있어야 앉힐 곳이 있다. */
    override fun update(e: AnActionEvent) {
        // 글자는 **여기서** 못박는다: plugin.xml 의 번들 경로는 언어팩이 없을 때
        // JVM 기본 로케일로 새어 한국어가 뜬다(실측). MagiBundle 은 언어팩 유무로
        // 정하므로, 한 규칙으로 통일한다.
        e.presentation.text = MagiBundle.msg("action.magi.draftCommit.text")
        e.presentation.description = MagiBundle.msg("action.magi.draftCommit.description")
        e.presentation.isEnabledAndVisible =
            e.project != null && e.getData(VcsDataKeys.COMMIT_MESSAGE_CONTROL) != null
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val box = e.getData(VcsDataKeys.COMMIT_MESSAGE_CONTROL) ?: return
        val doc = e.getData(VcsDataKeys.COMMIT_MESSAGE_DOCUMENT)
        // 누른 순간의 칸(EDT). 모델 왕복은 수 초라 그동안 사람이 계속 치는 것이 보통 경로다 —
        // 착지 때 이 값과 다르면 덮지 않는다(사라지는 입력 없음 — 컴포저 제안의 그 가드).
        val before = doc?.text
        Workspace(project).onDaemon({ why -> tell(project, "초안을 못 받았다 — $why") }) { comp ->
            val r = comp.draftCommit()
            val draft = r.out
            when {
                !r.ok -> tell(project, "초안을 못 받았다 — ${r.error ?: "사유 없음"}")
                draft.isNullOrBlank() -> tell(project, "데몬이 빈 초안을 줬다 — 스테이지된 변경이 없을 수 있다")
                else -> SwingUtilities.invokeLater {
                    // 다이얼로그가 그새 닫혔으면(disposed) 조용히 죽는 대신 풍선으로 초안을 건넨다.
                    val landed = runCatching {
                        if (doc != null && doc.text != before) false
                        else { box.setCommitMessage(draft); true }
                    }.getOrDefault(false)
                    if (!landed) tell(project, "칸이 그새 바뀌어 덮지 않았다. 초안:\n$draft")
                }
            }
        }
    }

    private fun tell(project: com.intellij.openapi.project.Project, text: String) =
        NotificationGroupManager.getInstance().getNotificationGroup("magi")
            .createNotification(text, NotificationType.WARNING).notify(project)
}
