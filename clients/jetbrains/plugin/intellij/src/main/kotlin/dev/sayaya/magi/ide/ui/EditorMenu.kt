package dev.sayaya.magi.ide.ui

import com.intellij.openapi.actionSystem.ActionPlaces
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.DefaultActionGroup

/**
 * 에디터 우클릭 컨텍스트 메뉴 내 magi 하위 그룹([DefaultActionGroup]).
 *
 * 개별 액션들을 단일 하위 메뉴로 그룹화하여 컨텍스트 메뉴의 시각적 과밀을 방지한다.
 * Find Action 및 Keymap 검색 시에는 플러그인 식별을 위해 `magi: ` 접두사를 유지하고,
 * 하위 메뉴 내부 렌더링 시에만 접두사를 동적으로 제거하여 시각적 중복을 방지한다 ([item]).
 */
internal class MagiEditorMenu : DefaultActionGroup(), com.intellij.openapi.project.DumbAware {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        e.presentation.text = MagiBundle.msg("group.magi.editorMenu.text")
        // 파일이 없는 에디터(diff 뷰어, 콘솔 등)에서 자식 액션이 모두 숨겨져 빈 서브메뉴만 남는 현상을 방지하기 위해
        // 자식 액션과 동일한 가상 파일 존재 조건을 검사한다 (IntelliJ 2026.1 hideIfNoVisibleChildren 삭제 대응, 리뷰 R7).
        e.presentation.isEnabledAndVisible = e.project != null &&
            e.getData(com.intellij.openapi.actionSystem.CommonDataKeys.VIRTUAL_FILE) != null
    }

    companion object {
        /**
         * 컨텍스트에 따라 액션 텍스트를 반환한다.
         * 에디터 팝업 메뉴 내부에서는 중복된 `magi: ` 접두사를 제거하여 간결하게 표출한다.
         */
        fun item(e: AnActionEvent, key: String): String {
            val full = MagiBundle.msg(key)
            return if (e.place == ActionPlaces.EDITOR_POPUP) full.removePrefix("magi: ") else full
        }
    }
}
