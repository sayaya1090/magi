package dev.sayaya.magi.ide

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 플러그인 페이지 — 사람이 「깔 것인가」를 정하는 자리.
 *
 * 표·설명·갈 곳 셋 다 **빠져도 아무것도 안 터진다.** 플러그인은 그대로 돌고, 목록에서만
 * 초라해진다 — 그래서 조용히 빠진 채로 오래 간다. 실제로 이 플러그인은 표가 없는 채로 여기까지
 * 왔고, 설명은 두 문장이라 기능 이름이 하나도 없었다. 눈이 커밋마다 보지 않는 자리라 여기서 잰다.
 *
 * 색을 여기 적지 않는다. 같은 마크의 원천은 콘솔의 파비콘(internal/webassets/assets.go 의
 * Icon)이고, 두 벌로 적으면 한쪽만 바뀌어 두 화면의 표가 갈린다. 그 파일을 읽어 대조한다:
 * 못 찾으면 건너뛰는 게 아니라 **실패**한다 — 안 재는 가드는 없는 가드보다 나쁘다.
 */
class PluginPageTest {

    private val plugin = File(System.getProperty("user.dir")).parentFile // plugin/

    /**
     * 콘솔 마크의 원천 — 경로는 **빌드가 넘겨준다**(core/build.gradle.kts 의 `consoleMark`).
     *
     * 스스로 위로 올라가며 찾지 않는 이유는 팔레트 대조가 이미 적어 둔 그대로다: 재는 쪽(시험)과
     * 다시 돌게 하는 쪽(inputs 선언)이 서로 다른 파일을 보게 되면 둘 다 제 몫은 하는데 합쳐서
     * 아무것도 안 막는다.
     */
    private fun consoleMark(): File {
        val at = System.getProperty("magi.console.mark")
            ?: throw AssertionError("magi.console.mark 이 안 넘어왔다 — 빌드가 이 시험의 입력을 선언하지 않으면 UP-TO-DATE 로 조용히 안 돈다")
        val f = File(at)
        assertTrue(f.isFile, "$at 가 없다 — 대조할 원천이 없으면 이 시험은 아무것도 안 재는 것이다")
        return f
    }

    private fun icon(name: String): String {
        val f = File(plugin, "intellij/src/main/resources/META-INF/$name")
        assertTrue(f.isFile, "${f.absolutePath} 가 없다 — 플러그인 목록에 표가 안 선다")
        return f.readText()
    }

    private fun fills(svg: String): List<String> =
        Regex("""fill="(#[0-9A-Fa-f]{6})"""").findAll(svg).map { it.groupValues[1].uppercase() }.toList()

    private fun stroke(svg: String): String =
        Regex("""stroke="(#[0-9A-Fa-f]{6})"""").find(svg)?.groupValues?.get(1)?.uppercase()
            ?: throw AssertionError("고리가 없다 — 원 셋을 하나로 묶는 선이 마크의 절반이다")

    @Test
    fun `밝은 표와 어두운 표가 둘 다 서고, 규격은 40×40이다`() {
        for (name in listOf("pluginIcon.svg", "pluginIcon_dark.svg")) {
            val svg = icon(name)
            // SDK 가 정한 자리 크기다. 다른 수를 적으면 목록에서 흐려지거나 잘린다.
            assertTrue(Regex("""width="40"""").containsMatchIn(svg), "$name 의 width 가 40 이 아니다")
            assertTrue(Regex("""height="40"""").containsMatchIn(svg), "$name 의 height 가 40 이 아니다")
        }
    }

    @Test
    fun `콘솔의 파비콘과 같은 마크다 — 색을 두 벌로 적지 않는다`() {
        val console = consoleMark().readText()
        val at = console.indexOf("const Icon = ")
        assertTrue(at >= 0, "assets.go 에 const Icon 이 없다 — 원천의 이름이 바뀌었다면 여기도 따라가야 한다")
        val mark = console.substring(at, console.indexOf("`", console.indexOf("`", at) + 1) + 1)

        val want = fills(mark)
        assertEquals(3, want.size, "콘솔 마크의 원이 셋이 아니다 — 카운슬이 셋이라는 것이 이 그림의 뜻이다")
        for (name in listOf("pluginIcon.svg", "pluginIcon_dark.svg")) {
            assertEquals(want, fills(icon(name)), "$name 의 색이 콘솔과 다르다 (콘솔=$want)")
            assertEquals(stroke(mark), stroke(icon(name)), "$name 의 고리 색이 콘솔과 다르다")
        }
    }

    @Test
    fun `마크가 자리를 넘지 않는다 — 목록은 이 그림을 제 배경 위에 그대로 얹는다`() {
        // 잘림은 눈으로만 보이고 눈은 커밋마다 보지 않는다. 원의 중심과 반지름으로 잰다.
        val shape = Regex("""cx="([\d.]+)"\s+cy="([\d.]+)"\s+r="([\d.]+)"""")
        for (name in listOf("pluginIcon.svg", "pluginIcon_dark.svg")) {
            val found = shape.findAll(icon(name)).toList()
            assertEquals(4, found.size, "$name 에 원이 넷이 아니다 (셋 + 고리)")
            for (m in found) {
                val (cx, cy, r) = m.destructured.toList().map { it.toDouble() }
                assertTrue(cx - r >= 2 && cx + r <= 38, "$name 의 원이 가로로 자리를 넘는다: $cx ± $r")
                assertTrue(cy - r >= 2 && cy + r <= 38, "$name 의 원이 세로로 자리를 넘는다: $cy ± $r")
            }
        }
    }

    @Test
    fun `페이지에 갈 곳이 있다 — 목록에서 여기 말고는 알아볼 데가 없다`() {
        val xml = File(plugin, "intellij/src/main/resources/META-INF/plugin.xml").readText()
        val head = Regex("""<idea-plugin\b[^>]*>""").find(xml)?.value
            ?: throw AssertionError("<idea-plugin> 을 못 찾았다")
        assertTrue(head.contains("url="), "<idea-plugin> 에 url 이 없다 — 플러그인 페이지에서 나갈 길이 없다")
        val vendor = Regex("""<vendor\b[^>]*>""").find(xml)?.value
            ?: throw AssertionError("<vendor> 를 못 찾았다")
        assertTrue(vendor.contains("url="), "<vendor> 에 url 이 없다")
    }

    /**
     * 설명이 **무엇을 하는지 말하는가.**
     *
     * 문장 수나 글자 수로 재지 않는다 — 그건 길게 쓰면 통과하는 검사고, 길기만 한 설명은
     * 짧은 설명과 똑같이 쓸모없다. 대신 이 플러그인이 **광고하는 액션의 수**를 세어, 설명이
     * 그중 몇 가지를 실제로 이름 대는지 본다. 기능을 붙이면서 설명을 안 고치면 이 비율이
     * 떨어진다.
     */
    @Test
    fun `설명이 광고하는 기능을 이름 댄다`() {
        val xml = File(plugin, "intellij/src/main/resources/META-INF/plugin.xml").readText()
        val body = Regex("""<description><!\[CDATA\[(.*?)]]></description>""", RegexOption.DOT_MATCHES_ALL)
            .find(xml)?.groupValues?.get(1)
            ?: throw AssertionError("<description> 을 못 찾았다")
        // 이 플러그인이 실제로 하는 일의 이름들 — 번들의 액션 글자에서 고른 낱말이다.
        val topics = listOf("review", "commit", "terminal", "approv", "wrote")
        val named = topics.count { body.contains(it, ignoreCase = true) }
        assertTrue(named >= 4,
            "설명이 광고하는 기능 중 $named 가지만 이름 댄다 (${topics.size} 중). " +
                    "플러그인 페이지는 사람이 「깔 것인가」를 정하는 자리다")
        assertTrue(body.contains("<li>"), "설명이 줄글 한 덩어리다 — 목록으로 나눈 것과 읽는 값이 다르다")
    }

    /**
     * 설명에 **이름 엔티티를 쓰지 않는다.**
     *
     * `&mdash;` 로 적었더니 플러그인 목록에 글자 「mdash;」가 그대로 찍혔다(실측 2026-09-03,
     * 샌드박스 IDE 를 띄워 눈으로 봤다) — 이 자리를 그리는 쪽이 `&` 를 먼저 이스케이프해서
     * 엔티티가 풀리지 않는다. CDATA 안이라 XML 파서도 안 건드리고, 어떤 시험도 안 울었다.
     *
     * 숫자 엔티티(`&#8212;`)도 같은 이유로 막는다. 글자를 그대로 쓰면 되는 자리다.
     */
    @Test
    fun `설명에 엔티티를 쓰지 않는다 — 목록에는 글자로 찍힌다`() {
        val xml = File(plugin, "intellij/src/main/resources/META-INF/plugin.xml").readText()
        val body = Regex("""<description><!\[CDATA\[(.*?)]]></description>""", RegexOption.DOT_MATCHES_ALL)
            .find(xml)?.groupValues?.get(1)
            ?: throw AssertionError("<description> 을 못 찾았다")
        val found = Regex("""&(#?\w+);""").findAll(body).map { it.value }.toList()
        assertTrue(found.isEmpty(),
            "설명에 엔티티가 있다: ${found.distinct()} — 목록에는 그 글자가 그대로 찍힌다(실측). 글자를 직접 쓸 것")
    }
}
