package dev.sayaya.magi.ide.ui

/**
 * 승인 모드의 **와이어 토큰과 화면 글자**를 가르는 한 자리.
 *
 * 콤보가 `ask`·`auto`·`allow`·`deny` 를 그대로 세우고 있었다. 그건 프로토콜의 어휘지 화면의
 * 어휘가 아니고, 규약도 콤보 항목은 문장형 대문자를 요구한다(Capitalization). 그런데 데몬으로
 * **나가는 값은 토큰 그대로**여야 한다 — 그래서 모델에는 토큰을 담고 렌더러에서만 바꾼다.
 * 두 세계가 만나는 자리를 한 군데로 몰아 두면, 새 모드가 생겨도 고칠 곳이 하나다.
 *
 * 모르는 토큰은 **그대로 돌려준다.** 데몬이 우리가 모르는 모드를 말하면 지어내는 것보다
 * 날것을 보이는 편이 낫다(§0.5-7 — 모름을 아는 척하지 않는다).
 */
internal object Perms {

    /** 콤보에 세우는 값 — 화면 글자가 아니라 **토큰**이다. [label] 이 그리는 몫을 맡는다. */
    val TOKENS = listOf("ask", "auto", "allow", "deny")

    fun label(token: String?): String = when (token) {
        "ask" -> MagiBundle.msg("perm.ask")
        "auto" -> MagiBundle.msg("perm.auto")
        "allow" -> MagiBundle.msg("perm.allow")
        "deny" -> MagiBundle.msg("perm.deny")
        null -> MagiBundle.msg("set.notsaid")
        else -> token // 모르는 것은 날것으로
    }
}
