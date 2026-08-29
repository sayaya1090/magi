package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

/**
 * 두 화면이 같은 물건을 그리므로 같은 색으로 그려야 한다.
 *
 * [Palette] 의 KDoc 이 "원본을 그대로 옮겨 적었다"고 말한다. 이 시험이 없으면 그 문장은 적힌
 * 순간에만 참이고, 거짓이 되는 순간에 아무것도 안 운다 — 이 저장소가 찾아낸 결함의 절반이 그
 * 모양이었다. 웹은 같은 문을 `cmd/magi-web/palette_test.go` 로 이미 세워 뒀고, 이것은 그
 * 코틀린 판이다.
 *
 * **한 방향으로만 잰다.** IDE 가 이름 대는 역할은 원본에 있어야 하고 값이 같아야 한다. 원본에
 * 있는데 여기 없는 것은 결함이 아니다 — 터미널의 diff 배경이나 문법 색은 IDE 가 그릴 것이
 * 아니다. 반대로 여기 있는데 원본에 없는 이름은 **IDE 가 색을 지어낸 것**이라 실패다.
 *
 * ### 안 도는 채로 초록일 수 있는 자리
 *
 * 이 시험이 읽는 파일은 이 모듈이 컴파일할 때 아무도 안 보는 파일이라, 그것만으로는 gradle 이
 * 이 작업을 UP-TO-DATE 로 건너뛴다 — 원본이 바뀌어도 안 돈다. 그래서 `core/build.gradle.kts`
 * 가 그 파일을 **입력으로 적어 두고** 경로를 시스템 속성으로 넘긴다. 경로를 여기서 짐작해
 * 올라가지 않는 이유가 그것이다: 짐작한 경로는 입력 선언과 갈라질 수 있고, 갈라지면 재는 쪽과
 * 다시 돌게 하는 쪽이 서로 다른 파일을 본다.
 *
 * 못 찾으면 **건너뛰지 않고 운다.** 원본이 사라진 채 초록인 것은 대조가 없는 것과 같다.
 */
class PaletteTest {

    private fun origin(): String {
        val path = System.getProperty("magi.palette.origin")
        assertTrue(!path.isNullOrBlank(),
            "`magi.palette.origin` 이 안 넘어왔다 — `core/build.gradle.kts` 가 그 속성을 세우고, " +
                "같은 경로를 작업 입력으로도 적는다. 속성이 없으면 이 대조는 안 돈 것이다")
        val f = File(path)
        assertTrue(f.isFile, "색의 원본을 못 찾았다: ${f.absolutePath}. 옮겨졌으면 빌드 파일의 " +
            "경로를 같이 고쳐야 한다 — 못 찾은 것을 통과로 세면 색은 그때부터 아무도 안 본다")
        return f.readText()
    }

    /** `var <name> = palette{…}` 리터럴 하나를 읽는다. */
    private fun paletteIn(src: String, name: String): Map<String, String> {
        val at = src.indexOf("var $name = palette{")
        assertTrue(at >= 0, "원본에 `$name` 이 없다 — 이름이 바뀌었으면 이 시험이 그것을 알아야 한다")
        var depth = 0
        var i = src.indexOf('{', at)
        val from = i + 1
        while (i < src.length) {
            if (src[i] == '{') depth++
            if (src[i] == '}') {
                depth--
                if (depth == 0) break
            }
            i++
        }
        val body = Regex("//[^\n]*").replace(src.substring(from, i), "")
        val found = Regex("\"([A-Za-z]+)\"\\s*:\\s*\"(#[0-9A-Fa-f]{6})\"")
            .findAll(body)
            .associate { it.groupValues[1] to it.groupValues[2] }
        // 파서가 빈 맵을 주면 아래 대조가 전부 "원본에 없는 이름"으로 우는 대신 조용히 통과할 수도
        // 있는 모양이 생긴다. 바닥을 둔다 — 원본은 이 수보다 훨씬 많은 역할을 갖고 있다.
        assertTrue(found.size >= 20,
            "`$name` 에서 ${found.size} 개만 읽었다. 리터럴의 모양이 바뀌어 이 파서가 원본을 " +
                "제대로 안 보고 있다")
        return found
    }

    @Test
    fun `IDE 의 색은 터미널의 색이다`() {
        val src = origin()
        val dark = paletteIn(src, "nervDark")
        val light = paletteIn(src, "nervLight")

        for ((role, ink) in Palette.roles) {
            val d = dark[role]
            val l = light[role]
            assertTrue(d != null && l != null,
                "`Palette` 가 `$role` 을 이름 대는데 원본은 그 역할을 모른다 — IDE 가 색을 " +
                    "지어냈거나 원본에서 없어진 이름을 붙들고 있다")
            assertEquals(d!!.lowercase(), ink.dark.lowercase(), "어두운 $role")
            assertEquals(l!!.lowercase(), ink.light.lowercase(), "밝은 $role")
        }

        // 목록이 통째로 비면 위의 루프는 한 번도 안 돌고 초록이 된다.
        assertTrue(Palette.roles.size >= 15,
            "IDE 가 이름 대는 역할이 ${Palette.roles.size} 개뿐이다 — 목록이 줄어 이 대조가 " +
                "화면이 실제로 칠하는 것을 더 이상 안 보고 있다")
    }

}
