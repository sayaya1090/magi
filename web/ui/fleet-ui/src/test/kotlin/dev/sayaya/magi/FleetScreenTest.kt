package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe

/**
 * 화면 관통 테스트 — 가짜 포트(FakeFleetRepository)의 고정 명단으로 그린 DOM을 검사한다.
 * 언어 팩은 테스트 페이지에서 못 읽으므로(fetch 실패 → 키 폴백), 문구가 아니라 구조를 잰다.
 */
@GwtHtml("fleettest.html")
internal class FleetScreenTest : GwtTestSpec({
    Given("두 팀과 다섯 상태의 고정 명단") {
        When("화면이 그려지면") {
            Then("요약 타일은 4개, 이 콘솔이 잰 것만 센다(elsewhere 제외)") {
                page.waitForSelector("#summary md-filter-chip")
                page.locator("#summary md-filter-chip").count() shouldBe 4
                page.locator("#summary md-filter-chip.waiting .n").textContent() shouldBe "1"
                page.locator("#summary md-filter-chip.working .n").textContent() shouldBe "1"
                page.locator("#summary md-filter-chip.idle .n").textContent() shouldBe "1"
                page.locator("#summary md-filter-chip.gone .n").textContent() shouldBe "1"
            }
            Then("행은 다섯: elsewhere 포함 전부 그려진다") {
                page.locator("#fleet .card").count() shouldBe 5
            }
            Then("골칫거리 먼저 — 첫 행은 waiting이고 딴 데 것은 맨 뒤다") {
                page.locator("#fleet .card").first().getAttribute("class").contains("waiting") shouldBe true
                page.locator("#fleet .card").last().getAttribute("class").contains("remote") shouldBe true
            }
            Then("팀 헤더 둘: alpha(막힌 팀이라 먼저) 그리고 무명") {
                page.locator("#fleet .teamhead").count() shouldBe 2
                page.locator("#fleet .teamhead .tname").first().textContent() shouldBe "alpha"
            }
            Then("허브는 팀 헤더가 말한다") {
                page.locator("#fleet .teamhead .thub").count() shouldBe 1
            }
            Then("퍼미션 질문엔 네 결정이 한 무게로 선다") {
                page.locator("#fleet .answer .bgroup md-outlined-button").count() shouldBe 4
            }
            Then("working 행은 플랜 진행(2/5)과 툴 보고 줄을 단다") {
                page.locator("#fleet .card.working .plan").textContent() shouldBe "2/5"
                page.locator("#fleet .card.working .note").count() shouldBe 1
            }
            Then("뒤처진 빌드는 스스로 말한다(behind)") {
                page.locator("#fleet .card .ver.behind").count() shouldBe 1
            }
            Then("살아 움직이는 행에만 인터럽트가 선다") {
                page.locator("#fleet .actions md-icon-button.stop").count() shouldBe 2
            }
        }
        When("working 타일을 누르면") {
            page.locator("#summary md-filter-chip.working").click()
            Then("표는 working 하나로 줄고, 다시 누르면 전부로 돌아온다") {
                page.waitForCondition { page.locator("#fleet .card").count() == 1 }
                page.locator("#fleet .card").first().getAttribute("class").contains("working") shouldBe true
                page.locator("#summary md-filter-chip.working").click()
                page.waitForCondition { page.locator("#fleet .card").count() == 5 }
            }
        }
        When("퍼미션의 deny를 누르면") {
            page.locator("#fleet .answer .bgroup md-outlined-button").last().click()
            Then("커맨더 포트로 답이 간다(가짜가 window에 적는다)") {
                page.waitForCondition { page.evaluate("window.__magi_test_last") != null }
                page.evaluate("window.__magi_test_last") shouldBe "answer alpha-1 deny"
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤 없이 행들이 그대로 읽힌다(운영 css의 폰 배치)") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                page.locator("#fleet .card").first().isVisible() shouldBe true
            }
        }
    }
})
