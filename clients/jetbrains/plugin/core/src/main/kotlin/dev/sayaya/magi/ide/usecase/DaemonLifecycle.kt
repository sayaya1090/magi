package dev.sayaya.magi.ide.usecase

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
    /**
     * 전송. 이 클래스는 소켓을 모른다 — 원격 개발에서 갈아 끼울 자리가 여기 하나이고, 시험은
     * 진짜 소켓 없이 이 자리에 페이크를 준다.
     */
    private val daemons: Daemons,
    private val sleep: (Long) -> Unit = { Thread.sleep(it) },
    private val random: Random = Random.Default,
) {

    /**
     * 감시자가 재기동을 할지 말지. §2 "사람이 끈 것과 죽은 것".
     *
     * **[Reach] 와 갈래 수가 같은데 왜 둘인가.** 오늘은 1:1 이지만 바뀌는 사유가 다르다 —
     * [Reach] 는 **전송이 새로 만날 것**이 생기면 늘고(Gateway 의 타임아웃, WSL 의 호스트 부재),
     * [Verdict] 는 **재기동 정책**이 바뀌면 는다. 합치면 정책이 포트 계약으로 새서, 전송을 새로
     * 쓰는 사람이 자기가 안 정할 일("되살린다")을 읽게 된다. 오늘 같아 보인다고 접지 말 것.
     */
    sealed interface Verdict {
        /** 붙을 수 있다. */
        data object Alive : Verdict

        /** 소켓 파일이 없다 = 질서 있게 나갔다. 사람이 껐거나 스스로 나갔다. 되살리지 않는다. */
        data object Left : Verdict

        /** 파일은 있는데 붙기를 거절당했다 = 죽임을 당했다. 되살린다. */
        data object Killed : Verdict

        /**
         * 물어볼 수가 없었다. [why] 는 만난 것 그대로.
         *
         * **되살리지 않는다** — 죽었다는 근거가 없기 때문이다. 예전엔 이 자리가 [Killed] 로
         * 접혀 있었고, 그래서 소켓이 아닌 파일이 그 경로에 있거나 디렉토리를 볼 수 없을 때
         * "죽은 것으로 보인다"고 말하며 되살리려 들었다. 모르는 것을 아는 척하는 자리가
         * **판정**이면 그 대가는 문장이 아니라 행동이다.
         */
        data class Unknown(val why: String) : Verdict
    }

    fun verdict(): Verdict = when (val r = daemons.reach(socket)) {
        is Reach.Listening -> Verdict.Alive
        is Reach.Absent -> Verdict.Left
        is Reach.Refused -> Verdict.Killed
        is Reach.CouldNotAsk -> Verdict.Unknown(r.why)
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
        daemons.unusable(socket)?.let { return Outcome.Unreachable(it) }

        runCatching { return Outcome.Attached(daemons.connect(socket)) }

        var started = false
        var backoff = firstBackoffMillis
        repeat(attempts) {
            if (!started) {
                runCatching { start(socket) }.onSuccess { started = true }
            }
            sleep(backoff + random.nextLong(backoff / 2 + 1))
            backoff = (backoff * 2).coerceAtMost(2_000)
            runCatching { return Outcome.Attached(daemons.connect(socket)) }
        }
        return Outcome.Unreachable(
            "데몬에 못 붙었다: $socket — 기동을 ${attempts}회 시도했고 마지막까지 응답이 없다"
        )
    }

    sealed interface Outcome {
        data class Attached(val client: Daemon) : Outcome
        data class Unreachable(val reason: String) : Outcome
    }
}
