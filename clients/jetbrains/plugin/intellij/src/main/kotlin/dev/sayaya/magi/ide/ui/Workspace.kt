package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
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
                if (sid.isNullOrBlank()) return@executeOnPooledThread
                    trouble("데몬이 어느 대화에 있는지 공표하지 않았다 — 붙을 자리를 넘겨짚지 않는다.")
                DaemonClient.connect(sock).use { work(Companion(it, sid)) }
            } catch (e: Exception) {
                val v = DaemonLifecycle(sock, start = {}, daemons = SocketDaemons).verdict()
                trouble(
                    when (v) {
                        DaemonLifecycle.Verdict.LEFT -> "데몬이 없다 — 아직 안 켰거나 질서 있게 나갔다."
                        DaemonLifecycle.Verdict.KILLED -> "소켓은 있는데 아무도 안 듣는다 — 죽은 것으로 보인다."
                        DaemonLifecycle.Verdict.ALIVE -> "붙었다가 끊겼다: ${e.message}"
                    }
                )
            }
        }
    }
}
