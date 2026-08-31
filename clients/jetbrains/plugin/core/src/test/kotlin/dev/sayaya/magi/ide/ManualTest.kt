package dev.sayaya.magi.ide

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
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
        val en = props(File(root, "intellij/src/main/resources/messages/MagiBundle.properties"))
        val ko = props(File(root, "intellij/src/main/resources/messages/MagiBundle_ko.properties"))

        // 액션 이름은 이제 plugin.xml 이 아니라 **번들**에 산다(IDE 언어팩을 따르려고 옮겼다).
        // 그래서 가드도 그리로 따라간다 — 규칙이 사는 곳이 바뀌면 재는 곳도 바뀐다.
        // **선택 디스크립터도 센다.** 액션 하나가 저 파일들에 살면서 이 가드 밖에 있었고,
        // 그래서 「글자는 번들에서 온다」는 규칙만 조용히 초록이었다 — 그 액션은 글자를 XML 에
        // 박고 있었다(가이드라인 검토 G1). 훑는 곳이 규칙이 사는 곳보다 좁으면 가드는 반쯤 죽는다.
        val meta = File(root, "intellij/src/main/resources/META-INF")
        val descriptors = meta.walkTopDown().filter { it.isFile && it.extension == "xml" }.sortedBy { it.name }.toList()
        assertTrue(descriptors.size >= 2, "META-INF 에 디스크립터가 ${descriptors.size}개뿐이다 — 유도가 깨졌다")
        // 글자를 실을 수 있는 **요소 전부**와 속성 **둘 다**를 잰다. 처음엔 `<action … text=`
        // 하나만 봤는데, 잡아 낸 그 파일은 `description=` 도 같이 박고 있었다 — 반쪽만 재는
        // 가드는 되박는 손을 절반 확률로 통과시킨다(리뷰 R8). `override-text` 는 SDK 가
        // 「자리별 글자」에 쓰라고 문서화한 요소라 다음에 제일 먼저 닿을 자리다.
        val hardcoded = Regex("""<(action|group|separator|override-text)\b[^>]*\s(text|description)\s*=\s*["']""")
        for (d in descriptors) assertFalse(
            hardcoded.containsMatchIn(d.readText()),
            "${d.name} 이 액션 글자를 직접 적는다 — 글자는 번들에서 온다(SDK Action System)",
        )
        // 확장점의 표시 이름도 같은 규칙이다. 액션 계열만 재던 동안 `projectConfigurable
        // displayName=` 을 되박아도 가드는 초록이었고, 그 셋이 하필 이 규칙으로 옮긴 자리였다
        // (리뷰 R8). 표시 이름을 받는 확장점은 전부 `bundle`+`key` 를 받는다.
        for (d in descriptors) assertFalse(
            Regex("""\sdisplayName\s*=\s*["']""").containsMatchIn(d.readText()),
            "${d.name} 이 확장점 표시 이름을 직접 적는다 — `bundle`+`key` 로 번들에서 가져올 것",
        )
        val all = descriptors.joinToString("\n") { it.readText() }
        val ids = Regex("""<action id="([^"]+)"""").findAll(all).map { it.groupValues[1] }.toList()
        assertTrue(ids.size >= 5, "디스크립터에서 액션이 ${ids.size}개뿐이다 — 유도가 깨졌다")
        for (id in ids) {
            val key = "action.$id.text"
            assertTrue(key in en, "영어 번들에 「$key」 가 없다 — 언어팩 없는 IDE 가 빈 글자를 본다")
            assertTrue(key in ko, "한국어 번들에 「$key」 가 없다 — 번역이 빠졌다")
            // 설명도 같이 잰다: 글자만 옮기고 설명을 XML 에 두고 오면 그 액션의 툴팁만 안 따른다.
            val desc = "action.$id.description"
            assertTrue(desc in en, "영어 번들에 「$desc」 가 없다 — 툴팁이 빈다")
            assertTrue(desc in ko, "한국어 번들에 「$desc」 가 없다 — 번역이 빠졌다")
            val name = ko.getValue(key)
            assertTrue(
                name in manual,
                "매뉴얼에 액션 「$name」 이 없다 — 광고된 기능은 전부 매뉴얼에 적는다(사용자 지시)",
            )
        }
        // 스트라이프 버튼 글자 — 없으면 도구창 **id 가 그대로** 사람 앞에 뜬다(오른쪽 독에
        // `magi.plan` 이 떠 있었다). 창을 새로 만들 때마다 같은 일이 나므로 유도해서 잰다.
        val windows = Regex("""<toolWindow id="([^"]+)"""").findAll(xml).map { it.groupValues[1] }.toList()
        assertTrue(windows.size >= 2, "plugin.xml 에서 도구창이 ${windows.size}개뿐이다 — 유도가 깨졌다")
        for (id in windows) {
            val key = "toolwindow.stripe.$id"
            assertTrue(key in en, "영어 번들에 「$key」 가 없다 — 스트라이프에 id 「$id」 가 그대로 뜬다")
            assertTrue(key in ko, "한국어 번들에 「$key」 가 없다 — 번역이 빠졌다")
        }

        // 두 번들의 열쇠 집합이 같아야 한다: 한쪽에만 있는 열쇠는 그 언어에서 빈 글자다.
        assertEquals(en.keys, ko.keys, "번들 두 벌의 열쇠가 갈렸다 — 갈린 쪽 언어가 글자를 잃는다")

        // 모든 값은 MessageFormat 을 지난다(`MagiBundle.msg`). 그래서 두 가지를 잰다:
        //  ① 홑따옴표는 반드시 짝으로 — 하나만 적으면 그 뒤의 `{0}` 이 글자로 굳는다. 전에는
        //     「인자를 안 받는 값이면 괜찮다」였고, 그 면제는 누가 그 값에 자리표시자를 하나
        //     넣는 순간 사라진다(한 줄 편집 거리의 함정 — 리뷰 R10).
        //  ② 패턴 자체가 깨지지 않는다 — 짝 안 맞는 중괄호는 화면이 아니라 예외로 나온다.
        // 값이 **빈 채로** 실리는 것을 막는다. 여러 줄 문구를 쓰려다 실제 줄바꿈을 파일에 적으면
        // 자바 프로퍼티는 그 줄에서 값을 끊고, 다음 줄은 **값 없는 열쇠**가 된다 — 화면에는 문장의
        // 앞 토막만 뜨고 파일은 겉보기에 멀쩡하다(실측: `core.get.body` 가 그 모양으로 깨졌다).
        // 여러 줄은 `\n` 두 글자로 적는다.
        for ((lang, rows) in listOf("en" to en, "ko" to ko)) for ((k, v) in rows) {
            assertTrue(v.isNotBlank(), "[$lang] 「$k」 의 값이 비었다 — 값 안에 진짜 줄바꿈을 적으면 이렇게 끊긴다")
        }
        for ((lang, rows) in listOf("en" to en, "ko" to ko)) for ((k, v) in rows) {
            assertFalse(
                "'" in v.replace("''", ""),
                "[$lang] 「$k」 의 홑따옴표가 짝이 아니다 — MessageFormat 이 뒤의 자리표시자를 먹는다: $v",
            )
            val ok = runCatching { java.text.MessageFormat(v).format(arrayOf("1", "2", "3")) }.isSuccess
            assertTrue(ok, "[$lang] 「$k」 가 MessageFormat 패턴으로 안 선다: $v")
        }

        val stories = mapOf(
            "toolWindow id=\"magi\"" to "하단 독",
            "toolWindow id=\"magi.plan\"" to "magi.plan",
            "statusBarWidgetFactory" to "상태 표시줄",
            "inline.completion.provider" to "자동완성",
            "projectConfigurable" to "Settings",
            "intentionAction" to "Alt+Enter",
            "notificationGroup" to "풍선",
            "OpenBufferListener" to "자동 동봉",
            "editorNotificationProvider" to "띠",
            "postStartupActivity" to "입력 중 검토",
        )
        for ((ad, story) in stories) {
            assertTrue(ad in xml, "plugin.xml 에서 「$ad」 광고가 사라졌다 — 기능을 접었으면 매뉴얼과 이 표에서도 접을 것")
            assertTrue(story in manual, "매뉴얼에 「$story」 이야기가 없다 — plugin.xml 의 「$ad」 가 광고하는 기능이다")
        }
    }

    /** `.properties` 를 열쇠→값으로. 유니코드 이스케이프(\uXXXX)를 푼다 — 번들이 그렇게 읽는다. */
    private fun props(f: File): Map<String, String> {
        val p = java.util.Properties()
        // **UTF-8 로 읽는다.** `load(InputStream)` 은 ISO-8859-1 로 읽어 한글이 깨진다 —
        // 플랫폼은 플러그인 번들을 UTF-8 로 읽으므로 시험도 같은 눈이어야 한다.
        f.reader(Charsets.UTF_8).use { p.load(it) }
        return p.entries.associate { (k, v) -> k.toString() to v.toString() }
    }
}
