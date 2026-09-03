package dev.sayaya.magi.ide.usecase

/**
 * 예약 편집기가 **무엇을 보낼지** 정하는 규칙.
 *
 * 잡은 묻거나(prompt) 돌거나(command) 둘 중 하나다 — 데몬이 둘 다인 잡도, 아무것도 없는 잡도
 * 거부한다. 화면은 라디오 한 쌍으로 그 배타를 만들 수 없게 하는데, 그 라디오가 **실제로 무엇을
 * 보내는가**는 화면 코드 안쪽에 숨어 있어 안 재진다. 그래서 그 한 줄을 여기로 꺼냈다.
 *
 * 잠긴 칸에 남은 글자를 안 보내는 것이 핵심이다: 종류를 바꾸면 반대편 칸은 비활성이 될 뿐
 * 글자는 그대로 남아 있고, 그걸 같이 보내면 데몬이 「둘 다」로 읽고 거부한다.
 */
object Schedules {
    /** 편집기가 채운 칸들. 빈 문자열은 "안 보낸다"는 뜻으로 데몬이 읽는다. */
    data class Edit(val prompt: String, val command: String, val timeout: String)

    /**
     * @param running 「명령을 돕니다」 쪽이 켜져 있나
     * @param prompt  묻는 말 칸의 글자 (잠겨 있어도 남아 있다)
     * @param command 명령 칸의 글자
     * @param timeout 시한 칸의 글자
     */
    fun edit(running: Boolean, prompt: String, command: String, timeout: String): Edit =
        if (running) Edit("", command.trim(), timeout.trim())
        else Edit(prompt.trim(), "", "")
}
