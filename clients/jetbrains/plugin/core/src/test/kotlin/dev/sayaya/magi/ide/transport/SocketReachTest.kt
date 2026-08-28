package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.usecase.Reach
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.net.StandardProtocolFamily
import java.net.UnixDomainSocketAddress
import java.nio.channels.ServerSocketChannel
import java.nio.file.Files
import java.nio.file.Path

/**
 * 붙어 보고 만난 것을 [Reach] 갈래로 가르는 것 — **진짜 소켓으로** 잰다.
 *
 * 이 시험이 이 커밋의 근거다. 갈래 넷은 내가 나눈 것이 아니라 **예외가 이미 갈라 놓은 것**이고,
 * 그것을 확인하는 유일한 방법이 실제로 syscall 을 때려 보는 것이다. 페이크로는 못 잰다 — 페이크는
 * 내가 믿는 매핑을 그대로 되풀이할 뿐이라, 내가 틀렸으면 같이 틀린다.
 *
 * 데몬은 필요 없다. 다섯 조건 전부 임시 디렉토리에 손으로 만들 수 있다.
 */
class SocketReachTest {

    /** 짧은 자리를 쓴다. AF_UNIX 주소는 100바이트 남짓이 한계이고 시험 자리가 길면 그것부터 터진다. */
    private fun tempDir(): Path = Files.createTempDirectory("rch").also { it.toFile().deleteOnExit() }

    private fun assertUsable(socket: Path) =
        assertEquals(null, SocketPath.tooLong(socket), "시험 자리가 너무 길다 — 재려는 것과 무관한 실패다")

    @Test
    fun `듣고 있으면 붙는다`() {
        val sock = tempDir().resolve("s.sock")
        assertUsable(sock)
        ServerSocketChannel.open(StandardProtocolFamily.UNIX).use { server ->
            server.bind(UnixDomainSocketAddress.of(sock))
            assertEquals(Reach.Listening, DaemonClient.reach(sock))
        }
    }

    @Test
    fun `듣던 것이 사라지고 파일만 남으면 거절당한다`() {
        // 이것이 유일한 「죽임을 당했다」다. 닫아도 소켓 파일은 남는다 — 그래서 파일의 존재로는
        // 죽은 데몬과 산 데몬이 구분이 안 되고, 설계 문서 §2 가 그 얘기다.
        val sock = tempDir().resolve("s.sock")
        assertUsable(sock)
        ServerSocketChannel.open(StandardProtocolFamily.UNIX).use { it.bind(UnixDomainSocketAddress.of(sock)) }
        assertTrue(Files.exists(sock), "닫은 뒤 소켓 파일이 없어졌다 — 이 시험의 전제가 깨졌다")
        assertEquals(Reach.Refused, DaemonClient.reach(sock))
    }

    @Test
    fun `없는 경로는 나간 것이다`() {
        assertEquals(Reach.Absent, DaemonClient.reach(tempDir().resolve("nobody.sock")))
    }

    @Test
    fun `소켓이 아닌 보통 파일은 못 물어본 것이지 죽은 것이 아니다`() {
        // 예전에 이 자리가 「죽임을 당했다」로 접혀 있었다: 파일이 있고(`present`) 못 붙었으니
        // (`!alive`) 죽은 것으로 판정됐다. 데몬이 아니었던 적이 없으므로 되살릴 것도 없다.
        val sock = tempDir().resolve("s.sock")
        Files.writeString(sock, "나는 소켓이 아니다")
        val r = DaemonClient.reach(sock)
        assertInstanceOf(Reach.CouldNotAsk::class.java, r, "보통 파일을 죽은 데몬으로 읽었다: $r")
    }

    @Test
    fun `볼 수 없는 자리도 못 물어본 것이다`() {
        // `Files.exists` 는 볼 수 없을 때도 false 라, 그것으로 판정하면 여기가 「나갔다」가 된다.
        // `Files.notExists` 는 같은 자리에서 false 를 준다(둘 다 false = 모른다).
        //
        // uid 에 따라 만나는 것이 다르다 — 보통은 `BindException: Permission denied`, root 면
        // 디렉토리를 통과해 보통 파일을 만나 `SocketException: … non-socket`. **갈래는 둘 다
        // 같다**, 그래서 이 시험은 uid 를 안 탄다.
        val dir = Files.createDirectory(tempDir().resolve("d"))
        val sock = Files.writeString(dir.resolve("s.sock"), "x")
        dir.toFile().setExecutable(false, false)
        try {
            val r = DaemonClient.reach(sock)
            assertInstanceOf(Reach.CouldNotAsk::class.java, r, "볼 수 없는 자리를 아는 것으로 읽었다: $r")
        } finally {
            dir.toFile().setExecutable(true, false)
        }
    }
}
