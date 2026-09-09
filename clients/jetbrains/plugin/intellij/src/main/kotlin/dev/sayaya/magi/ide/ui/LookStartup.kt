package dev.sayaya.magi.ide.ui

import com.intellij.openapi.editor.EditorFactory
import com.intellij.openapi.project.Project
import com.intellij.openapi.startup.ProjectActivity

/**
 * 타이핑 중 코드 검토(LookWhileTyping) 리스너 등록 및 프로젝트 시작 초기화 작업([ProjectActivity]).
 *
 * 파일 선택 이벤트(FileEditorManagerListener) 대신 EditorFactory 이벤트 멀티캐스터(eventMulticaster)에 등록하여,
 * 프로젝트 오픈 시점에 이미 열려 있던 기존 에디터 버퍼의 편집 이벤트가 누락되는 현상을 방지한다.
 * 리스너는 프로젝트 범위의 Disposable 스코프에 등록되어 프로젝트 종료 시 자동 해제된다.
 */
internal class LookStartup : ProjectActivity {
    override suspend fun execute(project: Project) {
        EditorFactory.getInstance().eventMulticaster.addDocumentListener(
            LookWhileTyping.Ears(project), LookWhileTyping.scope(project),
        )
        stripes(project)
        // 프로젝트 워크스페이스가 초기화되었으므로 자동 기동 옵션 확인 후 필요 시 데몬을 기동한다.
        StartDaemon.ifAbsent(project)
    }

    /**
     * 도구 창 스트라이프(Stripe) 버튼 타이틀을 [MagiBundle] 번들 리소스로 명시적 갱신한다.
     *
     * `plugin.xml`의 `toolwindow.stripe.<id>` 키는 언어팩 미설치 환경에서 플랫폼 기본 로케일(JVM fallback)로 인해
     * 영문 IDE 환경에서 한국어 타이틀이 표출되는 현상(리뷰 R5)을 방지하기 위해 런타임에 동기화한다.
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
