package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.Waiting
import dev.sayaya.magi.ide.transport.DaemonClient

/**
 * 붙어 있는 컴패니언에게 말을 걸고, 그가 묻는 것에 답한다.
 *
 * 여기 있는 것은 **전사가 없어도 되는 것들**이다. 전사는 데몬에 `transcript` 문이 생겨야 오지만
 * (설계 문서 §3), 지시를 보내는 것과 프롬프트에 답하는 것은 지금 있는 메서드로 된다. 그래서
 * 먼저 붙인다 — 사람이 기다리는 화면을 빈 채로 두지 않는 것이 §2의 규칙이기도 하다.
 */
class Companion(
    private val client: DaemonClient,
    private val session: String,
) {

    /**
     * 사람이 친 것을 보낸다.
     *
     * 턴이 돌고 있으면 `steer`, 아니면 `submit` 이다. **부르는 쪽이 고르지 않는다** — 데몬에게
     * 지금 무엇을 하는지 물어서 정한다. 화면이 기억한 상태로 고르면, 그 사이 다른 뷰어가
     * 인터럽트했거나 턴이 끝났을 때 조용히 틀린 메서드를 부른다.
     */
    fun say(text: String): Response {
        val method = if (turnIsOpen()) "steer" else "submit"
        return client.exchange(Request(method = method, session = session, text = text))
    }

    /** 지금 사람을 기다리고 있으면 그 프롬프트, 아니면 null. */
    fun waiting(): Waiting? = status().waiting

    /**
     * 퍼미션 프롬프트에 답한다. 결정은 코어가 쓰는 낱말 그대로 — `allow` | `deny` | `always`.
     * 불리언으로 옮기지 않는 이유는 한 결정에 어휘가 둘이면 언젠가 갈라져서다(데몬 쪽 주석).
     */
    fun allow(callId: String) = decide(callId, "allow")
    fun deny(callId: String) = decide(callId, "deny")
    fun always(callId: String) = decide(callId, "always")

    private fun decide(callId: String, decision: String): Response =
        client.exchange(Request(method = "permission", session = session, callId = callId, decision = decision))

    /** 선택지가 있는 질문에 답한다. 퍼미션과 메서드가 다르다(`answer`). */
    fun answer(callId: String, answer: String): Response =
        client.exchange(Request(method = "answer", session = session, callId = callId, answer = answer))

    /** 돌고 있는 턴을 세운다. */
    fun interrupt(): Response = client.exchange(Request(method = "interrupt", session = session))

    private fun status(): Response = client.exchange(Request(method = "status", session = session))

    /**
     * 턴이 열려 있나. `doing` 은 도는 툴이 자기에 대해 마지막으로 말한 것이라, 값이 있으면 무언가
     * 돌고 있다는 뜻이다. 사람을 기다리는 중(`waiting`)도 턴 안이므로 열린 것으로 센다 — 그때
     * 보낸 말은 새 대화가 아니라 지금 턴에 얹혀야 한다.
     */
    private fun turnIsOpen(): Boolean {
        val s = status()
        return s.waiting != null || !s.doing.isNullOrBlank()
    }
}
