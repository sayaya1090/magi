package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Waiting
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

/**
 * 화면 둘이 한 벌씩 적어 두고 있던 판정을 여기서 잰다.
 *
 * 이 시험이 새로 생긴 것이 요점이다. 같은 `when` 이 `StatusBar` 와 `MagiConfigurable` 에 있을
 * 때는 **잴 자리가 없었다** — `intellij` 모듈에는 테스트 소스셋이 아예 없어서, 그쪽 글자를
 * 무엇으로 바꿔도 스위트는 초록이었다. 실제로 그 둘은 이미 갈라져 있었고 갈라진 쪽이 안 재지는
 * 쪽이었다([Activity] 의 주석).
 */
class ActivityTest {

    private fun facts(doing: String? = null, waiting: Waiting? = null) =
        Companion.Facts(doing = doing, permission = null, session = "s1", waiting = waiting,
            model = null, backend = null)

    private val asked = Waiting(id = "c1", kind = "permission", what = "bash")

    @Test
    fun `기다림이 돎을 이긴다`() {
        // 사람을 막아 세운 컴패니언도 턴 안이라 `doing` 이 **같이** 실려 온다. 순서를 뒤집으면
        // 화면은 「도는 중」이라 적고, 사람이 답해야 나아가는 상태가 저절로 끝날 상태와 같아
        // 보인다 — 아무도 답하지 않아 영영 안 끝난다.
        assertEquals(Activity.Waiting, Activity.of(facts(doing = "go test ./...", waiting = asked)))
    }

    @Test
    fun `도는 것을 그대로 실어 보낸다`() {
        // 판은 이 글자를 그대로 그린다. 안 실으면 「무언가 돈다」까지만 남고 무엇이 도는지가
        // 사라지는데, 화면에는 여전히 한 줄이 떠 있어 잃은 티가 안 난다.
        assertEquals(Activity.Doing("go test ./..."), Activity.of(facts(doing = "go test ./...")))
    }

    @Test
    fun `안 말한 것을 쉬는 중으로 접지 않는다`() {
        // 이 갈래가 「안 도는 중」이 되는 순간 화면은 데몬이 한 적 없는 말을 한다. 상태 표시줄이
        // 정확히 그렇게 적혀 있었다.
        assertEquals(Activity.Unsaid, Activity.of(facts()))
    }
}
