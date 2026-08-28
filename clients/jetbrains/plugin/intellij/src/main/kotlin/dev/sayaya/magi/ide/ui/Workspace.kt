package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
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
     * 경로 비교로만 판정한다 — 심링크는 풀지 않는다. 이 목록은 사람에게 보여 줄 말이지 툴 게이트가
     * 아니고, 진짜 판정은 코어가 자기 규칙으로 한다. 여기서 흉내내면 **두 번째 표현**이 생긴다.
     */
    fun rootsOutsideWorkspace(): List<String> {
        val base = project.basePath ?: return emptyList()
        val basePath = Paths.get(base).normalize()
        return ModuleManager.getInstance(project).modules
            .flatMap { ModuleRootManager.getInstance(it).contentRoots.asList() }
            .map { Paths.get(it.path).normalize() }
            .filterNot { it.startsWith(basePath) }
            .map { it.toString() }
            .distinct()
            .sorted()
    }

    /**
     * 데몬에 한 번 붙어 무언가 하고 끊는다. 연결을 들고 있지 않는 이유는 스트림이 아직 없어서다 —
     * 전사 문이 생기면 그때 스트림 하나를 usecase 가 단독으로 소유한다(§3).
     *
     * 못 붙으면 [trouble] 로 **말한다.** 빈 화면은 "할 일 없음"처럼 보이는데 사실은 "모른다"이고,
     * 이 트리는 그 둘을 구분한다(§0.5-7).
     */
    fun onDaemon(trouble: (String) -> Unit, work: (Companion) -> Unit) {
        val sock = socket() ?: return trouble("이 프로젝트에는 경로가 없어 워크스페이스를 정할 수 없다.")
        ApplicationManager.getApplication().executeOnPooledThread {
            SocketPath.tooLong(sock)?.let { return@executeOnPooledThread trouble(it) }
            try {
                // 세션 id 는 데몬이 공표한 것을 그대로 쓴다. "이 워크스페이스의 최신"으로 고르면
                // 며칠 도는 데몬에서 그사이 누가 연 대화를 연다(daemon.go 의 사유).
                val sid = Published.of(sock)?.session
                // 중괄호가 장식이 아니다. 이 줄이 한때 `if (…) return@executeOnPooledThread` 로
                // 끝나고 `trouble(…)` 이 다음 줄에 더 들여쓴 채 있었는데, 코틀린은 그것을 **별개
                // 문장**으로 읽는다. 그래서 정상일 때마다 "데몬 없음"을 말한 다음 이어서 성공했다 —
                // 메시지가 정확히 거꾸로였다. 실측: 폴 46회 전부 trouble 과 ok 가 같은 밀리초에.
                if (sid.isNullOrBlank()) {
                    trouble("데몬이 어느 대화에 있는지 공표하지 않았다 — 붙을 자리를 넘겨짚지 않는다.")
                    return@executeOnPooledThread
                }
                DaemonClient.connect(sock).use { work(Companion(it, sid)) }
            } catch (e: Exception) {
                val v = DaemonLifecycle(sock, start = {}, daemons = SocketDaemons).verdict()
                trouble(
                    // `else` 를 안 쓴다. 판정이 하나 늘면 여기서 컴파일이 서는 것이, 새 갈래가
                    // 옛 문장 뒤에 조용히 숨는 것보다 싸다.
                    when (v) {
                        is DaemonLifecycle.Verdict.Left -> "데몬이 없다 — 아직 안 켰거나 질서 있게 나갔다."
                        is DaemonLifecycle.Verdict.Killed -> "소켓은 있는데 아무도 안 듣는다 — 죽은 것으로 보인다."
                        is DaemonLifecycle.Verdict.Alive -> "붙었다가 끊겼다: ${e.message}"
                        is DaemonLifecycle.Verdict.Unknown -> "데몬이 살았는지 물어볼 수가 없었다: ${v.why}"
                    }
                )
            }
        }
    }
}
