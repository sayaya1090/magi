package dev.sayaya.magi.ide.model

import kotlinx.serialization.Serializable

/**
 * **`magi ide-bridge` 가 답하는 행과 그 변화** — 이 창이 제 셰이퍼 대신 읽게 될 모양.
 *
 * 왜 [Wire] 와 따로인가: 저쪽은 **데몬의** 전선이고 이쪽은 **브리지의** 전선이다. 같은 파일에 두면
 * 「어느 프로세스가 이 말을 하나」가 흐려지고, 그 물음이 이 트리에서 값을 치른 자리다(문 하나에
 * 붙어 있던 규칙, `#197`).
 *
 * ⚠ **아직 아무도 안 읽는다.** 이관은 셋으로 나뉘어 있고 이것이 첫째다 — 모양을 먼저 세우고, 그
 * 위에 전송로를, 그 위에 그리는 코드를 올린다. 순서가 그렇게 된 이유는 이 파일을 쓰다 나왔다:
 * 부분적인 모델은 **조용히 칸을 버린다**(`@Serializable` 은 선언 없는 칸을 아예 안 읽는다), 그래서
 * 전송로보다 모양이 먼저다. 이 클래스들은 `WireConformanceTest` 가 정본 구조체와 대조한다.
 */
@Serializable
data class BridgeRow(
    val at: String = "",
    val seq: Long = 0,
    val draft: Boolean = false,
    val who: String = "",
    val text: String = "",
    val callId: String = "",
    val args: String = "",
    val ok: Boolean? = null,
    val note: Boolean = false,
    val round: Int = 0,
    val decision: String = "",
    val silent: Boolean = false,
    val lens: String = "",
    val rule: String = "",
    val opened: Boolean = false,
    val readOnly: Boolean = false,
    val evidence: String = "",
    val cite: String = "",
    val keep: String = "",
    val thought: String = "",
    val confidence: Double? = null,
    val queued: Boolean = false,
    val out: String = "",
    val pending: Boolean = false,
    val abandoned: Boolean = false,
    val msgId: String = "",
    val member: String = "",
    val folded: Boolean = false,
    val summary: String = "",
    val id: String = "",
)

/**
 * 한 행 목록에 대한 **변화 하나**. 낱말은 여섯이고(`reset`·`add`·`grow`·`patch`·`drop`·`move`)
 * 정본은 `internal/adapter/idebridge/live.go` 다 — 발명이 아니라 실측으로 정해진 목록이다.
 *
 * [after] 는 번호가 아니라 **이름**이다. 번호는 「내가 이걸 보낼 때 당신이 갖고 있던 목록」을 뜻하고,
 * 프레임을 놓친 클라이언트는 알아차릴 방법 없이 다른 줄을 고친다.
 */
@Serializable
data class BridgeOp(
    val op: String = "",
    val id: String = "",
    val row: BridgeRow? = null,
    val rows: List<BridgeRow> = emptyList(),
    val text: String = "",
    val after: String = "",
)

/**
 * 구독이 내는 **프레임 한 줄**. 첫 답은 `rows` 와 `sub` 를, 그 뒤는 `ops` 를, 끝은 `done` 과 (있으면)
 * `why` 를 싣는다.
 *
 * ⚠ **이 모양은 정본 구조체와 대조되지 않는다.** 브리지는 문의 답을 `map[string]any` 로 쓰므로
 * 짝지을 구조체가 없다 — `BridgeRow`·`BridgeOp` 와 달리 이 세 칸은 **그 문의 시험만**이 붙들고 있다.
 * 적어 두는 것과 모르는 것의 차이가 여기서는 전부다: 문의 답을 타입으로 바꾸는 것은 브리지 전체의
 * 관용을 바꾸는 일이라 이 조각에 안 넣었다.
 */
@Serializable
data class BridgeFrame(
    val id: Int = 0,
    val ok: Boolean? = null,
    val error: String = "",
    val sub: Int = 0,
    val rows: List<BridgeRow> = emptyList(),
    val events: Int = 0,
    val ops: List<BridgeOp> = emptyList(),
    val done: Boolean = false,
    val why: String = "",
)
