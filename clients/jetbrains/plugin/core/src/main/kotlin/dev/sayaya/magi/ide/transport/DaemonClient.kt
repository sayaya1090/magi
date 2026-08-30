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
 * 데몬 소켓 하나에 붙은 연결.
 *
 * 프레이밍은 한 줄에 JSON 객체 하나다(daemon.go 의 `Request`, "One object per line"). 그 이상이 필요 없고 `nc` 로 사람이 읽을
 * 수 있다.
 *
 * **이 연결은 락스텝이다.** 요청 한 줄을 쓰고 다음 한 줄을 그 요청의 답으로 읽는다. 그래서
 * 교환 전체가 한 자물쇠 안에 있어야 하고, 청하지 않은 프레임이 끼어들면 그 뒤 모든 교환이 한
 * 칸씩 밀린다. 데몬이 그 불변식을 주석으로 적어 두었고 `watch` 만 예외인 이유가 연결을 통째로
 * 넘겨받기 때문이다(daemon.go 의 `serveConn`). 스트림이 필요하면 [openStream] 으로 **다른 연결**을 판다.
 *
 * 느린 모델 호출을 이 연결로 보내지 말 것. 콘솔과 TUI 가 같은 이유로 일회용 연결을 따로
 * 판다(`clients/web/server/main.go` 의 `server.alone`, `cmd/magi/attach.go` 의 `attached.sock`).
 */
class DaemonClient private constructor(
    private val channel: SocketChannel,
    private val reader: BufferedReader,
    private val writer: BufferedWriter,
    private val patienceMs: Long,
) : Daemon {

    private val lock = ReentrantLock()

    /**
     * 한 번의 요청과 그 답. 락스텝이므로 통째로 잠근다.
     *
     * **시한이 있다.** `readLine` 은 무한 대기라, 연결은 받되 답하지 않는(웨지된) 데몬 하나가
     * 부른 스레드를 영영 세웠다 — 우측 독 폴이 그 소켓을 3초마다 두드리면 스레드가 쌓인다(리뷰
     * 실측 잔여의 승격). AF_UNIX 채널엔 읽기 SO_TIMEOUT 이 없어 **워치독이 연결을 닫는** 것으로
     * 시한을 만든다: 락스텝 연결은 답이 밀린 순간 이미 못 쓰는 물건이라, 닫는 것이 곧 정직한
     * 실패다. 기본 시한이 넉넉한 이유(2분)는 이 문으로 모델 호출(초안·제안)이 지나가서다 —
     * 짧게 잡으면 느린 로컬 모델의 정답이 시한 초과로 둔갑한다.
     */
    override fun exchange(request: Request): Response = lock.withLock {
        // 워치독은 **쓰기보다 먼저** 무장한다(리뷰 실측): AF_UNIX 버퍼는 8KB 라, 받기만 하고
        // 안 읽는 상대에 큰 요청(열린 버퍼 전문, look-over)을 쓰면 flush 가 무한 블록이다 —
        // 뒤에 무장하면 이 유닛이 죽이려던 hang 이 write 쪽으로 그대로 남는다.
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
            // 답이 정확히 시한 언저리에 오면 cancel 이 이미 도는 리퍼를 못 막아, 이번 답은
            // 정상 반환하고 **연결만** 닫히는 µs 창이 있다(리뷰 F2) — 같은 연결의 다음 교환이
            // "끊겼다"로 오귀속된다. 락스텝 연결은 시한을 스친 순간 어차피 사망 선고가 맞아
            // 창을 없애는 대신 여기 적는다.
            watchdog.cancel(false)
        }
        line ?: run {
            if (hung.get()) throw DaemonGone("응답 시한(${patienceMs}ms)을 넘겨 연결을 끊었다: ${request.method}")
            throw DaemonGone("데몬이 답하기 전에 연결을 닫았다: ${request.method}")
        }
        Wire.json.decodeFromString(Response.serializer(), line)
    }

    /**
     * 요청 한 줄을 쓰고 그 뒤로 오는 것을 전부 넘긴다.
     *
     * **락을 잡지 않는다.** `exchange` 가 잡는 락은 "요청 하나에 답 하나"를 지키려는 것인데
     * 스트림은 그 계약을 쓰지 않는다 — 이 연결은 이제 스트림의 것이고, 다른 교환은 애초에 오면
     * 안 된다. 락을 잡으면 스트림이 사는 내내 그 연결이 잠겨 같은 결과를 **에러 대신 교착**으로
     * 만든다.
     */
    override fun stream(request: Request, each: (Response) -> Boolean) {
        writer.write(Wire.json.encodeToString(Request.serializer(), request))
        writer.write("\n")
        writer.flush()
        while (true) {
            val line = reader.readLine() ?: return // 데몬이 닫았다 — 깨끗한 끝이지 에러가 아니다
            if (!each(Wire.json.decodeFromString(Response.serializer(), line))) return
        }
    }

    override fun close() {
        runCatching { channel.close() }
    }

    /** 데몬이 사라진 것과 요청이 거절당한 것은 다른 사건이라 타입을 나눈다. */
    class DaemonGone(message: String) : java.io.IOException(message)

    companion object {
        /**
         * 붙는다. 못 붙으면 예외이고, **그 예외가 곧 "내가 데몬이 될 차례"라는 답**이다
         * (파일이 없는 것과 있는데 아무도 안 듣는 것을 구분하지 않는 이유는 설계 문서 §2).
         *
         * AF_UNIX 는 어디서나 쓴다. 윈도우도 마찬가지이고 거기서는 담긴 디렉토리의 ACL 이
         * 권한을 정한다(listen_windows.go 의 `listenOwnerOnly`).
         */
        fun connect(socket: Path, patienceMs: Long = 120_000): DaemonClient {
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
         * 붙어 보고 **무엇을 만났는지**. 갈래의 뜻과 실측 표는 [Reach].
         *
         * 예전엔 `alive(): Boolean` 이었고 `runCatching { … }.getOrDefault(false)` 로 예외를
         * 전부 접었다. 접힌 것을 부르는 쪽이 "죽었다"로 폈으므로, 여기서 접는 것이 곧 저기서
         * 거짓말이었다. **접는 자리를 없애는 것이 고침이다** — 펴는 쪽을 고치면 다음 부르는
         * 쪽에서 같은 일이 또 일어난다.
         */
        fun reach(socket: Path): Reach {
            // 확실히 없는 것만 없다고 한다. `exists` 가 아니라 `notExists` 인 이유는 [Reach.Absent].
            if (Files.notExists(socket)) return Reach.Absent
            // **소켓인지 먼저 본다.** 갈래를 errno 로만 가르면 커널마다 답이 갈린다: 소켓이
            // 아닌 보통 파일에 붙어 보면 macOS 는 `ENOTSOCK`(→ CouldNotAsk)인데 리눅스는
            // `ECONNREFUSED` 를, 즉 **죽은 데몬과 똑같은 말**을 준다. 그래서 이 판정이
            // macOS 에서만 맞았고, CI(우분투)에서 하루 넘게 빨갛게 서 있었다.
            //
            // 무는 것은 시험만이 아니다. 리눅스에서 소켓 자리에 엉뚱한 파일이 있으면 화면이
            // 「소켓은 있는데 아무도 안 듣는다 — 죽은 것으로 보인다」고 말한다. 데몬이었던
            // 적이 없는 파일에 대해 죽었다고 말하는 것이고, 그 판정에는 재기동이 달려 있다.
            //
            // 파일 **종류**는 커널을 안 탄다. 소켓·FIFO·장치는 `isOther` 이고, 보통 파일과
            // 디렉토리는 아니다 — 확실히 소켓이 아닌 것만 여기서 거른다. 못 보면(권한) 판단을
            // 안 하고 아래로 보낸다: 모르는 것을 아는 척하지 않는다.
            val surelyNotSocket = runCatching {
                !Files.readAttributes(socket, BasicFileAttributes::class.java).isOther
            }.getOrDefault(false)
            if (surelyNotSocket) return Reach.CouldNotAsk("not a socket: $socket")
            return try {
                connect(socket).use { Reach.Listening }
            } catch (e: ConnectException) {
                // 유일하게 "아무도 안 듣는다"를 뜻하는 예외. 나머지는 아래로 간다.
                Reach.Refused
            } catch (e: Exception) {
                Reach.CouldNotAsk("${e.javaClass.simpleName}: ${e.message}")
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
