package dev.sayaya.magi.ide

import org.junit.jupiter.api.Assertions.assertEquals
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
}
