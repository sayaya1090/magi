package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.application.runReadActionBlocking
import com.intellij.openapi.module.ModuleManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.roots.ModuleRootManager
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.transport.Published
import dev.sayaya.magi.ide.transport.SocketDaemons
import dev.sayaya.magi.ide.transport.SocketPath
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.DaemonLifecycle
import java.nio.file.Paths

/**
 * 프로젝트별 백엔드 데몬 소켓 경로 및 워크스페이스 연결 관리자.
 *
 * 하단 도구 창(MagiToolWindow, 대화)과 우측 도구 창(PlanToolWindow, 계획/현황)이 공통으로 참조한다 (설계 문서 §5).
 * 데몬 소켓 해석, 워크스페이스 경계 검사, 생명주기 진단 로직의 중복을 방지하고 일관된 통신 인터페이스를 제공한다.
 */
internal class Workspace(private val project: Project) {

    /**
     * 작업 영역 데몬 소켓 파일 경로를 반환한다.
     * 환경 변수(`MAGI_CONFIG_DIR`) 불일치를 방지하기 위해 사용자 로그인 셸 환경을 우선 조회한다 ([Shell.configDir]).
     */
    fun socket() = project.basePath?.let { SocketPath.of(Shell.configDir(), Paths.get(it)) }

    /**
     * 프로젝트 모듈 컨텐트 루트 중 데몬 작업 디렉토리([Project.getBasePath]) 외부에 위치한 경로 목록을 반환한다.
     *
     * 데몬의 파일 작업 도구는 보안을 위해 `basePath` 디렉토리 내부로 제한되며, 외부는 거절 처리된다 (`internal/app/query.go`).
     * IntelliJ 프로젝트 구성에 따라 모듈 컨텐트 루트가 프로젝트 베이스 외부에 존재할 수 있으므로 (§8 실측),
     * 사용자가 IDE 상에서는 보이지만 에이전트가 수정할 수 없는 파일에 대해 혼선을 겪지 않도록 사전에 안내한다 (§0.5-7).
     *
     * 모듈 설정 변경을 반영하기 위해 매 호출 시 ReadAction 컨텍스트에서 동적으로 수집한다.
     */
    fun rootsOutsideWorkspace(): List<String> {
        val base = project.basePath ?: return emptyList()
        val basePath = Paths.get(base).normalize()
        return runReadActionBlocking {
            ModuleManager.getInstance(project).modules
                .flatMap { ModuleRootManager.getInstance(it).contentRoots.asList() }
                .map { Paths.get(it.path).normalize() }
                .filterNot { it.startsWith(basePath) }
                .map { it.toString() }
                .distinct()
                .sorted()
                }
    }

    /**
     * 데몬에 연결하여 지정된 작업을 실행한 후 연결을 종료한다.
     * 연결 또는 세션 오류 발생 시 [trouble] 콜백으로 오류 진단 메시지를 전달한다.
     */
    fun onDaemon(trouble: (String) -> Unit, work: (Companion) -> Unit) = onDaemon(null, trouble, work)

    /**
     * 활성 대화 세션 유무와 무관하게 데몬에 연결한다 (세션 목록 조회, 새 세션 시작 등 대화 세션 미지정 작업용).
     *
     * 공표된 활성 세션이 없는 초기 상태에서도 세션 목록 조회가 정상 동작하도록 보장한다.
     * 본 메서드로 획득한 [Companion]은 대화 전송 API 호출이 제한된다.
     */
    fun onDaemonWithoutChat(trouble: (String) -> Unit, work: (Companion) -> Unit) =
        connect(null, needChat = false, trouble, work)

    /**
     * 3초 주기 상태 폴링용 연결.
     *
     * 대화 추론 대기 타임아웃(2분) 대신 단기 타임아웃([DaemonClient.PATIENCE_POLL])을 적용하여,
     * 데몬 무응답 시 폴링 작업자 스레드가 백그라운드 풀에 누적되는 것을 방지한다.
     * 트레일링 람다 문법 호환성을 유지하기 위해 독립 메서드 오버로드로 제공한다.
     */
    fun onDaemonPolling(trouble: (String) -> Unit, work: (Companion) -> Unit) =
        connect(null, needChat = false, trouble, work, DaemonClient.PATIENCE_POLL)

    /**
     * 특정 대화 세션 식별자([at])를 지정하여 연결한다 (고정 탭 작업용).
     * 기본형과 파라미터를 분리하여 트레일링 람다 문법을 유지한다.
     */
    fun onDaemon(at: String?, trouble: (String) -> Unit, work: (Companion) -> Unit) =
        connect(at, needChat = true, trouble, work)

    private fun connect(
        at: String?,
        needChat: Boolean,
        trouble: (String) -> Unit,
        work: (Companion) -> Unit,
        patienceMs: Long = DaemonClient.PATIENCE_ASK,
    ) {
        val sock = socket() ?: return trouble(MagiBundle.msg("chat.noworkspace"))
        ApplicationManager.getApplication().executeOnPooledThread {
            SocketPath.tooLong(sock)?.let { return@executeOnPooledThread trouble(it) }
            try {
                // 공표 파일 유무로 판단하지 않고 실제 소켓 연결을 먼저 시도하여
                // 데몬 미기동, 비정상 종료, 연결 해제 상태를 정확히 진단한다.
                DaemonClient.connect(sock, patienceMs).use { client ->
                    // 세션 ID는 데몬이 공표한 현재 세션을 참조하되, 명시적 세션(at)이 지정된 경우 이를 우선 적용한다.
                    val sid = at ?: Published.of(sock)?.session
                    if (needChat && sid.isNullOrBlank()) {
                        trouble(MagiBundle.msg("chat.nosession"))
                        return@use
                    }
                    work(Companion(client, sid.orEmpty()))
                }
            } catch (e: Exception) {
                val v = DaemonLifecycle(sock, start = {}, daemons = SocketDaemons).verdict()
                trouble(
                    // 누락 방지를 위해 when 구문의 모든 분기를 명시적으로 매핑한다.
                    when (v) {
                        is DaemonLifecycle.Verdict.Left -> MagiBundle.msg("chat.daemon.left")
                        is DaemonLifecycle.Verdict.Killed -> MagiBundle.msg("chat.daemon.killed")
                        is DaemonLifecycle.Verdict.Alive -> MagiBundle.msg("chat.daemon.dropped", e.message ?: MagiBundle.msg("common.noreason"))
                        is DaemonLifecycle.Verdict.Unknown -> MagiBundle.msg("chat.daemon.unknown", v.why)
                    }
                )
            }
        }
    }
}
