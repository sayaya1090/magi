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

    /** 골든이 실어 온 "그래서 어떻게 고치나". 실패 메시지에 붙인다 — 그러라고 실린 필드다. */
    private fun why(): String = golden["regenerate"]!!.jsonPrimitive.content

    private val golden: JsonObject by lazy {
        val text = javaClass.getResourceAsStream("/socketpath-golden.json")!!
            .readBytes().decodeToString()
        Json.parseToJsonElement(text).jsonObject
    }

    private fun pairsOf(key: String) = golden[key]!!.jsonArray.map { it.jsonArray.map { v -> v.jsonPrimitive.content } }

    @Test
    fun `순수 함수는 골든과 같은 답을 낸다`() {
        pairsOf("shortHash").forEach { (i, want) -> assertEquals(want, SocketPath.shortHash(i), "$i\n${why()}") }
        pairsOf("sanitize").forEach { (i, want) -> assertEquals(want, SocketPath.sanitize(i), "$i\n${why()}") }
        pairsOf("socketPath").forEach { (dir, wd, want) ->
            assertEquals(want, SocketPath.of(Paths.get(dir), Paths.get(wd)).toString(), why())
        }
    }

    @Test
    fun `경로 해소가 Go 와 같은 답을 낸다`() {
        val platform = golden["platform"]!!.jsonPrimitive.content
        assumeTrue(platform == goName(), "골든은 $platform 것 — 여기는 ${goName()}, 건너뛴다")

        // `$ROOT` 는 **만들어진 그대로**의 임시 디렉토리다 — macOS 에서는 그 자체가 /var → /private/var
        // 뒤에 있고, 그 **긴 사슬**이 재려는 것이다. 해소한 형태를 쓰면 walker 가 Go 가 잰 것보다
        // 한 홉 짧은 사슬을 걷는다(골든의 Go 쪽 주석이 그 드리프트를 겪고 적어 두었다).
        // 답은 해소된 형태 기준이고 그쪽 자리표가 `$REAL` 이다.
        val root = Files.createTempDirectory("magi-golden")
        val real = root.toRealPath()
        build(root)

        // 철자 둘이 한 컴패니언인가. 사용자가 실제로 겪는 증상("철자 둘, 소켓 둘")을 키 수준에서
        // 잰다. 볼륨이 대소문자를 접을 때가 진짜 시험이다 — 같은 디렉토리인데 키가 둘이어야 한다.
        // 안 접는 볼륨에서는 애초에 다른 디렉토리라 검사가 약하고, 골든이 그 사실을 싣는다.
        val folds = golden["caseFolds"]!!.jsonPrimitive.boolean
        assertNotEquals(
            SocketPath.workspaceKey(sub(root, real, "\$ROOT/CaseDir")),
            SocketPath.workspaceKey(sub(root, real, "\$ROOT/casedir")),
            "철자 둘이 한 키가 됐다 — 대소문자를 접고 있다" +
                (if (folds) " (이 볼륨은 접는다: 진짜 시험)" else " (이 볼륨은 안 접는다: 약한 시험)") +
                "\n" + why(),
        )

        for (c in golden["cases"]!!.jsonArray.map { it.jsonObject }) {
            val name = c["name"]!!.jsonPrimitive.content
            val raw = c["input"]!!.jsonPrimitive.content
            val input = sub(root, real, raw)
            val got = SocketPath.evalSymlinks(input)
            if (c["errors"]?.jsonPrimitive?.booleanOrNull == true) {
                // Go 가 에러를 내면 WorkspaceKey 는 안 푼 경로를 그대로 쓴다. 반쯤 푼 것을 내면 안 된다.
                assertEquals(input, got, "$name — 에러 자리에서 입력을 그대로 돌려줘야 한다\n${why()}")
            } else {
                assertEquals(sub(root, real, c["real"]!!.jsonPrimitive.content), got, "$name\n${why()}")
            }
        }
    }

    private fun sub(root: Path, real: Path, s: String): Path =
        Paths.get(s.replace("\$REAL", real.toString()).replace("\$ROOT", root.toString()))

    /** 골든이 사람이 읽는 줄로 실어 온 배치를 그대로 세운다. */
    private fun build(root: Path) {
        for (line in golden["layout"]!!.jsonArray.map { it.jsonPrimitive.content }) {
            when {
                line.startsWith("mkdir ") ->
                    Files.createDirectories(root.resolve(line.removePrefix("mkdir ")))
                line.startsWith("symlink ") -> {
                    val (from, to) = line.removePrefix("symlink ").split(" -> ", limit = 2)
                    // layout 의 타깃은 만들어진 그대로의 root 를 가리킨다. $REAL 은 답 쪽 자리표라
                    // 여기서는 다루지 않는다 — 둘을 같은 값으로 넣으면 방금 가른 구분이 도로 흐려진다.
                    val target = if (to.contains("\$REAL")) error("layout 타깃에 \$REAL 이 있다: $to")
                    else if (to.startsWith("\$ROOT")) Paths.get(to.replace("\$ROOT", root.toString()))
                    else Paths.get(to)
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
