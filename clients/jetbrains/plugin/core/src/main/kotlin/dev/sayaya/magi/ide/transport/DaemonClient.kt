package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.Wire
import dev.sayaya.magi.ide.usecase.Daemon
import dev.sayaya.magi.ide.usecase.Daemons
import dev.sayaya.magi.ide.usecase.Reach
import java.io.BufferedReader
import java.io.BufferedWriter
import java.net.ConnectException
import java.net.StandardProtocolFamily
import java.net.UnixDomainSocketAddress
import java.nio.channels.Channels
import java.nio.channels.SocketChannel
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.attribute.BasicFileAttributes
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

/**
 * 데몬 도메인 소켓 클라이언트 연결 세션.
 *
 * 프레이밍은 라인 단위 단일 JSON 객체 교환 방식이다 (`internal/adapter/daemon/protocol.go`의 `Request`, "One object per line").
 *
 * 이 연결은 **락스텝(Lockstep)** 프로토콜로 동작한다. 하나의 요청 라인을 전송하고 즉시 단일 응답 라인을 수신하므로,
 * 전체 트랜잭션이 단일 뮤텍스([lock]) 내에서 원자적으로 보호되어야 한다. 요청하지 않은 프레임이 중간에 유입될 경우
 * 이후 모든 요청-응답 쌍의 순서가 어긋나는 결함이 발생한다 (`internal/adapter/daemon/serve.go`의 `serveConn`).
 * 지속적인 이벤트 수신이 필요한 스트림의 경우 [stream] 메서드를 통해 독립적인 전용 연결을 생성하여 사용한다.
 *
 * 실행 시간이 긴 모델 추론 요청을 공용 폴링 연결로 전송해서는 안 된다. 웹 콘솔 및 TUI 역시 동일한 이유로
 * 일회용 전용 소켓 연결을 분리하여 사용한다 (`clients/web/server/main.go`의 `server.alone`, `cmd/magi/attach.go`의 `attached.sock`).
 */
class DaemonClient private constructor(
    private val channel: SocketChannel,
    private val reader: BufferedReader,
    private val writer: BufferedWriter,
    private val patienceMs: Long,
) : Daemon {

    private val lock = ReentrantLock()

    /**
     * 단일 요청 전송 및 응답 수신을 원자적으로 수행한다 (락스텝 프로토콜 보호).
     *
     * `readLine()`은 소켓이 닫히지 않는 한 무한 블록되므로, 응답이 지연되거나 무응답(wedge) 상태에 빠진 데몬 소켓을
     * 우측 독이 3초 주기로 폴링할 경우 워커 스레드가 고갈되는 결함이 발생한다.
     * AF_UNIX 소켓 채널은 네이티브 SO_TIMEOUT을 지원하지 않으므로, 백그라운드 워치독 타이머([reaper])를 통해
     * 타임아웃 발생 시 소켓 채널을 강제 종료(`channel.close()`)하는 방식으로 안전망을 구축한다.
     * 락스텝 연결에서는 응답 순서가 한 번이라도 어긋나면 세션을 복구할 수 없으므로 채널을 즉시 닫고 실패를 반환하는 것이 안전하다.
     */
    override fun exchange(request: Request): Response = lock.withLock {
        // AF_UNIX 송신 버퍼(8KB)가 가득 차서 flush() 단계에서 블록되는 상황을 방지하기 위해
        // 워치독은 쓰기 작업 개시 전에 미리 무장(arm)한다 (큰 페이로드 전송 시 블로킹 방어).
        val hung = java.util.concurrent.atomic.AtomicBoolean(false)
        val watchdog = reaper.schedule({
            hung.set(true)
            runCatching { channel.close() }
        }, patienceMs, java.util.concurrent.TimeUnit.MILLISECONDS)
        val line = try {
            writer.write(Wire.json.encodeToString(Request.serializer(), request))
            writer.write("\n")
            writer.flush()
            reader.readLine()
        } catch (e: java.io.IOException) {
            if (hung.get()) throw DaemonGone("응답 시한(${patienceMs}ms)을 넘겨 연결을 끊었다: ${request.method}")
            throw e
        } finally {
            // 응답 수신이 타임아웃 경계선과 거의 동시에 발생할 경우, cancel()이 이미 트리거된 리퍼 스레드를
            // 막지 못해 응답 자체는 정상 반환되나 채널만 닫히는 마이크로초 단위 경합(Race Condition)이 존재할 수 있다 (리뷰 F2).
            // 락스텝 연결에서는 시한에 임박한 경우 후속 트랜잭션의 신뢰성이 떨어지므로 안전하게 취소 처리한다.
            watchdog.cancel(false)
        }
        line ?: run {
            if (hung.get()) throw DaemonGone("응답 시한(${patienceMs}ms)을 넘겨 연결을 끊었다: ${request.method}")
            throw DaemonGone("데몬이 답하기 전에 연결을 닫았다: ${request.method}")
        }
        Wire.json.decodeFromString(Response.serializer(), line)
    }

    /**
     * 스트리밍 요청을 전송하고 연속되는 응답 라인을 콜백으로 전달한다.
     *
     * 이 연결은 스트리밍 전용으로 전환되므로 [exchange]와 같은 뮤텍스 락을 획득하지 않는다.
     * 락을 획득할 경우 스트림 세션이 유지되는 동안 공용 연결이 블록되어 데드락이 발생할 수 있다.
     */
    override fun stream(request: Request, each: (Response) -> Boolean) {
        writer.write(Wire.json.encodeToString(Request.serializer(), request))
        writer.write("\n")
        writer.flush()
        while (true) {
            val line = reader.readLine() ?: return // 소켓 정상 종료 (정상 스트림 완료)
            if (!each(Wire.json.decodeFromString(Response.serializer(), line))) return
        }
    }

    override fun close() {
        runCatching { channel.close() }
    }

    /** 데몬 비정상 종료(소켓 끊김)와 비즈니스 요청 거절을 명확히 구분하기 위한 I/O 예외. */
    class DaemonGone(message: String) : java.io.IOException(message)

    companion object {
        /**
         * 모델 추론을 수반하는 요청의 대기 시한 (120초).
         * 초안, 제안, 인라인 완성 등 LLM 백엔드를 경유하는 요청이므로 로컬 저사양 모델의 정상 응답이 타임아웃되지 않도록 넉넉하게 설정한다.
         */
        const val PATIENCE_ASK = 120_000L

        /**
         * 인메모리 상태 폴링 요청의 대기 시한 (30초).
         * 3초 주기로 동작하는 UI 폴러가 데몬 무응답 상태에서 120초 시한을 사용할 경우 최대 40개의 대기 스레드가 누적되는 결함이 발생했다
         * (2026-09-09 실측). 단순 인메모리 조회가 30초를 초과하는 것은 데몬이 교착 상태에 빠진 것으로 판단한다 (VS Code 확장 `deadlineFor`와 동등).
         */
        const val PATIENCE_POLL = 30_000L

        fun connect(socket: Path, patienceMs: Long = PATIENCE_ASK): DaemonClient {
            val ch = SocketChannel.open(StandardProtocolFamily.UNIX)
            try {
                ch.connect(UnixDomainSocketAddress.of(socket))
            } catch (e: Exception) {
                runCatching { ch.close() }
                throw e
            }
            return DaemonClient(
                ch,
                Channels.newReader(ch, Charsets.UTF_8).buffered(),
                Channels.newWriter(ch, Charsets.UTF_8).buffered(),
                patienceMs,
            )
        }

        /** 워치독 시계 하나. 데몬 스레드라 IDE 종료를 안 붙든다. */
        private val reaper = java.util.concurrent.ScheduledThreadPoolExecutor(1) { r ->
            Thread(r, "magi-daemonclient-watchdog").apply { isDaemon = true }
        }.apply {
            // 취소된 워치독을 큐에서 즉시 걷는다(기본 false — 실측): 안 걷으면 취소분이 시한
            // (2분)까지 닫힌 채널을 붙들고 폴러당 ~40개씩 상시 잔류한다. 유계지만 공짜로 0이다.
            removeOnCancelPolicy = true
        }

        /**
         * 소켓 접속 시도 결과를 정밀 판정하여 [Reach] 상태로 매핑한다.
         *
         * 단순히 불리언(`alive(): Boolean`)으로 예외를 은폐할 경우, 권한 부족으로 인한 접속 실패와
         * 프로세스 부재로 인한 거절이 동일하게 "데몬 죽음"으로 오판되어 무한 재기동 루프에 빠질 수 있다.
         */
        /**
         * 소켓 연결 실패 예외를 분석하여 상태를 분류한다.
         *
         * Windows 플랫폼의 잔여 소켓 파일 대응 (2026-09-09 실측 반영):
         * Windows에서 데몬이 비정상 종료되고 남은 잔여 AF_UNIX 소켓 파일에 연결을 시도할 경우,
         * 커널은 `ConnectException`이 아닌 `WSAEINVAL`("An invalid argument was supplied")을 반환하며,
         * 자바는 이를 일반 `SocketException`으로 래핑한다 (`listen_windows.go` 참조).
         * 이를 권한 오류(`CouldNotAsk`)로 오판하면 데몬 자동 기동이 억제되므로, 사전에 소켓 파일 존재 및
         * 비소켓 파일 여부를 검증한 상태라면 연결 거절([Reach.Refused])로 정확히 판정한다.
         */
        internal fun reachAfterFailedConnect(e: Exception): Reach = when {
            // 플랫폼 표준 연결 거절 예외
            e is ConnectException -> Reach.Refused
            // 접근 권한 부족(Permission Denied)은 데몬의 실제 생존 여부를 알 수 없으므로 CouldNotAsk로 보존한다.
            deniedByPermission(e) -> Reach.CouldNotAsk("${e.javaClass.simpleName}: ${e.message}")
            else -> Reach.Refused
        }

        internal fun deniedByPermission(e: Exception): Boolean {
            if (e is java.nio.file.AccessDeniedException) return true
            val m = (e.message ?: "").lowercase()
            return "permission denied" in m || "access is denied" in m || "access denied" in m
        }

        fun reach(socket: Path): Reach {
            // 소켓 파일의 완전한 부재는 즉시 Absent로 판정한다.
            if (Files.notExists(socket)) return Reach.Absent
            // 파일 유형 사전 검사:
            // 소켓이 아닌 일반 파일에 연결 시도 시 macOS는 ENOTSOCK(CouldNotAsk)을 반환하나,
            // Linux는 ECONNREFUSED(데몬 프로세스 부재와 동일한 에러)를 반환하는 커널 간 차이가 존재한다.
            // 일반 파일/디렉토리에 대해 "데몬 사망"으로 오판단하여 잘못된 재기동을 시도하지 않도록,
            // 파일 속성([BasicFileAttributes.isOther])을 통해 특수 파일(소켓/FIFO)이 아닌 경우 사전에 배제한다.
            val surelyNotSocket = runCatching {
                !Files.readAttributes(socket, BasicFileAttributes::class.java).isOther
            }.getOrDefault(false)
            if (surelyNotSocket) return Reach.CouldNotAsk("not a socket: $socket")
            return try {
                connect(socket).use { Reach.Listening }
            } catch (e: Exception) {
                reachAfterFailedConnect(e)
            }
        }
    }
}

/**
 * 유닉스 소켓으로 데몬을 여는 [Daemons]. 규칙 층이 보는 유일한 전송 구현이고, 원격 개발(Gateway,
 * WSL)에서 갈아 끼울 자리도 여기 하나다.
 */
object SocketDaemons : Daemons {
    override fun connect(socket: Path): Daemon = DaemonClient.connect(socket)
    override fun reach(socket: Path): Reach = DaemonClient.reach(socket)
    override fun unusable(socket: Path): String? = SocketPath.tooLong(socket)
}
