package dev.sayaya.magi.ide.usecase

import java.nio.file.Path
import kotlin.random.Random

/**
 * 워크스페이스 데몬 프로세스의 수명주기(기동, 감시, 재접속)를 관리한다.
 *
 * 설계 근거 및 상태 전이 규칙은 `clients/jetbrains/README.md` §2를 준수한다.
 */
class DaemonLifecycle(
    private val socket: Path,
    private val start: (Path) -> Unit,
    /** 전송 계층 추상화 ([Daemons]). 단위 테스트 페이크 주입 및 원격 환경 확장을 지원한다. */
    private val daemons: Daemons,
    private val sleep: (Long) -> Unit = { Thread.sleep(it) },
    private val random: Random = Random.Default,
) {

    /**
     * 감시 루프가 판정하는 데몬 상태 및 재기동 정책 (`clients/jetbrains/README.md` §2).
     *
     * [Reach]와 [Verdict]의 분리 이유:
     * 현재는 1:1 매핑되나 변경의 원인과 도메인이 상이하다.
     * [Reach]는 전송 계층이 직면하는 물리적 네트워크/소켓 상태(Gateway 타임아웃, WSL 호스트 부재 등)에 따라 확장되며,
     * [Verdict]는 클라이언트의 복구 정책(재기동 시도 여부)에 따라 확장된다.
     * 두 개념을 분리함으로써 전송 계층 계약에 비즈니스 재기동 정책이 결합되는 것을 방지한다.
     */
    sealed interface Verdict {
        /** 정상 청취 중 (정상 연결 가능). */
        data object Alive : Verdict

        /** 소켓 파일 부재 (정상 종료). 사용자가 수동 종료했거나 프로세스가 정상 탈출한 상태이므로 자동 재기동하지 않는다. */
        data object Left : Verdict

        /** 소켓 파일은 존재하나 연결 거절됨 (비정상 종료/강제 종료). 자동 재기동 대상. */
        data object Killed : Verdict

        /**
         * 상태 조회 불가 (권한 부족 또는 비소켓 파일).
         * 실제 프로세스 종료 여부가 불확실하므로 임의로 재기동하지 않는다 (무분별한 프로세스 생성 방지, §0.5-7).
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
     * 데몬에 연결하거나, 미기동 상태인 경우 기동 후 연결을 확립한다 (`clients/jetbrains/README.md` §2).
     *
     * 부재, 비정상 종료, 기동 중 상태를 구분하지 않고 연결 시도 후 실패 시 기동을 트리거하며,
     * 다중 IDE 창 동시 기동 시의 경쟁은 코어의 파일 락(flock)이 단일성을 중재한다.
     * 기동 경쟁에서 밀린 세션은 백오프 및 무작위 지터(Jitter)를 적용하여 재시도함으로써 동시 충돌을 분산한다.
     * 최종 실패 시 [Outcome.Unreachable]로 명시적 실패 사유를 반환한다.
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
