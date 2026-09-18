package dev.sayaya.magi.ide.model

import kotlinx.serialization.Serializable

/**
 * **`magi ide-bridge` 응답 행(Row) 및 갱신 오퍼레이션 DTO.**
 *
 * [Wire]와 분리 사유: [Wire]는 데몬 소켓 프로토콜 통신 모델이며, 본 파일은 `ide-bridge` 통신 모델입니다.
 * 프로세스 경계 및 라이프사이클이 상이하므로 DTO 정의를 명확히 분리하여 혼선을 방지합니다.
 *
 * 점진적 마이그레이션 순서: 데이터 모델 정의 -> 전송 계층 연동 -> UI 렌더링 순으로 적용합니다.
 * `@Serializable` 기반 역직렬화 시 미정의 필드는 기본적으로 무시되므로, 스키마 누락을 방지하기 위해
 * 모델을 선행 정의하고 `WireConformanceTest`를 통해 정본 구조체와 정합성을 대조합니다.
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
 * 한 행 목록에 대한 **변화 단위(Operation)**. (`reset`, `add`, `grow`, `patch`, `drop`, `move`)
 * 원본 명세는 `internal/adapter/idebridge/live.go`를 따릅니다.
 *
 * [after]는 행의 인덱스 번호가 아니라 행 고유 식별자 [id]입니다. 인덱스 번호는 프레임 유실 시
 * 엉뚱한 행을 변경할 수 있으므로, 식별자 기반으로 삽입 위치를 명시합니다.
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
 * 브리지 세션 구독 스트림의 프레임 단위 DTO.
 * 초기 응답은 전체 행 목록([rows])과 구독 식별자([sub])를 전달하고, 이후 변경 사항은 [ops]로 전달되며,
 * 종료 시 [done] 플래그 및 사유([why])가 전달됩니다.
 *
 * 브리지 측 반환 형식이 동적 맵(`map[string]any`)인 경우 정적 Go 구조체와의 1:1 매핑 대신
 * 단위 테스트(`BridgeRowsTest`)를 통해 필드 형태와 직렬화 정합성을 검증합니다.
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
