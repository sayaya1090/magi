package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.Wire
import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.Closeable
import java.net.StandardProtocolFamily
import java.net.UnixDomainSocketAddress
import java.nio.channels.Channels
import java.nio.channels.SocketChannel
import java.nio.file.Path
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
 * 판다(cmd/magi-web 의 `server.alone`, cmd/magi 의 `attached.sock`).
 */
class DaemonClient private constructor(
    private val channel: SocketChannel,
    private val reader: BufferedReader,
    private val writer: BufferedWriter,
) : Closeable {

    private val lock = ReentrantLock()

    /** 한 번의 요청과 그 답. 락스텝이므로 통째로 잠근다. */
    fun exchange(request: Request): Response = lock.withLock {
        writer.write(Wire.json.encodeToString(Request.serializer(), request))
        writer.write("\n")
        writer.flush()
        val line = reader.readLine()
            ?: throw DaemonGone("데몬이 답하기 전에 연결을 닫았다: ${request.method}")
        Wire.json.decodeFromString(Response.serializer(), line)
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
        fun connect(socket: Path): DaemonClient {
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
            )
        }

        /** 붙을 수 있는지만 본다. 소켓 파일의 존재로 판정하면 안 되는 이유는 설계 문서 §2. */
        fun alive(socket: Path): Boolean =
            runCatching { connect(socket).use { true } }.getOrDefault(false)
    }
}
