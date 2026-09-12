package dev.sayaya.magi.ide.model

import java.io.File
import org.junit.jupiter.api.Assertions.assertEquals
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
    private val renamed = mapOf(
        "Published" to "Info", "LogEvent" to "Event",
        // 물음의 근거 한 줄. 코어 이름은 `report.Filled` 이고 이쪽은 쓰이는 자리에서 읽히게
        // `Ground` 다 — 「채워진 무엇」보다 「무엇을 근거로」가 이 창에서 하는 일이다.
        "Ground" to "Filled",
    )

    private fun goSources(): List<File> {
        val listed = System.getProperty("magi.wire.origins").orEmpty()
        assertTrue(listed.isNotBlank(), "magi.wire.origins 가 없다 — 빌드가 원천 경로를 안 줬다")
        // 디렉토리도 받는다 — **파일 이름을 적으면 늙는다.** 데몬의 큰 파일 하나를 이름으로 적어 뒀다가
        // 그 파일이 여섯으로 갈린 날 이 시험이 릴리스에서 죽었다(2026-09-03). 구조체는 패키지
        // 안에서 옮겨 다니고, 이 시험이 대조하는 것은 그 구조체이지 파일이 아니다.
        return listed.split(File.pathSeparator).map(::File).flatMap { f ->
            when {
                f.isDirectory -> f.listFiles { c: File -> c.name.endsWith(".go") && !c.name.endsWith("_test.go") }
                    .orEmpty().toList()
                else -> listOf(f)
            }
        }
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
        // `(` 바로 뒤에 붙은 것도 필드다 — `Actor(val kind: …)` 처럼 한 줄로 적은 데이터 클래스가
        // 그렇다. `(?:^|\s)` 만 보면 그 첫 필드가 **통째로 안 보이고**, 이름 대조도 그 칸은 한
        // 번도 안 했다. 이 시험의 짝(아래, 데몬이 보내는 칸을 다 읽나)을 붙이다 드러났다.
        val prop = Regex("""(?:^|[\s(])val (\w+)""")
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

    /**
     * ★ **그 반대 — 데몬이 보내는데 플러그인이 선언 안 한 칸.**
     *
     * 위 시험은 「우리가 읽는 이름이 전선에 있나」를 본다. 이건 짝이고 **더 비싼 쪽**이다:
     * 선언이 없으면 `@Serializable` 이 그 칸을 아예 안 읽으므로, 데몬이 보내는 사실이 화면에
     * 닿을 길이 없고 컴파일도 통과하고 아무것도 안 터진다.
     *
     * 이 부류로 한 세션에서 다섯을 잃었다(전부 VS Code 쪽에서 실측): `tools`(무엇이 붙었나) ·
     * `user`(로그인한 사람 이름) · `council`(카운슬 스위치) · `report`(물음의 근거) ·
     * `context`(창이 얼마나 찼나). 마지막 둘은 이 클라이언트도 같이 놓치고 있었다.
     *
     * 안 읽는 칸은 **사유와 함께** 적는다. 「아직 그 기능이 없다」도 사유이고, 그것을 적어 두는
     * 것과 조용히 빠뜨리는 것의 차이가 이 시험의 전부다.
     */
    @Test
    fun `데몬이 보내는 칸을 전부 읽거나, 안 읽는다고 적었다`() {
        val go = goTags()
        val kt = ktProps()
        assertTrue(kt.size >= 10, "Kotlin 클래스를 ${kt.size}개밖에 못 읽었다 — 스캔이 깨졌다")

        // 안 읽는 칸과 그 사유. 클래스별로 묶는다 — 사유가 같으면 한 줄이 정직하다.
        val skipped = mapOf(
            "ContextState" to setOf(
                // 모델은 상태 표시줄이 `status` 에서 받아 그린다 — 같은 사실을 두 문에서 받아
                // 두 자리에 그리면 둘이 갈린다. 메시지 수는 이 판에 슬롯이 없다.
                "model", "messages",
                // 접기 **한 번의 크기**(lastBefore·lastAfter)와 **총합**(shed). 접혔다는 사실과
                // 남은 주제는 그리고 이 셋은 안 그린다 — 좁은 판에서 숫자 셋을 더 세우면 읽히는
                // 것이 줄고, 「얼마나 잃었나」는 전사를 여는 사람의 물음이다.
                "shed", "lastAt", "lastBefore", "lastAfter",
                // 백엔드 캐시. 코어가 침묵과 0 을 갈라 싣는데(이 기본 백엔드는 침묵한다) 이 판에
                // 그 셋을 정직하게 그릴 자리가 아직 없다 — 자리를 만드는 날 같이 읽는다.
                "cached", "cacheReported",
            ),
            // 회의(meet/meet-join)를 이 클라이언트는 아직 안 한다. 붙이는 날 이 줄이 지워진다.
            "Request" to setOf("minutes", "room", "keep", "owner"),
            // `minutes`: 회의를 안 한다(위).
            // `live`: 전사 스트림이 「재생이 여기서 끝났다」고 대는 표(2026-09-12). 이 창은 스트림을
            // **생중계 꼬리로** 쓰므로 지금 당장 멈출 자리가 없어서 안 읽는다. 다만 이 칸은 이 창이
            // 그릴 값이 **있는** 칸이다 — 「붙는 중」과 「따라잡았고 조용하다」가 지금 같은 그림이고,
            // 사용자가 직접 짚은 불안이 그것이다("뜨는중인지 무슨 문제가 있는지 불안해"). 그 자리를
            // 만드는 날 이 줄이 지워진다.
            "Response" to setOf("minutes", "live"),
            // 플릿 행에 그 슬롯이 없다. 코어가 이 칸들을 로스터에 실은 사유는 「콘솔이 빈칸을
            // 그리고 못 보여 준 값을 바꾸라고 내놓던」 것인데, 이 판은 그 자리를 안 그린다 —
            // 슬롯을 만드는 날 같이 읽는다.
            "RosterRow" to setOf("permission", "backend", "model", "user"),
            // `session-new` 에 이름을 실어 여는 클라이언트만 쓰는 칸(「문서 X의 대화」). 이쪽은
            // 이름 없이 열므로 늘 비어 있고, 비어 있는 것을 목록에 그리면 빈 칸만 는다.
            "SessionRow" to setOf("for"),
            // 소켓 옆의 공표 레코드. 이 플러그인이 읽는 것은 **어느 대화에 붙을지**뿐이고
            // (`SocketPath.Published` — 「최신 세션」으로 넘겨짚지 않으려고 있다), 아래는 플릿
            // 가십이 쓰는 칸이라 이 창에 그릴 자리가 없다.
            "Published" to setOf("can", "does", "waiting", "handling", "addr", "account"),
        )

        val missed = mutableListOf<String>()
        var checked = 0
        for ((cls, props) in kt) {
            val tags = go[renamed[cls] ?: cls] ?: continue
            checked++
            // seq·ts 는 봉투의 것이고 이 표의 대상이 아니다.
            // `-` 는 「전선에 안 실린다」는 뜻이지 칸 이름이 아니다.
            val gap = tags - props - setOf("seq", "ts", "-") - skipped[cls].orEmpty()
            if (gap.isNotEmpty()) missed += "$cls: ${gap.sorted()}"
        }
        assertTrue(checked >= 10, "짝지어 본 클래스가 ${checked}개뿐이다 — 훑을 것이 없으면 이 시험은 늘 초록이다")

        // ★ **면제도 늙는다.** 위 목록은 「안 읽는다」는 **주장**이고, 칸을 나중에 읽기 시작해도
        // 줄은 그대로 남는다 — 그러면 파일에 거짓 사유가 서 있고, 무엇보다 그 칸이 **다시 안
        // 읽히게 됐을 때** 아무도 안 운다. 면제가 조용히 면제를 넓히는 것이다.
        //
        // 실제로 그랬다: `parts`·`topics` 는 「콘솔의 몫이고 여기 슬롯이 없다」는 사유와 함께
        // 적혀 있었는데, 그 사이 둘 다 선언되고 그려졌다. 이제 **읽는 칸을 면제에 적으면 운다.**
        val stale = skipped.flatMap { (cls, names) ->
            names.filter { it in kt[cls].orEmpty() }.map { "$cls.$it" }
        }
        assertTrue(
            stale.isEmpty(),
            "이 칸들은 「안 읽는다」고 적혀 있는데 실제로는 선언돼 있다 — 사유가 낡았고, 면제가 " +
                "살아 있는 한 이 칸이 다시 빠져도 아무도 안 운다: ${stale.sorted()}",
        )
        assertTrue(
            missed.isEmpty(),
            "데몬이 이 칸들을 보내는데 선언이 없어 **읽을 수가 없다** — 화면에 닿을 길이 없고 " +
                "아무것도 안 터진다: $missed. 읽거나, 사유와 함께 skipped 에 적을 것.",
        )
    }

    /** Go 구조체 이름 → (json 이름 → Go 타입). [goTags] 의 짝이되 **모양**까지 든다. */
    private fun goTypes(): Map<String, Map<String, String>> {
        val out = mutableMapOf<String, MutableMap<String, String>>()
        val open = Regex("""^type (\w+) struct \{""")
        val field = Regex("""^\t\w+\s+(\**\[?\]?[\w.\[\]]+)\s+`json:"([^",]+)""")
        for (f in goSources()) {
            if (!f.isFile) continue
            var name: String? = null
            for (line in f.readLines()) {
                open.find(line)?.let { name = it.groupValues[1]; out.getOrPut(name!!) { mutableMapOf() } }
                if (name == null) continue
                if (line == "}") { name = null; continue }
                field.find(line)?.let { out[name]!![it.groupValues[2]] = it.groupValues[1].removePrefix("*") }
            }
        }
        return out
    }

    /** Kotlin 클래스 이름 → (프로퍼티 → 선언된 타입). */
    private fun ktTypes(): Map<String, Map<String, String>> {
        val src = File(System.getProperty("user.dir"), "src/main/kotlin/dev/sayaya/magi/ide/model/Wire.kt")
        assertTrue(src.isFile, "Wire.kt 를 못 찾았다: $src")
        val out = mutableMapOf<String, MutableMap<String, String>>()
        var name: String? = null
        val open = Regex("""^(?:data )?class (\w+)\(""")
        val prop = Regex("""^\s+val (\w+):\s*([\w<>?]+)""")
        for (line in src.readLines()) {
            open.find(line)?.let { name = it.groupValues[1]; out.getOrPut(name!!) { mutableMapOf() } }
            if (name == null) continue
            if (line.startsWith(")")) { name = null; continue }
            prop.find(line)?.let { out[name]!![it.groupValues[1]] = it.groupValues[2] }
        }
        return out
    }

    /**
     * **이름이 맞아도 모양이 틀리면 같은 결함이다.**
     *
     * 옆 규칙은 「우리가 읽는 이름이 데몬이 보내는 이름인가」를 본다. 그것만으로는 모자란다 —
     * VS Code 쪽에서 `RosterRow` **다섯 칸**이 구조체와 모양이 어긋난 채 서 있었다(2026-09-10:
     * `hub` bool→String, `can` int→List, `does` []string→String, `waiting` int→String,
     * `handling` bool→Number). 이름은 전부 맞았다.
     *
     * 왜 조용한가: `ignoreUnknownKeys` 와 `coerceInputValues` 아래에서 모양이 어긋난 칸은 예외가
     * 아니라 **기본값**이 된다. 그리고 그것이 하는 일은 **맞는 읽기를 막는 것**이다 — 저쪽에서
     * `r.waiting > 0` 이 «Operator '>' cannot be applied to types 'string' and 'number'» 로
     * 거절됐고, 그래서 큐 깊이가 화면에 영영 없었다.
     *
     * 이 규칙은 오늘 **어긋난 것이 없음을 재서** 세운다(15쌍 전수). 지키는 것이지 고치는 것이 아니다.
     *
     * ⚠ **이 규칙의 값은 「아무도 안 읽는 칸」에 있다.** 변이로 재 봤다: 읽히는 칸(`waiting`·
     * `handling`·`models`)의 모양을 바꾸면 **코틀린 컴파일러가 먼저 운다** — 그건 이 규칙이 없어도
     * 잡힌다. 반대로 선언만 되고 아무도 안 읽는 칸(`can`)을 바꾸면 **컴파일은 깨끗하고** 이 규칙만
     * 운다. VS Code 에서 다섯이 조용히 어긋나 있던 자리가 정확히 그 부류였다.
     */
    @Test
    fun `플러그인이 읽는 모양은 데몬이 보내는 모양이다`() {
        val go = goTypes()
        val kt = ktTypes()
        val want = mapOf(
            "string" to setOf("String"), "int" to setOf("Int", "Long"), "int64" to setOf("Long", "Int"),
            "float64" to setOf("Double"), "bool" to setOf("Boolean"), "[]string" to setOf("List<String>"),
        )
        // 짝이 0이어도 조용히 초록이 되는 것부터 막는다 — 옆 규칙이 같은 이유로 같은 바닥을 둔다.
        val paired = kt.keys.map { renamed[it] ?: it }.filter { go.containsKey(it) }
        assertTrue(paired.size >= 12,
            "Go 짝을 찾은 클래스가 ${paired.size}개뿐이다 — 이 시험은 소스의 모양을 놓치고 있다")
        assertEquals("Int", kt["RosterRow"]?.get("waiting"),
            "훑기가 아는 칸을 못 읽는다 — 이 규칙이 무엇을 보든 통과한다")

        val drift = mutableListOf<String>()
        for ((cls, props) in kt) {
            val tags = go[renamed[cls] ?: cls] ?: continue
            for ((name, declared) in props) {
                val gt = tags[name] ?: continue
                val ok = want[gt] ?: continue          // 이 규칙이 판정할 수 없는 모양은 건너뛴다
                if (declared.removeSuffix("?") !in ok) {
                    drift += "$cls.$name: 데몬은 `$gt`, 여기는 `$declared`"
                }
            }
        }
        assertTrue(drift.isEmpty(),
            "모양이 어긋난 자리가 있다. 예외가 아니라 **기본값**으로 그려지고, 맞는 읽기는 " +
                "컴파일에서 막힌다:\n  " + drift.sorted().joinToString("\n  "))
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
