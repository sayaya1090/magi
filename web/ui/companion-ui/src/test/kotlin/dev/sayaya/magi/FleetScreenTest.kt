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
            Then("칩들 곁에 나가는 길들이 선다(.toview) — 보드·맵, 그리고 이 기계에 둘 이상이라 회의") {
                // 회의가 여기에도 있는 이유: 레일은 600px 아래에서 그려지지 않고, 부를 사람들이
                // 이 목록에 있다 — 그 일은 고를 목록 곁에 서는 것이 맞다(운영의 그 자리).
                page.locator("#summary .toview").count() shouldBe 3
                // 이름은 md 컴포넌트가 섀도로 위임하며 호스트에서 걷어간다(여러 번 밟은 함정) —
                // 그 자리의 그림으로 어느 길인지 잰다.
                page.locator("#summary .toview svg[data-i='#i-sl-comments']").count() shouldBe 1
                // 그래서 이 길들은 말을 들고 있어야 한다: 이름이 호스트에서 걷힌 그림 버튼이
                // 아무 말도 없으면, 눌러 보는 것 말고 알아낼 방법이 없다(운영도 여기에 붙인다).
                page.locator("#summary .toview[data-tip]").count() shouldBe 3
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
        When("멈추라는 명령이 거절당하면") {
            // 이 화면의 열 번째 「눌렀는데 아무 말도 없던 자리」: 거절당해도 명단만 다시 서고
            // 그 컴패니언은 여전히 working이라, 「멈추는 중」과 화면이 구별되지 않았다.
            page.evaluate("window.__magi_test_press_refuses = 'no turn is open'")
            page.locator("#fleet .card.working .actions md-icon-button.stop").click()
            page.waitForSelector("md-dialog.askconfirm")
            page.locator("md-dialog.askconfirm md-text-button.armed").last().click()
            Then("사유는 누른 카드에 선다 — 카드는 눌리기 전 그대로 working이다") {
                page.waitForCondition { page.locator("#fleet .card.working .refused").count() == 1 }
                page.locator("#fleet .card.working .refused").textContent() shouldBe "no turn is open"
                page.locator("#fleet .card.working .refused").getAttribute("role") shouldBe "alert"
                // 사유는 <b>그 카드의 것</b>이다: 화면 어디에도 두 줄이 서지 않는다.
                page.locator("#fleet .refused").count() shouldBe 1
                page.locator("#fleet .card.working").count() shouldBe 1
            }
        }
        When("다시 눌러 이번엔 서면") {
            page.evaluate("delete window.__magi_test_press_refuses")
            page.locator("#fleet .card.working .actions md-icon-button.stop").click()
            page.waitForSelector("md-dialog.askconfirm")
            page.locator("md-dialog.askconfirm md-text-button.armed").last().click()
            Then("낡은 사유는 걷힌다 — 서고 나면 그 자리는 다시 조용해야 한다") {
                page.waitForCondition { page.locator("#fleet .refused").count() == 0 }
            }
        }
        When("답이 거부당하면") {
            page.evaluate("window.__magi_test_refuse = 'that call was already answered'")
            page.locator("#fleet .card.waiting .answer .bgroup md-outlined-button").first().click()
            Then("사유는 답한 카드에 선다 — 멈춤이 쓰는 그 한 줄이다") {
                page.waitForCondition { page.locator("#fleet .card.waiting .refused").count() == 1 }
                page.locator("#fleet .card.waiting .refused").textContent() shouldBe
                    "that call was already answered"
                // 답 상자는 그대로다: 답이 서지 못했으니 물음도 그대로다.
                page.locator("#fleet .card.waiting .answer").count() shouldBe 1
                page.evaluate("delete window.__magi_test_refuse")
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
