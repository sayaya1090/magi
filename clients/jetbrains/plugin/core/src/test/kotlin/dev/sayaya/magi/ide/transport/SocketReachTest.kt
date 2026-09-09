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
        // **픽스처가 진짜인지 픽스처가 말하게 한다.** 「죽은 데몬이 남긴 것」을 흉내 낼 때
        // 보통 파일을 놓으면 시험 이름과 재는 것이 갈린다 — 크래시는 보통 파일을 안 남긴다
        // (bind 가 만드는 것도 크래시가 남기는 것도 소켓 아이노드다). 그렇게 흉내 낸 시험은
        // 「죽은 소켓」이 아니라 「보통 파일」을 재게 되고, 그러면 판정이 그 둘을 가르는 순간
        // 시험이 **결함 쪽을 지킨다**(코어 쪽 자매 시험 둘이 실제로 그 모양이었다).
        // 여기 JVM 은 닫을 때 소켓 파일을 안 지우지만, 그건 이 시험이 기대는 성질이라 잰다.
        assertTrue(Files.exists(sock), "닫은 뒤 소켓 파일이 없어졌다 — 이 시험의 전제가 깨졌다")
        assertTrue(
            Files.readAttributes(sock, java.nio.file.attribute.BasicFileAttributes::class.java).isOther,
            "남은 것이 소켓이 아니다 — 「죽은 데몬」이 아니라 딴것을 재고 있다",
        )
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
        // **맞는 답이 맞는 근거로 맞아야 한다.** 이 갈래가 예전엔 errno 로 왔고, 그 errno 는
        // 커널마다 다르다 — macOS 는 `ENOTSOCK`, 리눅스는 `ECONNREFUSED`(=죽은 데몬과 같은
        // 말). 그래서 이 자리는 macOS 에서만 초록이었고 CI(우분투)에서 하루 넘게 빨갰다.
        // 갈래만 재면 그 회귀가 이 기계에서 또 안 보인다: **어느 길로 왔는지**를 잰다.
        assertTrue(
            "not a socket" in (r as Reach.CouldNotAsk).why,
            "갈래는 맞는데 근거가 파일 종류가 아니다(errno 로 왔다) — 리눅스에서 다시 갈린다: ${r.why}",
        )
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
    /**
     * ★ **윈도우의 시체 소켓은 `ConnectException` 이 아니다.**
     *
     * 실사용 보고(2026-09-09): 윈도우에서 데몬이 죽고 소켓 파일만 남으면 플러그인이 아무것도 못
     * 했다. 그 커널은 남은 AF_UNIX 파일에 붙을 때 **WSAEINVAL**("An invalid argument was supplied")
     * 를 주고 — 코어의 `listen_windows.go` 가 그 사실을 적어 두고 있다 — 자바는 그것을
     * `SocketException` 으로 낸다. 그래서 그 갈래가 `CouldNotAsk` 로 떨어졌고, 자동 기동은
     * `CouldNotAsk` 에서 **일부러 안 띄운다**(모름을 없음으로 읽지 않으려고). 판정 하나 때문에
     * 되살아날 길이 전부 막혔다.
     *
     * **이 기계(macOS)에서는 그 커널을 못 만든다.** 그래서 재는 것은 갈래를 정하는 규칙 자체다:
     * 권한 아닌 실패는 「아무도 안 듣는다」이고, 권한만 「모른다」로 남는다.
     */
    @Test
    fun `권한이 아닌 실패는 아무도 안 듣는 것이다 — 커널이 어떤 낱말로 말하든`() {
        // 윈도우가 시체 소켓에 주는 그 문장.
        // 매핑 자체를 잰다. 규칙만 재고 「그 규칙을 쓰는지」를 안 재면 매핑을 통째로 옛 결함으로
        // 되돌린 변이가 초록으로 통과한다 — 실제로 그랬다.
        assertEquals(
            Reach.Refused,
            DaemonClient.reachAfterFailedConnect(java.net.SocketException("An invalid argument was supplied")),
            "WSAEINVAL 을 모름으로 읽으면 시체 소켓에서 영영 안 띄운다",
        )
        assertEquals(Reach.Refused, DaemonClient.reachAfterFailedConnect(java.io.IOException("Broken pipe")))
        assertEquals(Reach.Refused, DaemonClient.reachAfterFailedConnect(java.net.ConnectException("refused")))
    }

    /**
     * 권한만 「모름」으로 남는다. 권한이 없어 못 붙는 것은 데몬이 살았는지에 대해 아무 말도 하지
     * 않고, 그 자리에서 새로 띄우면 같은 이유로 또 못 붙어 기동 예산만 태운다.
     */
    @Test
    fun `권한 때문에 못 붙은 것은 모름으로 남는다`() {
        assertInstanceOf(
            Reach.CouldNotAsk::class.java,
            DaemonClient.reachAfterFailedConnect(java.nio.file.AccessDeniedException("/x/s.sock")),
        )
        assertInstanceOf(
            Reach.CouldNotAsk::class.java,
            DaemonClient.reachAfterFailedConnect(java.net.SocketException("Permission denied")),
        )
        assertInstanceOf(
            Reach.CouldNotAsk::class.java,
            DaemonClient.reachAfterFailedConnect(java.io.IOException("Access is denied")),
        )
    }

}
