package dev.sayaya.magi.ide

import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 매뉴얼(docs/MANUAL.ko.md)이 플러그인의 광고면을 전부 싣는지 잰다 — 사용자 지시:
 * "매뉴얼에 빠진 기능이 있으면 안 돼."
 *
 * 목록을 여기 두 벌 적지 않는다(두 벌이면 안 재지는 쪽이 갈라진다) — 광고면의 원천인
 * plugin.xml 에서 **유도**한다: 사람이 보는 액션 이름(text=) 전부와, 확장점의 존재가
 * 곧 기능인 것들(창·위젯·자동완성·설정·인텐션·리스너). 새 기능이 plugin.xml 에 광고되면
 * 매뉴얼에 적히기 전까지 이 시험이 빨갛다.
 *
 * 한계도 적어 둔다: 창 **안**의 기능(접기, 칩, 탭…)은 XML 에 광고되지 않아 기계로 못
 * 유도한다 — 그쪽의 빠짐은 유닛 사이클의 적대 리뷰가 맡는다.
 */
class ManualTest {

    private val root = File(System.getProperty("user.dir")).parentFile // plugin/

    private fun read(f: File): String {
        assertTrue(f.isFile, "${f.absolutePath} 가 없다 — 시험이 아무것도 안 보고 있다")
        return f.readText()
    }

    @Test
    fun `매뉴얼은 플러그인이 광고한 기능을 전부 싣는다`() {
        val manual = read(File(root.parentFile, "docs/MANUAL.ko.md"))
        val xml = read(File(root, "intellij/src/main/resources/META-INF/plugin.xml"))

        // 사람이 메뉴에서 보는 이름 전부 — text 속성에서 유도.
        val texts = Regex("""text="([^"]+)"""").findAll(xml).map { it.groupValues[1] }.toList()
        assertTrue(texts.size >= 5, "plugin.xml 에서 액션 이름이 ${texts.size}개뿐이다 — 유도가 깨졌다")
        for (t in texts) assertTrue(
            t in manual,
            "매뉴얼에 액션 「$t」 이 없다 — 광고된 기능은 전부 매뉴얼에 적는다(사용자 지시)",
        )

        // 확장점의 존재가 곧 기능인 것들: XML 에 이 광고가 있으면 매뉴얼에 그 이야기가 있어야
        // 한다. 왼쪽이 없어져도 실패한다 — 기능을 접었으면 매뉴얼과 이 대응도 같이 접는 것.
        val stories = mapOf(
            "toolWindow id=\"magi\"" to "하단 독",
            "toolWindow id=\"magi.plan\"" to "magi.plan",
            "statusBarWidgetFactory" to "상태 표시줄",
            "inline.completion.provider" to "자동완성",
            "projectConfigurable" to "Settings",
            "intentionAction" to "Alt+Enter",
            "notificationGroup" to "풍선",
            "OpenBufferListener" to "자동 동봉",
        )
        for ((ad, story) in stories) {
            assertTrue(ad in xml, "plugin.xml 에서 「$ad」 광고가 사라졌다 — 기능을 접었으면 매뉴얼과 이 표에서도 접을 것")
            assertTrue(story in manual, "매뉴얼에 「$story」 이야기가 없다 — plugin.xml 의 「$ad」 가 광고하는 기능이다")
        }
    }
}
