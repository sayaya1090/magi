package dev.sayaya.magi.ide.ui

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.wm.ToolWindowManager

/**
 * Run 콘솔 및 내장 터미널에서 선택된 출력 텍스트를 magi 에이전트에 직접 질의하는 액션.
 *
 * 선택된 텍스트를 마크다운 코드 블록으로 감싸 프롬프트 본문으로 전송하며,
 * 전송 후 하단 대화 도구 창을 자동으로 활성화하여 응답 스트림을 확인하도록 유도한다.
 */
class AskConsoleAction : AnAction() {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    /**
     * 액션이 등록된 위치별 고유 식별자. 동일 클래스가 Run 콘솔과 터미널에 각각 등록되므로
     * 식별자에 따라 개별 리소스 번들 텍스트를 분기 매핑한다.
     */
    private val myId: String by lazy {
        com.intellij.openapi.actionSystem.ActionManager.getInstance().getId(this) ?: "magi.askConsole"
    }

    override fun update(e: AnActionEvent) {
        // 언어팩 미설치 환경에서 JVM 기본 로케일로 폴백되는 문제를 방지하기 위해 MagiBundle 기반으로 명시적 설정한다.
        e.presentation.text = MagiBundle.msg("action.$myId.text")
        e.presentation.description = MagiBundle.msg("action.$myId.description")
        val sel = selection(e)
        e.presentation.isEnabledAndVisible = e.project != null && !sel.isNullOrBlank()
    }

    /**
     * 선택된 텍스트를 조회한다.
     * IntelliJ 2026.1 터미널 블록은 `CommonDataKeys.EDITOR` 대신 터미널 전용 데이터 키를 사용하므로
     * [TerminalDataContextUtils]를 통해 에디터 인스턴스를 획득한다.
     * 터미널 플러그인이 비활성화된 환경에서의 `NoClassDefFoundError` 전파를 방지하기 위해 `runCatching`으로 래핑한다.
     */
    private fun selection(e: AnActionEvent): String? =
        e.getData(CommonDataKeys.EDITOR)?.selectionModel?.selectedText ?: terminalSelection(e)

    private fun terminalSelection(e: AnActionEvent): String? = runCatching {
        with(org.jetbrains.plugins.terminal.block.util.TerminalDataContextUtils) {
            e.terminalEditor?.selectionModel?.selectedText
        }
    }.getOrNull()

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val raw = selection(e) ?: return
        // 대용량 콘솔 출력으로 인한 데몬 스캐너 한도 초과 및 연결 단절을 방지하기 위해 최대 64KB로 절단하고 안내 표식을 추가한다.
        // 해당 표식은 모델 지시용 페이로드이므로 화면 텍스트 리소스(MagiBundle)와 분리한다 (리뷰 R4).
        val sel = if (raw.length > 65_536) raw.take(65_536) + "\n" + CUT else raw
        val ask = ask(MagiBundle.locale()) + "\n```\n" + sel + "\n```"
        // 사용자 입력창의 대기 중인 파일 첨부 칩을 무단 소비하지 않도록 기본 전송 API를 호출한다.
        Workspace(project).onDaemon({ why -> tell(project, MagiBundle.msg("common.notsent", why)) }) { comp ->
            val r = comp.say(ask)
            if (!r.ok) tell(project, MagiBundle.msg("common.notsent", r.error ?: MagiBundle.msg("common.noreason")))
        }
        ToolWindowManager.getInstance(project).getToolWindow("magi")?.show()
    }

    internal companion object {
        /** 모델 지시용 영문 프롬프트 (UI 리소스 번들 분리 원칙). */
        const val ASK = "Explain what this output means, and how to fix it if it needs fixing."
        const val CUT = "…(the selection was long, so it was cut here)"

        /**
         * IDE 활성 언어팩 로케일에 맞춰 모델 응답 언어 지시문을 구성한다 (2026-09-01 실측 결함 개선).
         * 모델 추론 정확도를 위해 지시 자체는 영어로 전달하되, 영어가 아닌 경우에만 응답 언어(영어 명칭 기준)를 지정한다.
         */
        fun ask(where: java.util.Locale): String =
            if (where.language == java.util.Locale.ENGLISH.language) ASK
            else ASK + "\nAnswer in " + where.getDisplayLanguage(java.util.Locale.ENGLISH) + "."
    }

    private fun tell(project: com.intellij.openapi.project.Project, text: String) =
        NotificationGroupManager.getInstance().getNotificationGroup("magi")
            .createNotification(text, NotificationType.WARNING).notify(project)
}
