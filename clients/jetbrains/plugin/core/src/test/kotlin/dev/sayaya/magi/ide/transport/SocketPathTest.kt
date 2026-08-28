package dev.sayaya.magi.ide.transport

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.nio.file.Paths

/**
 * 이 파일이 지키는 것은 하나다 — **JVM 이 Go 와 같은 소켓 이름을 낸다.**
 *
 * 골든 값은 지어낸 것이 아니라 실제로 돌던 데몬에서 왔다. `/tmp/mw1` 아래에
 * `daemon-ws1-b1lp9vc8.sock` 이 서 있었고 그 레코드의 workdir 가 `/tmp/ws1` 이었다.
 * macOS 에서 `/tmp` 는 `/private/tmp` 로 풀리므로 해싱된 문자열은 `/private/tmp/ws1` 이다.
 */
class SocketPathTest {

    @Test
    fun `해시는 돌던 데몬이 낸 이름과 같다`() {
        assertEquals("b1lp9vc8", SocketPath.shortHash("/private/tmp/ws1"))
    }

    /**
     * 위 골든의 해시는 상위 비트가 서 있다(15699900456404220863 > 2^63). Long 은 부호가 있어서
     * `%` 와 `/` 를 그냥 쓰면 음수가 나오고 답이 갈린다. 즉 저 한 줄이 부호 없는 연산의
     * 시험이기도 한데, 그 사실이 안 보이면 다음 사람이 `remainderUnsigned` 를 지운다.
     * 그래서 여기 적어 두고 상위 비트가 없는 값도 같이 건다.
     */
    @Test
    fun `상위 비트가 서도 서지 않아도 같은 답을 낸다`() {
        assertEquals("3rdt71jr", SocketPath.shortHash("/a"))          // 상위 비트 있음
        assertEquals("ov1j1jmu", SocketPath.shortHash("/"))           // 없음
        assertEquals("6lw0yxf3", SocketPath.shortHash("/Users/sayaya/IdeaProjects/magi"))
    }

    @Test
    fun `해시는 룬이 아니라 UTF-8 바이트를 먹는다`() {
        assertEquals("87toz40s", SocketPath.shortHash("/프로젝트/앱"))
    }

    /**
     * Go 의 sanitize 는 룬 단위다. 자바의 char 단위로 돌면 서로게이트 쌍 하나가 '-' 둘이 되어
     * 이름이 갈린다. 이모지 한 글자는 '-' 하나여야 한다.
     */
    @Test
    fun `허용 문자 밖은 룬 하나당 하이픈 하나가 된다`() {
        assertEquals("a-b", SocketPath.sanitize("a😀b"))
        assertEquals("-", SocketPath.sanitize("앱"))
        assertEquals("my-repo_2", SocketPath.sanitize("my-repo_2"))
        assertEquals("a-b-c", SocketPath.sanitize("a.b c"))
    }

    @Test
    fun `소켓 이름은 베이스와 해시를 이어 붙인다`() {
        val socket = SocketPath.of(Paths.get("/tmp/mw1"), Paths.get("/private/tmp/ws1"))
        assertEquals("daemon-ws1-b1lp9vc8.sock", socket.fileName.toString())
        assertEquals(
            "/tmp/mw1/daemon-ws1-b1lp9vc8.sock.session",
            SocketPath.sessionFile(socket).toString(),
        )
    }

    @Test
    fun `너무 긴 경로는 그 사유를 말한다`() {
        assertNull(SocketPath.tooLong(Paths.get("/tmp/mw1/daemon-ws1-b1lp9vc8.sock")))
        val long = Paths.get("/" + "x".repeat(120) + "/daemon-a-b.sock")
        val why = SocketPath.tooLong(long)
        assertTrue(why != null && why.contains("MAGI_CONFIG_DIR"))
    }

    /**
     * Go 의 EvalSymlinks 는 준 대소문자를 그대로 돌려준다. 자바의 toRealPath() 는 고친다.
     * 대소문자를 무시하는 볼륨에서 그 차이가 소켓을 둘로 가른다.
     */
    @Test
    fun `심링크는 풀되 대소문자는 고치지 않는다`() {
        val base = java.nio.file.Files.createTempDirectory("magi-case")
        java.nio.file.Files.createDirectory(base.resolve("CaseDir"))
        val asked = base.resolve("casedir")
        org.junit.jupiter.api.Assumptions.assumeTrue(
            java.nio.file.Files.exists(asked), "대소문자 구분 볼륨 — 건너뜀")
        assertEquals("casedir", SocketPath.evalSymlinks(asked).fileName.toString())
    }

    @Test
    fun `심링크는 실제로 푼다`() {
        val base = java.nio.file.Files.createTempDirectory("magi-link")
        val real = java.nio.file.Files.createDirectory(base.resolve("real"))
        val link = java.nio.file.Files.createSymbolicLink(base.resolve("link"), real)
        assertEquals(real.fileName.toString(), SocketPath.evalSymlinks(link).fileName.toString())
    }

    @Test
    fun `설정 디렉토리는 MAGI_CONFIG_DIR 이 이긴다`() {
        val dir = SocketPath.configDir(env = { if (it == "MAGI_CONFIG_DIR") "/tmp/mw1" else null })
        assertEquals("/tmp/mw1", dir.toString())
    }

    @Test
    fun `설정 디렉토리는 플랫폼마다 다른 자리를 본다`() {
        val mac = SocketPath.configDir(env = { null }, os = "Mac OS X", home = "/Users/x")
        assertEquals("/Users/x/Library/Application Support/magi", mac.toString())
        val linux = SocketPath.configDir(env = { null }, os = "Linux", home = "/home/x")
        assertEquals("/home/x/.config/magi", linux.toString())
        val xdg = SocketPath.configDir(
            env = { if (it == "XDG_CONFIG_HOME") "/home/x/cfg" else null },
            os = "Linux", home = "/home/x",
        )
        assertEquals("/home/x/cfg/magi", xdg.toString())
    }
}

/**
 * 데몬의 레코드를 읽는다. 어느 대화에 붙을지를 넘겨짚지 않기 위한 자리라, 모양이 바뀌면
 * 조용히 빈 세션으로 붙는 대신 여기가 먼저 깨져야 한다.
 */
class PublishedTest {
    @Test
    fun `레코드에서 세션과 워크디렉토리를 읽는다`() {
        val dir = java.nio.file.Files.createTempDirectory("magi-rec")
        val sock = dir.resolve("daemon-ws1-b1lp9vc8.sock")
        java.nio.file.Files.writeString(
            SocketPath.sessionFile(sock),
            """{"socket":"$sock","workdir":"/tmp/ws1","session":"s_abc","pid":42,"unknown":"무시된다"}""",
        )
        val rec = Published.of(sock)
        assertEquals("s_abc", rec?.session)
        assertEquals("/tmp/ws1", rec?.workdir)
        assertEquals(42, rec?.pid)
    }

    @Test
    fun `레코드가 없으면 null 이고, 그때는 넘겨짚지 않는다`() {
        assertNull(Published.of(java.nio.file.Paths.get("/nope/none.sock")))
    }
}
