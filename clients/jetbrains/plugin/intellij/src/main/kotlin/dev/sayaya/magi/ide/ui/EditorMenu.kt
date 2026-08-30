package dev.sayaya.magi.ide.ui

import com.intellij.openapi.actionSystem.ActionPlaces
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.DefaultActionGroup

/**
 * 편집기 우클릭 메뉴 안의 **magi 하위 메뉴**.
 *
 * 넷이 남의 메뉴 바닥에 줄줄이 붙어 있었다. 이웃(Git·Local History)도, 어시스턴트 플러그인들도
 * 하위 메뉴 하나로 접는다 — 남의 메뉴에 우리 항목 넷을 평평하게 까는 것은 그 메뉴를 쓰는
 * 사람의 비용이다. 접으면 항목마다 붙이던 `magi: ` 접두도 필요 없어진다: 어느 메뉴에 있는지를
 * 메뉴 이름이 말한다.
 *
 * 접두를 **값에서 지우지는 않는다.** Find Action 과 Keymap 은 이 항목들을 메뉴 밖에서 보이고,
 * 거기서 「Review This File」 은 어느 플러그인 것인지 알 수 없다. 그래서 접두는 번들에 그대로
 * 두고, **이 메뉴 안에서 그릴 때만** 벗긴다([item]).
 */
internal class MagiEditorMenu : DefaultActionGroup(), com.intellij.openapi.project.DumbAware {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        // 글자를 여기서 못박는 사유는 액션들과 같다 — plugin.xml 의 번들 경로는 언어팩이
        // 없을 때 JVM 기본 로케일로 샌다(MagiBundle 의 주석).
        e.presentation.text = MagiBundle.msg("group.magi.editorMenu.text")
        // **자식과 같은 전제로 선다.** 프로젝트만 보면, 파일 없는 편집기(diff 뷰어·콘솔)에서
        // 자식 넷이 전부 숨고 **빈 「magi」 하위 메뉴만** 남는다 — 넷을 평평하게 깔던 소음을
        // 없애러 가서 빈 메뉴 하나를 놓고 오는 것이다. 2026.1 에는 이걸 구제할 플랫폼 장치가
        // 없다(`ActionGroup.hideIfNoVisibleChildren` 은 삭제됐고, 빈 메뉴엔 자리표시자가 선다).
        // 그래서 전제를 자식과 맞춘다(리뷰 R7 — 그 「파일 없는 편집기」는 이 저장소가 이미
        // 실측해 `AttachToChatAction` 주석에 적어 둔 자리다).
        e.presentation.isEnabledAndVisible = e.project != null &&
            e.getData(com.intellij.openapi.actionSystem.CommonDataKeys.VIRTUAL_FILE) != null
    }

    companion object {
        /**
         * 이 자리에서 그릴 글자. 하위 메뉴 안이면 `magi: ` 접두를 벗긴다 — 메뉴 이름이 이미
         * 그 말을 했고, 같은 낱말을 두 번 읽히는 것은 자리의 낭비다.
         */
        fun item(e: AnActionEvent, key: String): String {
            val full = MagiBundle.msg(key)
            return if (e.place == ActionPlaces.EDITOR_POPUP) full.removePrefix("magi: ") else full
        }
    }
}
