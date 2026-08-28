package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.usecase.Hand
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File
import java.util.concurrent.TimeUnit

/**
 * 진짜 magi MCP 클라이언트가 이 손 서버에 붙는가.
 *
 * **제 시험이 통과하는 것과 코어가 붙는 것은 다른 문장이다.** [HandServerTest] 는 제가 코어를 읽고
 * 옮긴 대화를 겁니다 — 옮기면서 틀렸으면 그 틀린 것에 맞춰 통과한다. 그래서 한 번은 실물을 부른다.
 * 골든을 Go 가 만들고 Kotlin 이 읽는 것과 같은 규율이다(§7).
 *
 * env 로 잠근다: `MAGI_HAND_INTEROP=1` 과 저장소 루트가 필요하다. CI 에는 Go 툴체인이 없을 수
 * 있으므로 기본은 건너뜀이고, **건너뛴 것을 통과로 읽지 않도록** 이름이 그렇게 말한다.
 */
class HandInteropTest {

    private class FakeIde : Hand.Ide {
        override fun show(path: String, line: Int?) = "opened $path"
        override fun replace(path: String, old: String, new: String, all: Boolean) = "replaced in $path"
    }

    @Test
    fun `진짜 magi 클라이언트가 붙고 도구를 등록한다`() {
        if (System.getenv("MAGI_HAND_INTEROP") != "1") return
        val root = File(System.getenv("MAGI_REPO") ?: "../../../..").canonicalFile
        assertTrue(File(root, "go.mod").isFile, "저장소 루트가 아니다: $root")

        HandServer.start(Hand(FakeIde())).use { s ->
            val p = ProcessBuilder("go", "test", "./internal/adapter/mcp/", "-run", "TestHandServerInterop", "-count=1", "-v")
                .directory(root)
                .also {
                    it.environment()["MAGI_HAND_URL"] = s.url
                    it.environment()["MAGI_HAND_TOKEN"] = s.token
                }
                .redirectErrorStream(true)
                .start()
            val out = p.inputStream.bufferedReader().readText()
            p.waitFor(120, TimeUnit.SECONDS)
            assertTrue(out.contains("ATTACHED"), "진짜 클라이언트가 못 붙었다:\n$out")
            assertTrue(p.exitValue() == 0, "Go 시험이 실패했다:\n$out")
        }
    }
}
