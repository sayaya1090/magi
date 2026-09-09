package dev.sayaya.magi.ide.usecase

/**
 * 데몬 자동 재기동 예산 및 주기 제어 규칙.
 *
 * 초기에는 "프로젝트당 1회" 기동 정책이었으나, 초기 기동 실패 또는 런타임 크래시 발생 시 자동 복구 경로가 단절되는 결함이 있었다.
 * (2026-09-01 실측: 기동 2분 30초 후 데몬 종료 시 10분간 UI에 "실행되지 않음" 상태가 지속되고 재기동 시도가 발생하지 않음).
 *
 * 반면 매 폴링 주기(3초)마다 무조건 기동을 시도할 경우 대량의 좀비 프로세스가 생성될 위험이 있으므로,
 * 최대 재시도 횟수([attempts], 기본 3회)와 최소 재시도 간격([interval], 기본 60초)을 결합하여 예산을 관리한다.
 * 예산 소진 시 추가 기동을 중단하여 지속적 장애 환경에서의 리소스 낭비를 방지한다.
 */
class Restarts(
    private val attempts: Int = 3,
    private val interval: Long = 60_000,
) {
    private var used = 0

    /**
     * 마지막 기동 시각 (epoch ms).
     * `Long.MIN_VALUE`를 센티넬 값으로 사용할 경우 `now - MIN_VALUE`의 산술 오버플로로 인해 음수가 발생하여
     * `< interval` 조건이 항상 참이 됨으로써 첫 기동 시도부터 거부되는 결함이 발생할 수 있으므로 널러블(`Long?`) 타입을 사용한다.
     */
    private var last: Long? = null

    /**
     * 재기동 허용 여부를 검사하고, 허용 시 시도 횟수를 원자적으로 1 차감(소진)한다.
     * 검사와 갱신 간 경합 조건(TOCTOU)을 방지하기 위해 단일 동기화 블록으로 처리한다.
     */
    @Synchronized
    fun take(now: Long): Boolean {
        if (used >= attempts) return false
        last?.let { if (now - it < interval) return false }
        used++
        last = now
        return true
    }

    /**
     * 데몬 연결 성공 시 재기동 예산을 초기화한다.
     * 장시간 실행되는 IDE 세션에서 과거의 일시적 장애 이력으로 인해 향후 복구가 영구 차단되지 않도록 보장한다.
     */
    @Synchronized
    fun ok() {
        used = 0
        last = null
    }

    @get:Synchronized
    val spent: Boolean get() = used >= attempts
}
