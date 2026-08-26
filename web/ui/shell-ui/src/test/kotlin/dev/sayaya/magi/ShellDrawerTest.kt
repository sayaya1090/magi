package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe

/**
 * 드로어의 계약: 마크업(레일·버거·스크림), 두 속성의 상태 기계(nav=폭, nav-wide=모양,
 * 닫힘은 250ms 뒤 모양), 선택 표시, 목적지 모듈 로드(가짜 로더가 window에 적는다).
 */
@GwtHtml("shelltest.html")
internal class ShellDrawerTest : GwtTestSpec({
    Given("셸이 선 화면") {
        When("첫 그리기가 끝나면") {
            Then("레일과 버거와 스크림이 서고, 문은 이식된 화면 수만큼만 있다") {
                page.waitForSelector("#rail")
                page.locator("#rail #railMenu").count() shouldBe 1
                page.locator("#scrim").count() shouldBe 1
                page.locator("#railNav .raili").count() shouldBe 1
            }
            Then("주소의 목적지(fleet)가 선택돼 있고, 그 모듈이 정확히 한 번 로드된다") {
                page.locator("#railNav .raili[selected]").count() shouldBe 1
                page.locator("#railNav .raili[aria-current=page]").count() shouldBe 1
                page.evaluate("window.__magi_test_loads") shouldBe "[fleet]"
            }
        }
        When("버거를 누르면") {
            page.locator("#railMenu").click()
            Then("드로어가 열리고(nav=open) 2단 패널이 선다") {
                page.waitForSelector("body[nav=open]")
                page.waitForSelector("#railPanel .railpanel-head")
                // 언어 팩이 없는 테스트 페이지라 키가 폴백이다 — 구조가 계약이고 문구는 팩의 몫.
                page.locator("#railPanel .railpanel-head").textContent() shouldBe "nav.companions"
            }
            Then("컴패니언 문의 속은 명단이다 — 기다리는 것이 먼저") {
                page.locator("#railPanel .subitem").count() shouldBe 2
                page.locator("#railPanel .subitem").first().getAttribute("class").contains("waiting") shouldBe true
                page.locator("#railPanel .subitem .sword").count() shouldBe 2
            }
        }
        When("스크림을 누르면") {
            page.locator("#scrim").click()
            Then("드로어가 닫히고 2단도 함께 걷힌다") {
                page.waitForCondition { page.locator("body[nav=open]").count() == 0 }
                page.locator("#railPanel").isVisible() shouldBe false
            }
        }
        When("명단(가짜: 둘, 하나는 기다림)이 마스트헤드에 닿으면") {
            Then("바가 서고, 수와 기다림 점프가 한 줄에 선다") {
                page.waitForSelector("#masthead #state .scount")
                page.locator("#masthead .mark").textContent() shouldBe "MAGI"
                page.locator("#state .jump").count() shouldBe 1
                page.locator("#state.asking").count() shouldBe 1
                page.locator("#state.live").count() shouldBe 1
            }
            Then("컴패니언 문의 배지가 기다림 수를 단다") {
                page.locator("#railBadge:not([hidden])").count() shouldBe 1
                page.evaluate("document.getElementById('railBadge').value") shouldBe "1"
            }
        }
        When("선 목적지를 다시 눌러도") {
            page.locator("#railNav .raili").first().click()
            Then("로드는 여전히 한 번이다") {
                page.evaluate("window.__magi_test_loads") shouldBe "[fleet]"
            }
        }
    }
})
