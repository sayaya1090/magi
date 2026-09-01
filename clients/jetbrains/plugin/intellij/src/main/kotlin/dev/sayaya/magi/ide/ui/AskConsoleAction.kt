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

    /**
     * 이 인스턴스가 등록된 **자리의 id** — 같은 클래스가 Run 콘솔과 터미널 두 자리에 서고,
     * 자리마다 id 가 다르면 글자도 다르다(SDK Action System: 자리마다 고유 id). XML 등록은
     * 자리마다 인스턴스를 따로 만드므로 이 값은 인스턴스당 하나다.
     */
    private val myId: String by lazy {
        com.intellij.openapi.actionSystem.ActionManager.getInstance().getId(this) ?: "magi.askConsole"
    }

    override fun update(e: AnActionEvent) {
        // 글자는 **여기서** 못박는다: plugin.xml 의 번들 경로는 언어팩이 없을 때
        // JVM 기본 로케일로 새어 한국어가 뜬다(실측). MagiBundle 은 언어팩 유무로
        // 정하므로, 한 규칙으로 통일한다.
        e.presentation.text = MagiBundle.msg("action.$myId.text")
        e.presentation.description = MagiBundle.msg("action.$myId.description")
        val sel = selection(e)
        e.presentation.isEnabledAndVisible = e.project != null && !sel.isNullOrBlank()
    }

    /**
     * 선택된 출력. **터미널은 제 에디터를 `CommonDataKeys.EDITOR` 로 안 내놓는다** — 제 키를
     * 쓰고, 없을 때만 그리로 떨어진다(2026.1 `TerminalDataContextUtils` 바이트코드 실측).
     * 그래서 이 액션이 터미널에서 **한 번도 안 보였다**: 그룹 등록은 맞았고 집는 손이 틀렸다.
     * 사용자가 「선택하고도 안 보임」으로 잡았다.
     *
     * 터미널 쪽 손은 **없을 때만** 부른다 — 콘솔에서는 첫 줄이 답하므로, 터미널을 끈 IDE 에서는
     * 저 클래스를 로드할 일이 아예 없다. 그래도 감싼다: 없는 플러그인의 클래스를 부르는 것은
     * 예외가 아니라 `NoClassDefFoundError` 이고, 그것이 `update` 를 타고 나가면 남의 메뉴가
     * 통째로 깨진다.
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
        // 콘솔 선택은 수 MB 도 된다 — 데몬 스캐너 줄 한도에 끊기면 사유가 "연결 끊김"으로
        // 부정확해진다. 자르고 잘랐다고 말한다.
        // **번들에 안 둔다.** 이 두 글자는 어디에도 안 그려진다 — 전부 `say()` 의 페이로드,
        // 즉 모델에게 가는 지시다. 번들은 「화면 글자」로 광고된 파일이라, 거기 두면 번역을
        // 다듬는 손이 모델 지시를 바꾼다(모르는 부수효과 — 리뷰 R4). 잘림 표식도 마찬가지다:
        // 그건 사람이 아니라 모델이 「뒤가 더 있다」로 읽는 신호다.
        val sel = if (raw.length > 65_536) raw.take(65_536) + "\n" + CUT else raw
        val ask = ask(MagiBundle.locale()) + "\n```\n" + sel + "\n```"
        // 컴포저에 서 있는 첨부 칩은 여기 안 실린다(say 의 refs 기본값 빈 목록) — 칩은 "다음
        // 수동 전송"의 상태이고, 이 질문이 그것을 소리 없이 소비하면 모르는 부수효과다.
        Workspace(project).onDaemon({ why -> tell(project, MagiBundle.msg("common.notsent", why)) }) { comp ->
            val r = comp.say(ask)
            if (!r.ok) tell(project, MagiBundle.msg("common.notsent", r.error ?: MagiBundle.msg("common.noreason")))
        }
        ToolWindowManager.getInstance(project).getToolWindow("magi")?.show()
    }

    internal companion object {
        /** 전선으로 나가는 글자 — 화면이 아니다(위 주석). 번들로 옮기지 말 것. */
        const val ASK = "Explain what this output means, and how to fix it if it needs fixing."
        const val CUT = "…(the selection was long, so it was cut here)"

        /**
         * **답할 언어는 IDE 를 따른다.**
         *
         * 이 지시는 오래 한국어 한 줄이었다. 그래서 영문 IDE 에서도 답이 한국어로 왔다 —
         * 사람이 잡았다(2026-09-01): 「답이 나온다. 그런데 한글인 게 찜찜하다」. 화면 글자는
         * 언어팩을 따르는데 **모델에게 가는 지시만 안 따르고 있었다.**
         *
         * 지시 자체는 영어로 둔다. 모델은 영어 지시를 가장 잘 알아듣고, 이 문장은 사람이 볼
         * 것이 아니다. 대신 **답할 언어만** 붙인다 — 그것이 사람이 읽을 부분이라서다.
         * 영어면 아무것도 안 붙인다: 안 붙여도 영어로 오는데 한 줄을 더 보내면 그만큼이
         * 매번 실린다.
         *
         * 언어 이름은 영어로 적는다(`Locale.ENGLISH` 기준). 모델에게 「한국어로 답해」라고
         * 한국어로 말하면, 그 말을 못 읽는 모델에게는 지시가 아니라 잡음이다.
         */
        fun ask(where: java.util.Locale): String =
            if (where.language == java.util.Locale.ENGLISH.language) ASK
            else ASK + "\nAnswer in " + where.getDisplayLanguage(java.util.Locale.ENGLISH) + "."
    }

    private fun tell(project: com.intellij.openapi.project.Project, text: String) =
        NotificationGroupManager.getInstance().getNotificationGroup("magi")
            .createNotification(text, NotificationType.WARNING).notify(project)
}
