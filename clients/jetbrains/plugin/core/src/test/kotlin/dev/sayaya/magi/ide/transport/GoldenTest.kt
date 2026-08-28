package dev.sayaya.magi.ide.transport

import kotlinx.serialization.json.*
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assumptions.assumeTrue
import org.junit.jupiter.api.Test
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths

/**
 * Go 가 뱉은 답과 맞댄다.
 *
 * 기대값을 **아무도 손으로 적지 않는다.** 이 함수에서 이식이 네 번 갈렸는데(대소문자, 반쯤 푼
 * 경로, 절반만 걸은 경로, 안 접힌 `..`) 네 번 다 손으로 적었으면 못 맞혔을 값이었다. 손으로 적은
 * 기대값은 적는 사람이 이미 아는 것만 덮는다.
 *
 * 그래서 골든은 Go 테스트가 실제 `filepath.EvalSymlinks` 와 `WorkspaceKey` 를 돌려 만든다:
 *
 *     MAGI_GOLDEN_UPDATE=1 go test ./internal/adapter/daemon/ -run Golden
 *
 * 경로 해소는 **플랫폼의 사실**이라(볼륨이 대소문자를 접는지, `/tmp` 가 심링크인지) 골든은 어느
 * 플랫폼에서 났는지를 싣고, 다른 데서는 건너뛴다.
 */
class GoldenTest {

    private val golden: JsonObject by lazy {
        val text = javaClass.getResourceAsStream("/socketpath-golden.json")!!
            .readBytes().decodeToString()
        Json.parseToJsonElement(text).jsonObject
    }

    private fun pairsOf(key: String) = golden[key]!!.jsonArray.map { it.jsonArray.map { v -> v.jsonPrimitive.content } }

    @Test
    fun `순수 함수는 골든과 같은 답을 낸다`() {
        pairsOf("shortHash").forEach { (input, want) -> assertEquals(want, SocketPath.shortHash(input), input) }
        pairsOf("sanitize").forEach { (input, want) -> assertEquals(want, SocketPath.sanitize(input), input) }
        pairsOf("socketPath").forEach { (dir, wd, want) ->
            assertEquals(want, SocketPath.of(Paths.get(dir), Paths.get(wd)).toString())
        }
    }

    @Test
    fun `경로 해소가 Go 와 같은 답을 낸다`() {
        val platform = golden["platform"]!!.jsonPrimitive.content
        assumeTrue(platform == goName(), "골든은 $platform 것 — 여기는 ${goName()}, 건너뛴다")

        val root = Files.createTempDirectory("magi-golden").toRealPath()
        build(root)

        for (c in golden["cases"]!!.jsonArray.map { it.jsonObject }) {
            val name = c["name"]!!.jsonPrimitive.content
            val raw = c["input"]!!.jsonPrimitive.content
            if (name == "case-two-spellings-one-key") {
                // 코드가 아니라 볼륨의 성질이다. 두 철자가 한 컴패니언인지.
                val a = SocketPath.workspaceKey(sub(root, "\$ROOT/CaseDir"))
                val b = SocketPath.workspaceKey(sub(root, "\$ROOT/casedir"))
                if (c["real"]!!.jsonPrimitive.content == "different") assertNotEquals(a, b, name) else assertEquals(a, b, name)
                continue
            }
            val input = sub(root, raw)
            val got = SocketPath.evalSymlinks(input)
            if (c["errors"]?.jsonPrimitive?.booleanOrNull == true) {
                // Go 가 에러를 내면 WorkspaceKey 는 안 푼 경로를 그대로 쓴다. 반쯤 푼 것을 내면 안 된다.
                assertEquals(input, got, "$name — 에러 자리에서 입력을 그대로 돌려줘야 한다")
            } else {
                assertEquals(sub(root, c["real"]!!.jsonPrimitive.content), got, name)
            }
        }
    }

    private fun sub(root: Path, s: String): Path = Paths.get(s.replace("\$ROOT", root.toString()))

    /** 골든이 사람이 읽는 줄로 실어 온 배치를 그대로 세운다. */
    private fun build(root: Path) {
        for (line in golden["layout"]!!.jsonArray.map { it.jsonPrimitive.content }) {
            when {
                line.startsWith("mkdir ") ->
                    Files.createDirectories(root.resolve(line.removePrefix("mkdir ")))
                line.startsWith("symlink ") -> {
                    val (from, to) = line.removePrefix("symlink ").split(" -> ", limit = 2)
                    val target = if (to.startsWith("\$ROOT")) sub(root, to) else Paths.get(to)
                    Files.createDirectories(root.resolve(from).parent)
                    Files.createSymbolicLink(root.resolve(from), target)
                }
                else -> error("골든의 layout 에 모르는 줄이 있다: $line")
            }
        }
    }

    private fun goName(): String = when {
        System.getProperty("os.name").orEmpty().startsWith("Mac") -> "darwin"
        System.getProperty("os.name").orEmpty().startsWith("Windows") -> "windows"
        else -> "linux"
    }
}
