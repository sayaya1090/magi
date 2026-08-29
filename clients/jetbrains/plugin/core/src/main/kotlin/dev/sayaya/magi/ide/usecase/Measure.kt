package dev.sayaya.magi.ide.usecase

/**
 * 산문 폭 판정 — **한 벌**.
 *
 * IDE 쪽 상한 래퍼의 doLayout 과 getPreferredSize 가 「이 폭에서 글은 몇 픽셀인가」를 각자
 * 적어 두던 동안 width==0(콜드 첫 패스·스플리터 0 폭)에서 갈라졌다(리뷰): 선호크기 패스는
 * 상한, 레이아웃 패스는 0 — 검증 사이클마다 서로 되돌린다. 규칙을 두 벌 적으면 안 재지는
 * 쪽이 갈라지므로, 판정을 순수 함수로 내려 시험 소스셋이 있는 여기서 못박는다(intellij
 * 모듈엔 시험이 없다 — 거기 남겨 두면 실물 공식을 고쳐도 아무것도 안 운다).
 *
 * 답: 판 폭을 모르거나(0 이하) 상한 이상이면 상한, 그 사이면 판 폭.
 */
object Measure {
    fun proseWidth(panel: Int, cap: Int): Int = if (panel in 1 until cap) panel else cap
}
