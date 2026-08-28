package dev.sayaya.magi.ide.usecase

/**
 * 이 플러그인이 컴패니언에 도구 서버로 붙을 때 쓰는 이름.
 *
 * 규칙이 산문으로만 있으면 다음 사람이 다르게 고른다. 그래서 값과 그 값이 지켜야 하는 성질을
 * 여기 두고 테스트가 건다. 사유는 `clients/jetbrains/README.md` §4에 있다.
 */
object McpName {

    /**
     * **`jetbrains`.** 고정이고, 인스턴스마다 다르게 짓지 않는다.
     *
     * 붙고 나면 도구가 `mcp__jetbrains__apply_edit` 로 목록에 들어간다. 그런데 `mcp__` 접두는
     * 무조건 위험 도구라(`internal/app/execute.go`) 파일 하나 여는 데도 매번 확인이 뜨고, 그걸
     * 면하는 **유일한 수단이 오퍼레이터가 쓰는 allow 룰**이다. 이름에 PID 나 창 번호를 넣으면
     * 그 룰이 재시작마다 무효가 된다 — 유일한 완화책을 못 쓰게 만드는 이름은 고를 이유가 없다.
     *
     * IDEA 만이 아니라 PyCharm·GoLand 도 같은 플랫폼이라 제품명이 아니라 플랫폼 이름을 쓴다.
     */
    const val VALUE = "jetbrains"

    /**
     * 코어가 이름을 다듬는 규칙(`mcp/manager.go` 의 `sanitizeToolPart`). `[a-zA-Z0-9_-]` 만 남고
     * 나머지는 `_` 가 된다.
     *
     * 여기 두는 이유는 [VALUE] 가 **자기 자신으로 다듬어지는지**를 걸기 위해서다. 다듬어진 뒤에
     * 달라지는 이름을 고르면, 사람이 목록에서 보는 이름과 allow 룰에 적어야 하는 이름이 갈린다.
     */
    fun sanitize(s: String): String {
        // 코드포인트로 걷는다. `for (ch in s)` 는 UTF-16 코드 유닛을 도는데 원본은 룬 단위라,
        // 서로게이트 쌍이 Go 에서는 `_` 하나이고 char 로 걸으면 `__` 둘이 된다. 이 저장소가 이미
        // 한 번 밟은 함정이다 — `SocketPath.sanitize` 의 주석이 같은 것을 경고한다. 여기서 그
        // 교훈 앞의 모양으로 돌아가 있었다.
        val b = StringBuilder(s.length)
        var i = 0
        while (i < s.length) {
            val cp = s.codePointAt(i)
            val keep = cp in 'a'.code..'z'.code || cp in 'A'.code..'Z'.code ||
                cp in '0'.code..'9'.code || cp == '_'.code || cp == '-'.code
            b.append(if (keep) cp.toChar() else '_')
            i += Character.charCount(cp)
        }
        // 빈 이름은 네임스페이스가 될 수 없다. 코어가 "x" 로 대신한다.
        return if (b.isEmpty()) "x" else b.toString()
    }

    /** 붙기를 거절당했을 때, 사유가 둘이고 사람이 할 일이 다르다. */
    enum class Refusal {
        /** 같은 이름이 이미 붙어 있다 — 이 워크스페이스의 손이 이미 있다는 뜻이다. */
        ALREADY_ATTACHED,

        /** 다른 이름이 다듬어진 뒤 이 이름과 같아졌다 — 설정에 그런 서버가 적혀 있다는 뜻이다. */
        COLLIDES_AFTER_SANITISE,

        /** 그 외. 서버에 못 닿았거나 정책이 막았거나. */
        OTHER,
    }

    /**
     * 코어가 돌려준 사유 문장을 갈래로 읽는다. 문장을 그대로 사람에게 보이되, **무엇을 해야 하는지**는
     * 갈래마다 다르므로 여기서 나눈다. 코어의 두 문장은 `mcp/manager.go` 의 `registerClient` 에 있다.
     */
    fun refusalOf(error: String?): Refusal = when {
        error == null -> Refusal.OTHER
        error.contains("collides with") -> Refusal.COLLIDES_AFTER_SANITISE
        error.contains("already attached") -> Refusal.ALREADY_ATTACHED
        else -> Refusal.OTHER
    }
}
