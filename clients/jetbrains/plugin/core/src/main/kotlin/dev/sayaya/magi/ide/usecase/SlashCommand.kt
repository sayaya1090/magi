package dev.sayaya.magi.ide.usecase

/**
 * 입력이 슬래시 명령의 모양인가 — `/이름 나머지` 를 (이름, 나머지)로 가른다.
 *
 * 모양만 본다. 그 이름이 정말 명령인지는 데몬이 내놓는 목록(`commands`)이 정한다 — 목록에 없는
 * `/낱말` 은 지금처럼 모델에게 가는 말이다(경로 `/usr/bin` 을 묻는 문장이 명령으로 먹히면 안 된다).
 * `//` 로 시작하면 명령이 아니다.
 */
object SlashCommand {
    fun split(typed: String): Pair<String, String>? {
        val t = typed.trim()
        if (!t.startsWith("/") || t.startsWith("//")) return null
        val body = t.substring(1)
        val name = body.takeWhile { !it.isWhitespace() }
        if (name.isEmpty()) return null
        return name to body.substring(name.length).trim()
    }
}
