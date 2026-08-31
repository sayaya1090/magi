package dev.sayaya.magi.ide.live

import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

/**
 * **재는 기계를 잰다.** [Shapes] 는 모델이 있어야 도는 시험 안에서만 쓰이므로, 이 시험이
 * 없으면 평소 CI 에서 한 줄도 안 실행된다 — 그리고 정작 모델을 갈아 끼운 날 판정이 고장나
 * 있으면 「전부 통과」로 보인다.
 *
 * 먹이는 것은 **실제로 겪은 모양들**이다. 지어낸 것이 아니라 이 저장소가 라이브에서 만나
 * 주석과 커밋에 적어 둔 것들이다.
 */
class ShapesTest {

    private val prefix = "fun add(a: Int, b: Int): Int {\n    return "

    @Test
    fun `멀쩡한 완성은 안 불평한다`() {
        assertNull(Shapes.insertable("a + b", prefix))
        assertNull(Shapes.insertable("a + b\n", prefix))
    }

    @Test
    fun `펜스로 감싼 완성을 잡는다`() {
        assertNotNull(Shapes.insertable("```kotlin\na + b\n```", prefix))
    }

    @Test
    fun `머리말이 붙은 완성을 잡는다`() {
        assertNotNull(Shapes.insertable("Here's the completion:\na + b", prefix))
        assertNotNull(Shapes.insertable("다음은 완성입니다:\na + b", prefix))
        // 멀쩡한 코드가 우연히 걸리면 안 된다 — 넓게 짜면 그쪽이 더 비싸다.
        assertNull(Shapes.insertable("a + b // sure", prefix))
    }

    @Test
    fun `접두를 되풀이한 완성을 잡는다`() {
        assertNotNull(Shapes.insertable("fun add(a: Int, b: Int): Int {\n    return a + b\n}", prefix))
    }

    @Test
    fun `한 자리에 소설을 쓰면 잡는다`() {
        assertNotNull(Shapes.insertable((1..20).joinToString("\n") { "line $it" }, prefix))
    }

    @Test
    fun `제안은 여러 줄이면 잡는다`() {
        assertNull(Shapes.oneLine("이 저장소에서 무엇을 고칠까?"))
        assertNotNull(Shapes.oneLine("첫 줄\n둘째 줄\n셋째 줄"))
        assertNotNull(Shapes.oneLine("```\n무엇\n```"))
    }

    @Test
    fun `훑어보기가 줄에 안 붙으면 잡는다`() {
        // 계약 모양(탭)과, 실측된 어긋난 모양(구분자 없이 붙여 보냄 — 이것도 붙어야 한다).
        assertNull(Shapes.anchored("2\t인덱스가 범위를 넘을 수 있다", 8))
        assertNull(Shapes.anchored("2 인덱스가 범위를 넘을 수 있다", 8))
        // 줄번호가 아예 없으면 전부 띠로 밀린다.
        assertNotNull(Shapes.anchored("인덱스가 범위를 넘을 수 있습니다.", 8))
        // 없는 줄을 짚으면 인레이가 엉뚱한 데 붙거나 안 붙는다.
        assertNotNull(Shapes.anchored("99\t뭔가 이상하다", 8))
    }

    @Test
    fun `커밋 초안의 모양을 잡는다`() {
        assertNull(Shapes.commitShape("ide: 소켓 갈래를 파일 종류로 가른다\n\n사유는…"))
        assertNotNull(Shapes.commitShape("```\nfix: something\n```"))
        assertNotNull(Shapes.commitShape("Here is a commit message:\nfix: something"))
        assertNotNull(Shapes.commitShape("x".repeat(120)))
    }

    @Test
    fun `빈 답은 어느 문에서도 통과가 아니다`() {
        for (c in listOf(Shapes.insertable(""), Shapes.oneLine(""), Shapes.anchored("", 3), Shapes.commitShape(""))) {
            assertNotNull(c, "빈 답을 통과시켰다 — 「할 말이 없다」와 「못 받았다」가 같아 보인다")
        }
    }
}
