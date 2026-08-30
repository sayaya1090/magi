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
 */
internal class LookStartup : ProjectActivity {
    override suspend fun execute(project: Project) {
        EditorFactory.getInstance().eventMulticaster.addDocumentListener(
            LookWhileTyping.Ears(project), LookWhileTyping.scope(project),
        )
    }
}
