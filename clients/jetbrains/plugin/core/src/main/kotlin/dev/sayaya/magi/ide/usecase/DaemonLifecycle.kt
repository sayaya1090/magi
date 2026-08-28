package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.transport.SocketPath
import java.nio.file.Files
import java.nio.file.Path
import kotlin.random.Random

/**
 * 워크스페이스에 데몬 하나가 계속 서 있게 한다.
 *
 * 설계 근거는 `clients/jetbrains/README.md` §2 에 있다. 여기 있는 것은 그 규칙의 코드이고, 규칙마다
 * 왜 그런지가 아니라 **어느 절인지**를 적는다.
 */
class DaemonLifecycle(
    private val socket: Path,
    private val start: (Path) -> Unit,
    private val sleep: (Long) -> Unit = { Thread.sleep(it) },
    private val random: Random = Random.Default,
) {

    /** 감시자가 재기동을 할지 말지. §2 "사람이 끈 것과 죽은 것". */
    enum class Verdict {
        /** 붙을 수 있다. */
        ALIVE,

        /** 소켓 파일이 없다 = 질서 있게 나갔다. 사람이 껐거나 스스로 나갔다. 되살리지 않는다. */
        LEFT,

        /** 파일은 있는데 안 듣는다 = 죽임을 당했다. 되살린다. */
        KILLED,
    }

    fun verdict(): Verdict = when {
        DaemonClient.alive(socket) -> Verdict.ALIVE
        !Files.exists(socket) -> Verdict.LEFT
        else -> Verdict.KILLED
    }

    /**
     * 붙거나, 없으면 띄운다. §2 "갈래는 dial 하나로 정한다".
     *
     * 세 가지 실패(없음·죽음·지금 뜨는 중)를 구분하지 않는다. 어느 쪽이든 답이 "띄워 보고
     * flock 이 판정하게 두기"로 같기 때문이다. 기동 경쟁에서 지는 것은 예외가 아니라 정상이라
     * (창 셋이 한꺼번에 열린다) 진 쪽은 에러를 띄우지 않고 백오프+지터로 다시 붙는다. 지터가
     * 없으면 셋이 같은 박자로 재시도해 같은 순간에 또 부딪힌다.
     *
     * 끝내 못 붙으면 [Outcome.Unreachable] 로 **말한다.** 빈 화면은 "할 일 없음"처럼 보이는데
     * 사실은 "모른다"이고, 이 트리는 그 둘을 구분한다.
     */
    fun attachOrStart(
        attempts: Int = 6,
        firstBackoffMillis: Long = 120,
    ): Outcome {
        SocketPath.tooLong(socket)?.let { return Outcome.Unreachable(it) }

        runCatching { return Outcome.Attached(DaemonClient.connect(socket)) }

        var started = false
        var backoff = firstBackoffMillis
        repeat(attempts) {
            if (!started) {
                runCatching { start(socket) }.onSuccess { started = true }
            }
            sleep(backoff + random.nextLong(backoff / 2 + 1))
            backoff = (backoff * 2).coerceAtMost(2_000)
            runCatching { return Outcome.Attached(DaemonClient.connect(socket)) }
        }
        return Outcome.Unreachable(
            "데몬에 못 붙었다: $socket — 기동을 ${attempts}회 시도했고 마지막까지 응답이 없다"
        )
    }

    sealed interface Outcome {
        data class Attached(val client: DaemonClient) : Outcome
        data class Unreachable(val reason: String) : Outcome
    }
}
