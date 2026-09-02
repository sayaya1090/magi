package dev.sayaya.magi.ide

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 플러그인의 표 — 있는가, 규격인가, 그리고 **콘솔과 같은 마크인가**.
 *
 * 이 그림은 플러그인 목록·마켓플레이스·업데이트 알림에 뜬다. 없으면 IDE 가 이름의 첫 글자로
 * 회색 판을 그리고, 그건 고장이 아니라 「아직 아무도 안 만든 것」처럼 읽힌다 — 조용히 빠져도
 * 아무 시험도 울지 않는 자리라 여기서 잰다.
 *
 * 색을 여기 적지 않는다. 같은 마크의 원천은 콘솔의 파비콘(internal/webassets/assets.go 의
 * Icon)이고, 두 벌로 적으면 한쪽만 바뀌어 두 화면의 표가 갈린다. 그 파일을 읽어 대조한다:
 * 못 찾으면 건너뛰는 게 아니라 **실패**한다 — 안 재는 가드는 없는 가드보다 나쁘다.
 */
class PluginIconTest {

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
}
