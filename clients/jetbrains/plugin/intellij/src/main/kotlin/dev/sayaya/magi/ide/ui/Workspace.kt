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
 * 이 프로젝트의 데몬에 닿는 길. 창 둘이 같이 쓴다.
 *
 * 창이 둘이 된 것은 배치 때문이다(설계 문서 §5 "어디에 놓나") — 대화는 하단 독, 사실 판은 우측.
 * 그 전에는 이 배선이 창 하나 안에 있었고, 둘째 창을 만들면서 복사하면 **같은 규칙이 두 곳에**
 * 생긴다. 이 트리가 오늘 하루 종일 고친 것이 그 결함이라 여기로 뺀다.
 */
internal class Workspace(private val project: Project) {

    /** 이 프로젝트의 소켓. 심링크를 푸는 자리는 SocketPath 안이다(§2). */
    fun socket() = project.basePath?.let { SocketPath.of(SocketPath.configDir(), Paths.get(it)) }

    /**
     * 컴패니언이 **못 만지는** 컨텐트 루트들. 없으면 빈 목록.
     *
     * magi 의 워크스페이스는 디렉토리 하나이고 파일 툴이 거기 갇힌다 — 밖을 짚으면
     * `"%s is outside this workspace"` 로 거절한다(`internal/app/query.go`). 그런데 IntelliJ 의
     * 컨텐트 루트는 `basePath` 밖에 있을 수 있다. 실측했다(§8): `.iml` 둘짜리 프로젝트에서 하나가
     * `basePath` 아래, 하나가 완전히 밖이었다.
     *
     * 그러면 사람은 Project 뷰에서 그 파일을 **보면서** 컴패니언에게 시킬 수 없고, 거절 문장은
     * IDE 가 왜 그것을 보여 주는지 설명하지 않는다. 화면과 에이전트가 서로 다른 워크스페이스를
     * 믿는 상태다. **거절이 오기 전에 말하는 것**이 §0.5-7 이 요구하는 모양이라 여기서 센다.
     *
     * **부를 때마다 센다.** 컨텐트 루트는 세션 중에 바뀐다(Project Structure 에서 더하고 뺀다).
     * 그리고 읽기 락 안에서 세므로 **어느 스레드에서 불러도 된다** — 이건 편의가 아니라 자물쇠다.
     * 풀 스레드에서 못 부르면 부르는 쪽이 "그럼 열 때 한 번 세어 필드에 두자"로 가고, 그렇게 적어
     * 둔 값은 사람이 루트를 고쳐도 안 변한다. 실제로 두 자리가 그 모양이었다.
     *
     * 경로 비교로만 판정한다 — 심링크는 풀지 않는다. 이 목록은 사람에게 보여 줄 말이지 툴 게이트가
     * 아니고, 진짜 판정은 코어가 자기 규칙으로 한다. 여기서 흉내내면 **두 번째 표현**이 생긴다.
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
     * 데몬에 한 번 붙어 무언가 하고 끊는다. 연결을 들고 있지 않는 이유는 스트림이 아직 없어서다 —
     * 전사 문이 생기면 그때 스트림 하나를 usecase 가 단독으로 소유한다(§3).
     *
     * 못 붙으면 [trouble] 로 **말한다.** 빈 화면은 "할 일 없음"처럼 보이는데 사실은 "모른다"이고,
     * 이 트리는 그 둘을 구분한다(§0.5-7).
     */
    fun onDaemon(trouble: (String) -> Unit, work: (Companion) -> Unit) = onDaemon(null, trouble, work)

    /**
     * **대화를 안 고르고** 붙는다 — 목록·새 대화·갈아타기처럼 대화가 없어도 되는 일들.
     *
     * 이 자리가 없어서 「대화 탭 열기」가 정작 필요한 순간에 안 됐다(사용자 실측): 데몬이 아직
     * 대화를 공표하지 않았으면 [onDaemon] 이 붙기도 전에 거절했고, 화면에는 **목록을 청했는데
     * 대화 얘기**가 떴다 — "Could not get the chat list — magi has not said which chat it is in".
     * 목록은 「어느 대화인지 모를 때」 부르는 문이다. 그것을 「어느 대화인지 알아야」 열게 두면
     * 필요한 순간에만 잠긴다.
     *
     * 여기서 나온 컴패니언은 **대화 문을 못 쓴다** — `Companion.send` 가 사유를 실어 거절한다.
     */
    fun onDaemonWithoutChat(trouble: (String) -> Unit, work: (Companion) -> Unit) =
        connect(null, needChat = false, trouble, work)

    /**
     * [at] 를 주면 공표된 현재 대신 **그 대화**에 붙는다 — 고정 탭의 문이다. 기본형과 오버로드로
     * 가른 이유: 꼬리의 기본값 인자는 트레일링 람다를 빼앗는다(람다는 **마지막** 파라미터에만
     * 붙는다) — 실제로 `onDaemon({}) { … }` 호출 전부가 깨졌다.
     */
    fun onDaemon(at: String?, trouble: (String) -> Unit, work: (Companion) -> Unit) =
        connect(at, needChat = true, trouble, work)

    private fun connect(
        at: String?,
        needChat: Boolean,
        trouble: (String) -> Unit,
        work: (Companion) -> Unit,
    ) {
        val sock = socket() ?: return trouble(MagiBundle.msg("chat.noworkspace"))
        ApplicationManager.getApplication().executeOnPooledThread {
            SocketPath.tooLong(sock)?.let { return@executeOnPooledThread trouble(it) }
            try {
                // **붙어 보고 나서 진단한다.** 전에는 공표 파일이 없으면 붙기도 전에
                // 「어느 대화인지 안 알려 줬다」로 끝냈는데, 데몬이 아예 안 돌 때도 공표 파일은
                // 없다 — 그래서 꺼져 있는 데몬이 늘 대화 공표 탓으로 보고됐다. 아래 catch 가
                // 「안 켰다 / 죽었다 / 끊겼다」를 가려 주는데, 그 자리에 닿지를 못했다.
                // 확인하기 전에 원인을 대지 않는다.
                DaemonClient.connect(sock).use { client ->
                    // 세션 id 는 데몬이 공표한 것을 그대로 쓴다. "이 워크스페이스의 최신"으로
                    // 고르면 며칠 도는 데몬에서 그사이 누가 연 대화를 연다(daemon.go 의 사유).
                    // at 를 이미 이름 댄 경로(고정 탭)는 공표를 안 본다 — 「넘겨짚지 않는다」는
                    // 자리를 모를 때의 규칙이지, 이름 댄 자리를 막는 규칙이 아니다(리뷰).
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
                    // `else` 를 안 쓴다. 판정이 하나 늘면 여기서 컴파일이 서는 것이, 새 갈래가
                    // 옛 문장 뒤에 조용히 숨는 것보다 싸다.
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
