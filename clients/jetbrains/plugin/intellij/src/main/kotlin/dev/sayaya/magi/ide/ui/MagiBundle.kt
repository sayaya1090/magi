package dev.sayaya.magi.ide.ui

import com.intellij.DynamicBundle
import org.jetbrains.annotations.Nls
import org.jetbrains.annotations.PropertyKey

private const val PATH = "messages.MagiBundle"

/**
 * 화면 글자의 한 자리. **IDE 가 정한 언어를 따른다** — 한글이 코드에 박혀 있어 영어 IDE 에서도
 * 한글이 나왔다(사용자 지적). 기본은 영어이고, 한국어 언어팩이 깔린 IDE 는 `_ko` 짝을 본다.
 *
 * **상속이 아니라 위임이다.** JetBrains 의 현행 권장이 그렇다(SDK Internationalization:
 * "prefer delegation to inheritance", `DynamicBundle(Class, String)` 생성자) — 상속하면
 * 번들이 플랫폼 클래스의 확장 지점을 물려받아 나중에 갈아타기 어렵다.
 *
 * 액션 글자는 여기 안 부른다: `plugin.xml` 의 `<resource-bundle>` 과 `action.<id>.text` /
 * `.description` 규약이 자동으로 가져간다.
 *
 * 로그는 옮기지 않는다 — 개발자가 읽는 증거고, 번역되면 검색이 갈라진다.
 */
object MagiBundle {
    private val INSTANCE = DynamicBundle(MagiBundle::class.java, PATH)

    @Nls
    fun msg(@PropertyKey(resourceBundle = PATH) key: String, vararg params: Any): String =
        INSTANCE.getMessage(key, *params)
}
