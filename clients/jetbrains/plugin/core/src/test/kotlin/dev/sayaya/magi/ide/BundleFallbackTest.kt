package dev.sayaya.magi.ide

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.Locale
import java.util.ResourceBundle

/**
 * 화면 글자가 **영어를 청했는데 한국어로 뜨던** 기전을 못박는다.
 *
 * 사용자가 「다른 설정은 다 영문인데 우리꺼만 한글이야」로 잡은 그 결함이다. 원인을 한동안
 * 「언어팩이 없으면 IDE 로케일이 JVM 기본으로 샌다」로 적어 두었는데, 라이브 로그가 그것을
 * 뒤집었다 — 언어팩이 **셋** 있는 기계에서 IDE 로케일은 정확히 `en` 이었다. 즉 그 갈래는
 * 이 결함을 만든 적이 없다.
 *
 * 진짜 기전은 **자바의 폴백 규칙**이고, 그건 플랫폼이 아니라 JDK 의 것이라 여기서 잴 수 있다:
 * `ResourceBundle` 의 기본 컨트롤은 청한 로케일에서 못 찾으면 **`Locale.getDefault()` 로**
 * 한 번 더 간다. 이 기계의 JVM 기본이 `ko_KR` 이라, 영어를 청해도 `_ko` 파일이 있으면 그것이
 * 뽑혔다. 인텔리제이 자신의 번들은 `_ko` 를 안 실어서 그 함정에 안 걸리고, 그래서 **우리 판만**
 * 한국어였다.
 *
 * 그래서 `MagiBundle` 은 `getNoFallbackControl` 을 쓴다. 이 시험은 그 선택이 **무엇을 막는지**
 * 를 두 컨트롤의 차이로 보인다 — 누가 그 인자를 지우면 여기가 먼저 빨개진다.
 */
class BundleFallbackTest {

    private val path = "messages.ProbeBundle"

    private fun with(default: Locale, body: () -> Unit) {
        val was = Locale.getDefault()
        try { Locale.setDefault(default); ResourceBundle.clearCache(javaClass.classLoader); body() } finally {
            Locale.setDefault(was); ResourceBundle.clearCache(javaClass.classLoader)
        }
    }

    @Test
    fun `기본 컨트롤은 영어를 청해도 JVM 기본으로 새어 한국어를 준다`() = with(Locale.KOREA) {
        val b = ResourceBundle.getBundle(path, Locale.ENGLISH, javaClass.classLoader)
        assertEquals(
            "korean", b.getString("probe.hello"),
            "이 줄이 「base」로 바뀌면 자바의 폴백 규칙이 변한 것이다 — 그때는 아래 시험의 사유가 사라진다",
        )
    }

    @Test
    fun `폴백을 끄면 청한 대로 영어가 온다`() = with(Locale.KOREA) {
        val b = ResourceBundle.getBundle(
            path, Locale.ENGLISH, javaClass.classLoader,
            ResourceBundle.Control.getNoFallbackControl(ResourceBundle.Control.FORMAT_PROPERTIES),
        )
        assertEquals(
            "base", b.getString("probe.hello"),
            "영어를 청했는데 한국어가 왔다 — 사용자가 「우리꺼만 한글」로 잡은 그 결함",
        )
    }

    @Test
    fun `한국어를 청하면 폴백을 꺼도 한국어가 온다`() = with(Locale.ENGLISH) {
        val b = ResourceBundle.getBundle(
            path, Locale.KOREA, javaClass.classLoader,
            ResourceBundle.Control.getNoFallbackControl(ResourceBundle.Control.FORMAT_PROPERTIES),
        )
        assertEquals(
            "korean", b.getString("probe.hello"),
            "폴백을 끈 것이 번역까지 막으면 한국어팩 사용자가 영어를 본다",
        )
    }

    /**
     * 그리고 **번들을 지나기는 하나.** 위의 폴백 고침은 번들을 지나는 글자만 고친다 — 소스에
     * 박힌 한국어에는 닿을 방법이 없고, 영어 IDE 에서 그대로 한국어로 뜬다. 사용자가 잡은
     * 증상(「다른 설정은 다 영문인데 우리꺼만 한글이야」)이 그 자리에서는 그대로 남는다.
     *
     * 실측(2026-09-09): 플러그인 소스의 한국어 문자열 69개 중 **열일곱**이 사람이 읽는 싱크로
     * 가고 있었다. 이 시험은 그 싱크들만 본다 — `LOG.*` 는 개발자 글자라 세지 않고, `core` 는
     * 번들이 없는 모듈이라(플랫폼 없이 도는 것이 그 모듈의 조건) 여기서 요구하지 않는다.
     *
     * 목록이 아니라 **훑는다**: 새 `tell(...)` 한 줄이 늘고 아무도 목록을 안 고치면 목록형
     * 규칙은 통과하면서 결함이 나간다.
     */
    @Test
    fun `사람이 읽는 자리에 박힌 한국어가 없다`() {
        val ui = java.io.File("../intellij/src/main/kotlin")
        val files = ui.walkTopDown().filter { it.isFile && it.extension == "kt" }.toList()
        assertTrue(files.isNotEmpty(), "$ui 에서 .kt 를 하나도 못 찾았다 — 이 가드는 아무것도 안 읽고 있다")

        val hangul = Regex("[가-힣]")
        // 사람이 읽는 싱크. 이 목록이 늘어나는 것은 좋다 — 줄어들면 가드가 눈머는 것이다.
        val sinks = Regex("""\b(tell|report|handSaid|toolTipText|push\(problems)""")
        val offenders = mutableListOf<String>()
        var scanned = 0
        for (f in files) {
            val body = f.readText()
                .replace(Regex("""/\*.*?\*/""", RegexOption.DOT_MATCHES_ALL), "")
                .lines().map { it.replace(Regex("//.*$"), "") }
            for ((i, line) in body.withIndex()) {
                if (line.contains("LOG.")) continue
                if (!sinks.containsMatchIn(line)) continue
                scanned++
                for (m in Regex("\"((?:[^\"\\\\]|\\\\.)*)\"").findAll(line)) {
                    val lit = m.groupValues[1]
                    if (hangul.containsMatchIn(lit)) offenders += "${f.name}:${i + 1}  $lit"
                }
            }
        }
        // 스캐너가 죽으면 「위반 없음」과 구별이 안 된다 — 훑은 줄이 있었는지부터 못박는다.
        assertTrue(scanned >= 20, "사람이 읽는 자리를 $scanned 줄밖에 못 찾았다 — 스캔이 깨진 것이지 코드가 깨끗한 게 아니다")
        assertEquals(
            emptyList<String>(), offenders,
            "영어 IDE 에서도 한국어로 뜬다 — MagiBundle 로 옮길 것(EN/KO 둘 다):\n" + offenders.joinToString("\n"),
        )
    }
}
