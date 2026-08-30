package dev.sayaya.magi.ide.ui

import com.intellij.openapi.editor.EditorFactory
import com.intellij.openapi.project.Project
import com.intellij.openapi.startup.ProjectActivity

/**
 * 타이핑 중 훑어보기의 **귀를 여는 자리**.
 *
 * 처음엔 파일 선택 이벤트에 귀를 달았는데, 그러면 **이미 열려 있던 파일**은 탭을 한 번
 * 갈아타기 전까지 아무도 안 듣는다 — 라이브에서 그 모양으로 배너가 안 떴다(문은 정상이었다).
 * 그래서 문서 멀티캐스터 하나에 붙는다: 프로젝트가 열리는 순간부터 모든 편집을 듣고,
 * 프로젝트가 닫히면 같이 죽는다(고아 리스너 금지).
 *
 * 여기서 **스트라이프 글자도 덮는다** — 사유는 아래.
 */
internal class LookStartup : ProjectActivity {
    override suspend fun execute(project: Project) {
        EditorFactory.getInstance().eventMulticaster.addDocumentListener(
            LookWhileTyping.Ears(project), LookWhileTyping.scope(project),
        )
        stripes(project)
    }

    /**
     * 도구창 버튼의 글자를 **[MagiBundle] 의 규칙으로** 다시 세운다.
     *
     * `plugin.xml` 의 `toolwindow.stripe.<id>` 는 플랫폼이 제 로케일로 읽는데, 그 로케일은
     * 언어팩이 없을 때 JVM 기본으로 샌다 — 이 저장소가 액션 글자에서 이미 데인 그 함정이고,
     * 액션은 `update()` 가 덮어서 빠져나온다. 스트라이프에는 그런 덮개가 없어서 영어 IDE 의
     * 오른쪽 독에 「magi 계획」이 뜰 수 있다(리뷰 R5). 그래서 여기 덮개를 둔다.
     *
     * 번들의 열쇠는 **그대로 남긴다**: 이 활동이 돌기 전(프로젝트 열리는 순간)에도 버튼은
     * 서 있고, 그때 id 가 날것으로 보이는 것보다 번역된 글자가 낫다.
     */
    private fun stripes(project: Project) {
        val mgr = com.intellij.openapi.wm.ToolWindowManager.getInstance(project)
        mgr.invokeLater {
            for (id in listOf("magi", "magi.plan")) {
                mgr.getToolWindow(id)?.stripeTitle = MagiBundle.msg("toolwindow.stripe.$id")
            }
        }
    }
}
