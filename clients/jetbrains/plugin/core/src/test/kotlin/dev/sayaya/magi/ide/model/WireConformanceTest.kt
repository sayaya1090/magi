package dev.sayaya.magi.ide.model

import java.io.File
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * **이 플러그인이 읽는 필드 이름이 데몬이 보내는 이름과 같은가.**
 *
 * [Wire] 의 `Json` 은 `ignoreUnknownKeys = true` 다 — 그래야 새 필드를 실은 데몬에 옛 플러그인이
 * 붙어도 안 터진다. 그 관용의 값은 반대편에서 치른다: **이름이 어긋나면 예외가 아니라 기본값**이다.
 * `command` 를 `cmd` 로 적으면 커맨드 잡은 「명령이 없는 잡」으로 그려지고, 아무것도 실패하지 않는다.
 *
 * 이 저장소는 그 결함을 코어에서 이미 겪었다 — `encoding/json` 이 미선언 인자를 조용히 버려
 * 「묻지 않은 질문」에 정상 응답을 하던 자리(25291 콜 중 387건). 여기는 같은 결함의 반대 방향이고,
 * 지금까지 아무도 안 보고 있었다. 필드는 손으로 옮겨 적는다 — 2026-09-03 에 `CronRow` 에
 * `command`·`timeout` 둘을 그렇게 넣었고, 오타였으면 조용했을 것이다.
 *
 * ## 한 방향만 잰다
 *
 * Kotlin 이 아는 이름은 전부 Go 에 있어야 한다. **반대는 아니다** — 플러그인이 데몬의 모든 필드를
 * 읽을 이유가 없고, 안 읽는 것은 결함이 아니라 선택이다. 그래서 Go 에만 있는 이름은 통과다.
 */
class WireConformanceTest {

    /** Kotlin 클래스 → 그 짝인 Go 구조체. 이름이 같으면 안 적는다. */
    private val renamed = mapOf("Published" to "Info", "LogEvent" to "Event")

    private fun goSources(): List<File> {
        val listed = System.getProperty("magi.wire.origins").orEmpty()
        assertTrue(listed.isNotBlank(), "magi.wire.origins 가 없다 — 빌드가 원천 경로를 안 줬다")
        return listed.split(File.pathSeparator).map(::File)
    }

    /** Go 구조체 이름 → 그 구조체가 내보내는 json 태그들. */
    private fun goTags(): Map<String, Set<String>> {
        val out = mutableMapOf<String, MutableSet<String>>()
        val open = Regex("""^type (\w+) struct \{""")
        for (f in goSources()) {
            if (!f.isFile) continue
            var name: String? = null
            for (line in f.readLines()) {
                val m = open.find(line)
                if (m != null) { name = m.groupValues[1]; out.getOrPut(name) { mutableSetOf() }; continue }
                if (name == null) continue
                if (line == "}") { name = null; continue }
                // `json:"socket"` 도 `json:"sighting,omitempty"` 도 같은 이름을 낸다. 콤마 앞까지만
                // 본다 — 옵션을 이름의 일부로 읽으면 전부 어긋난 것으로 보인다.
                Regex("""`json:"([^",]+)""").find(line)?.let { out[name]!!.add(it.groupValues[1]) }
            }
        }
        return out
    }

    /** Kotlin 클래스 이름 → 그 클래스가 읽는 이름들(@SerialName 이 있으면 그쪽). */
    private fun ktProps(): Map<String, Set<String>> {
        val src = File(System.getProperty("user.dir"), "src/main/kotlin/dev/sayaya/magi/ide/model/Wire.kt")
        assertTrue(src.isFile, "Wire.kt 를 못 찾았다: $src")
        val out = mutableMapOf<String, MutableSet<String>>()
        val open = Regex("""^(?:data )?class (\w+)\(""")
        val serial = Regex("""@SerialName\("([^"]+)"\)""")
        // `val x` 는 어노테이션과 **같은 줄**에 오기도 한다(`@SerialName("ageSeconds") val ageSeconds`).
        // 줄머리만 보면 그 줄을 필드로 못 세고, 그때 아래의 새는 일이 시작된다.
        val prop = Regex("""(?:^|\s)val (\w+)""")
        var name: String? = null
        // 「앞줄에 선 @SerialName」. 클래스가 열리거나 닫힐 때 반드시 지운다 — 안 지우면 한 클래스의
        // 마지막 어노테이션이 **다음 클래스의 첫 필드 이름**이 된다. 처음 이 시험을 돌렸을 때
        // 실제로 그랬고, Jobs 와 Waiting 두 곳에서 있지도 않은 드리프트를 보고했다.
        var renameNext: String? = null
        for (line in src.readLines()) {
            val m = open.find(line)
            if (m != null) {
                name = m.groupValues[1]
                renameNext = null
                val props = out.getOrPut(name!!) { mutableSetOf() }
                // 한 줄에 다 적힌 클래스 — `data class Actor(val kind: String = "", …)`. 여는 줄이
                // 곧 닫는 줄이라 아래의 `startsWith(")")` 를 영영 못 만나고, 그대로 두면 이 클래스가
                // **다음 클래스의 필드를 통째로 삼킨다**(Actor 가 ConfigItem 아홉 칸을 먹었다).
                if (line.count { it == '(' } == line.count { it == ')' }) {
                    prop.findAll(line).forEach { props.add(it.groupValues[1]) }
                    name = null
                }
                continue
            }
            if (name == null) continue
            if (line.startsWith(")")) { name = null; renameNext = null; continue }
            val here = serial.find(line)?.groupValues?.get(1)
            val p = prop.find(line)
            when {
                p != null -> { out[name]!!.add(here ?: renameNext ?: p.groupValues[1]); renameNext = null }
                here != null -> renameNext = here
            }
        }
        return out
    }

    @Test
    fun `플러그인이 읽는 이름은 전부 데몬이 보내는 이름이다`() {
        val go = goTags()
        val kt = ktProps()

        // 이 시험이 **아무것도 안 짚고 통과하는 것**을 먼저 막는다. 정규식이 소스의 모양을 놓치면
        // 짝이 0개가 되고, 그때도 아래 루프는 조용히 초록이다.
        val paired = kt.keys.map { renamed[it] ?: it }.filter { go.containsKey(it) }
        assertTrue(paired.size >= 12,
            "Go 짝을 찾은 클래스가 ${paired.size}개뿐이다 — 이 시험은 소스의 모양을 놓치고 있고, " +
                "표에 무엇이 적혀 있든 통과한다. 짝: $paired")
        assertTrue(go.values.sumOf { it.size } >= 100,
            "Go 쪽에서 읽은 json 태그가 ${go.values.sumOf { it.size }}개뿐이다 — 원천을 잘못 읽고 있다")

        val drift = mutableListOf<String>()
        val unpaired = mutableListOf<String>()
        for ((cls, props) in kt) {
            val twin = renamed[cls] ?: cls
            val tags = go[twin]
            if (tags == null) { unpaired += "$cls(→$twin)"; continue }
            val only = props - tags
            if (only.isNotEmpty()) drift += "$cls: ${only.sorted()} — Go 의 $twin 은 이 이름을 안 보낸다"
        }
        assertTrue(drift.isEmpty(),
            "이름이 어긋난 자리가 있다. ignoreUnknownKeys 라 예외가 아니라 **기본값**으로 그려지므로 " +
                "화면은 「없다」고 말하고 아무것도 실패하지 않는다:\n  " + drift.joinToString("\n  "))
        assertTrue(unpaired.isEmpty(),
            "Go 짝을 못 찾은 클래스가 있다 — 원천 파일이 목록에서 빠졌거나 구조체 이름이 바뀌었다. " +
                "이름이 다르면 renamed 에 적을 것: $unpaired")
    }
}
