package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/**
 * 컴패니언 패널(범용) — 레이아웃은 부모의 것이고, 가운데와 왼쪽은 자식이 채운다.
 * 자식 모듈은 이 하네스에 없으므로, 슬롯이 열려 있다는 것과 부모가 그리는 것(사실판·
 * 오른쪽 판)을 잰다. 자식이 실제로 채우는 것은 coding-agent-ui의 스펙이 잰다.
 */
@GwtHtml("companiontest.html")
internal class CompanionPanelTest : GwtTestSpec({
    Given("컴패니언 하나를 보고 있는 패널") {
        When("상세가 그려지면") {
            Then("위는 사실판, 아래는 무대(왼쪽·가운데·오른쪽)다") {
                page.waitForSelector("#companion #detail:not([hidden])")
                page.locator("#companion #cstage #cleft").count() shouldBe 1
                page.locator("#companion #cstage #cframe").count() shouldBe 1
                page.locator("#companion #cstage #side").count() shouldBe 1
            }
            Then("사실판은 부모의 것 — 상태와 워크스페이스를 요약한다") {
                page.locator("#detail .foldbar .sum").textContent() shouldContain "/Users/you/work/app"
                page.locator("#detail .f[data-k=\"field.steps\"] .v").textContent() shouldBe "7"
            }
            Then("오른쪽 판은 계획을 개수로 말한다 — 막대는 아는 만큼만") {
                page.waitForSelector("#side #plan:not([hidden])")
                page.locator("#side #plan .td").count() shouldBe 3
                page.locator("#side #plan .td.completed").count() shouldBe 1
                page.locator("#side #plan .plancount").textContent() shouldContain "plan.progress"
            }
            Then("넓은 화면엔 탭이 없다 — 나란히 있는 것을 고를 이유가 없다") {
                page.locator("#ptabs[hidden]").count() shouldBe 1
                (page.evaluate("document.body.hasAttribute('panel')") as Boolean) shouldBe false
            }
            Then("자식이 밀면 가운데와 왼쪽이 채워진다 — 부모는 무엇이 오는지 모른다") {
                page.evaluate("window.__magi_pane('centre', f => { f.textContent = 'child centre'; return true; })")
                page.evaluate("window.__magi_pane('left', f => { f.textContent = 'child left'; return true; })")
                page.waitForCondition { page.locator("#cframe").textContent() == "child centre" }
                page.locator("#cleft .cpane").count() shouldBe 1
                page.locator("#cleft .cpane").textContent() shouldBe "child left"
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("탭이 서고 한 번에 하나만 보인다 — 기본은 대화다") {
                page.waitForSelector("#ptabs:not([hidden])")
                page.locator("#ptabs md-primary-tab").count() shouldBe 3
                (page.evaluate("document.body.getAttribute('panel')")) shouldBe "talk"
                page.locator("#cframe:not([hidden])").count() shouldBe 1
                page.locator("#detail[hidden]").count() shouldBe 1
                page.locator("#cleft[hidden]").count() shouldBe 1
            }
            Then("정보 탭은 사실판과 계획을 함께 보인다") {
                page.locator("#ptab-facts").click()
                page.waitForSelector("#detail:not([hidden])")
                page.locator("#side:not([hidden])").count() shouldBe 1
                page.locator("#cframe[hidden]").count() shouldBe 1
            }
            Then("파일 탭은 왼쪽 자리를 보인다") {
                page.locator("#ptab-files").click()
                page.waitForSelector("#cleft:not([hidden])")
                page.locator("#detail[hidden]").count() shouldBe 1
            }
            Then("가로 스크롤은 없다") {
                (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean) shouldBe true
            }
        }
        When("다시 넓어지면") {
            page.setViewportSize(1400, 900)
            Then("탭이 걷히고 전부가 돌아온다") {
                // 창이 자리를 잡을 틈을 준다 — 뷰포트 변경과 리사이즈 처리 사이엔 프레임이 있다.
                page.waitForCondition { page.locator("#ptabs[hidden]").count() == 1 }
                page.waitForCondition { page.locator("#detail:not([hidden])").count() == 1 }
                page.locator("#cframe:not([hidden])").count() shouldBe 1
                page.locator("#cleft:not([hidden])").count() shouldBe 1
            }
        }
    }
})