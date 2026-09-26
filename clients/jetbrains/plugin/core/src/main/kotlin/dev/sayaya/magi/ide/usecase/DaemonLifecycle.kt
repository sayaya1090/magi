package dev.sayaya.magi.ide.usecase

import java.nio.file.Path

/**
 * 워크스페이스 데몬이 지금 어떤 상태인지 판정한다(살았나·질서 있게 나갔나·죽임을 당했나·모르나).
 *
 * 기동과 재접속은 여기서 하지 않는다 — 제품의 그 경로는 `ui/StartDaemon.kt` 이고, 모르는 상태에서
 * 띄우지 않기·자기 갱신 유예·재기동 예산·계보 구분을 거기서 한다. 이 클래스에 있던
 * `attachOrStart`(붙거나 띄우고 백오프)는 제품에 한 번도 연결되지 않은 채 시험만 붙들고 있었고,
 * 「모르면 띄운다」는 점에서 제품 경로보다 거칠어 2026-09-26 에 걷었다.
 * 설계 근거는 `clients/jetbrains/README.md` §2.
 */
class DaemonLifecycle(
    private val socket: Path,
    /** 전송 계층 추상화 ([Daemons]). 단위 테스트 페이크 주입 및 원격 환경 확장을 지원한다. */
    private val daemons: Daemons,
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
}

/** Owns only the process this IDE project started. Closing never targets a discovered daemon. */
class DaemonProcess(private val terminate: (Process) -> Unit = { it.destroy() }) : AutoCloseable {
    private var closed = false
    private var process: Process? = null

    @get:Synchronized
    val running: Boolean get() = process?.isAlive == true

    @Synchronized
    fun launch(start: () -> Process): Process? {
        if (closed || process?.isAlive == true) return null
        return start().also { process = it }
    }

    @Synchronized
    fun stop(child: Process) {
        if (process === child) {
            terminate(child)
            process = null
        }
    }

    @Synchronized
    override fun close() {
        closed = true
        process?.let(terminate)
        process = null
    }
}
