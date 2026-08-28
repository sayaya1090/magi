package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

/** 승인 창에 붙는 글자가 **승인되는 글자와 같은지**를 잰다. */
class MarkupTest {

    @Test
    fun `꺾쇠가 태그로 안 먹힌다`() {
        // 실제로 나는 모양이다. 스윙 라벨은 이걸 태그로 읽고 «<done>» 을 통째로 삼킨다 —
        // 사람은 없어진 조각을 못 보고 「허용」을 누른다.
        assertEquals("rm x &amp;&amp; echo &lt;done&gt;", Markup.text("rm x && echo <done>"))
    }

    @Test
    fun `앰퍼샌드를 먼저 바꾼다`() {
        // 순서를 뒤집으면 앞서 넣은 `&lt;` 의 `&` 를 다시 잡아 `&amp;lt;` 가 되고, 화면에는
        // 명령 대신 «&lt;» 라는 글자가 뜬다. 결과가 그럴듯해서 틀린 줄을 모르는 종류다.
        assertEquals("&lt;", Markup.text("<"))
        assertEquals("&amp;lt;", Markup.text("&lt;"))
    }

    @Test
    fun `여러 줄이 한 줄로 뭉치지 않는다`() {
        // 라벨은 날 줄바꿈을 공백으로 만든다. 두 명령이 한 줄로 붙으면 사람이 세는 것과
        // 실행되는 것이 달라진다.
        assertEquals("a<br/>b", Markup.text("a\nb"))
    }
}
