package dev.sayaya.magi.ide.ui

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.vcs.VcsDataKeys
import javax.swing.SwingUtilities

/**
 * VCS 커밋 메시지 작성 패널(Commit 도구 창 및 다이얼로그)용 'magi: 초안 작성' 액션.
 *
 * 백엔드 데몬의 `git-msg` 엔드포인트(`answerGitMsg` → `DraftCommit`)를 호출하여,
 * 현재 스테이징된 git 변경사항과 프로젝트 커밋 템플릿 스타일을 반영한 커밋 메시지 초안을 자동 생성한다.
 * 실패 시 커밋 메시지 입력란을 오염시키지 않고 IDE 알림 풍선으로 오류를 안내한다.
 */
class DraftCommitAction : AnAction() {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    /** 커밋 메시지 입력 컨트롤이 존재하는 경우에만 활성화한다. */
    override fun update(e: AnActionEvent) {
        e.presentation.text = MagiBundle.msg("action.magi.draftCommit.text")
        e.presentation.description = MagiBundle.msg("action.magi.draftCommit.description")
        e.presentation.isEnabledAndVisible =
            e.project != null && e.getData(VcsDataKeys.COMMIT_MESSAGE_CONTROL) != null
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val box = e.getData(VcsDataKeys.COMMIT_MESSAGE_CONTROL) ?: return
        val doc = e.getData(VcsDataKeys.COMMIT_MESSAGE_DOCUMENT)
        // 요청 시점의 기존 텍스트 스냅샷. 모델 응답 대기 중 사용자가 입력을 진행한 경우 덮어쓰지 않고 보존한다.
        val before = doc?.text
        Workspace(project).onDaemon({ why -> tell(project, MagiBundle.msg("draft.notgot", why)) }) { comp ->
            val r = comp.draftCommit()
            val draft = r.out
            when {
                !r.ok -> tell(project, MagiBundle.msg("draft.notgot", r.error ?: MagiBundle.msg("common.noreason")))
                draft.isNullOrBlank() -> tell(project, MagiBundle.msg("draft.empty"))
                else -> SwingUtilities.invokeLater {
                    // 다이얼로그가 닫혔거나 사용자가 내용을 수정한 경우 생성된 초안을 알림 풍선으로 전달한다.
                    // **상자는 사람 것이다.** 두 가지를 다 막는다: 기다리는 사이에 사람이 고친
                    // 경우(`doc.text != before`)와, **부를 때 이미 글자가 있던 경우**. 뒤엣것을
                    // 안 막는 동안 세 줄 써 두고 이 액션을 눌러 본 사람은 그 세 줄을 잃었다 —
                    // 눌렀다는 것이 「지금 쓴 것을 버려라」는 뜻은 아니다.
                    //
                    // 짝인 VS Code 가 같은 자리에 규칙을 적어 뒀다: *"Never overwrite. If they
                    // started typing, the draft goes to a notification instead — the box is
                    // theirs."* 초안은 사라지지 않고 풍선으로 간다.
                    val landed = runCatching {
                        if (doc != null && doc.text != before) false
                        else if (!before.isNullOrBlank()) false
                        else { box.setCommitMessage(draft); true }
                    }.getOrDefault(false)
                    if (!landed) tell(project, MagiBundle.msg("draft.moved", draft))
                }
            }
        }
    }

    private fun tell(project: com.intellij.openapi.project.Project, text: String) =
        NotificationGroupManager.getInstance().getNotificationGroup("magi")
            .createNotification(text, NotificationType.WARNING).notify(project)
}
