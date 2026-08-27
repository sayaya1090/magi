package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/** 보드 관통 — 두 팀의 오늘, 걸친 밤일, 라벨 찾기, 날짜 걸음. */
@GwtHtml("boardtest.html")
internal class BoardScreenTest : GwtTestSpec({
    Given("core 팀 둘(하나는 지금 도는 중)과 무팀 docs") {
        When("화면이 그려지면") {
            Then("머리가 서고 오늘이라 앞으로·오늘은 잠긴다") {
                page.waitForSelector("#board .boardhead")
                page.locator(".boardhead md-icon-button").count() shouldBe 2
                page.locator(".boardhead md-icon-button[disabled]").count() shouldBe 1
                page.locator(".boardhead md-text-button[disabled]").count() shouldBe 1
            }
            Then("레인은 팀이다 — core 하나(무팀 docs는 일이 없어 안 선다), 수를 단다") {
                page.waitForSelector(".lanes .lane")
                page.locator(".lanes .lane").count() shouldBe 1
                page.locator(".lane .lanehead .lname").textContent() shouldBe "core"
                page.locator(".lane .lanehead .lcount").textContent() shouldBe "3"
            }
            Then("지금 도는 카드는 now를 달고, 끝난 카드는 소요와 모델을 단다") {
                page.locator(".lane .wcard.now .wwhen").textContent() shouldContain "board.now"
                page.locator(".lane .wcard .wlong").first().textContent() shouldBe "2m"
                page.locator(".lane .wcard .wmodel").first().textContent() shouldBe "gpt-oss:120b"
            }
            Then("여럿의 레인이라 카드가 누가를 단다") {
                page.locator(".lane .wcard .wwho").count() shouldBe 3
            }
        }
        When("라벨 칩을 누르면") {
            page.locator(".wlabel").first().click()
            Then("그 말로 좁혀진다 — 라벨 단 카드만") {
                page.waitForCondition { page.locator(".lane .wcard").count() == 1 }
                page.locator(".lane .wcard .wwhat").textContent() shouldContain "retry storm"
            }
            page.locator(".boardhead md-outlined-text-field:not([type=date]) input, .boardhead md-outlined-text-field:not([type=date]) textarea").first().fill("")
            page.waitForCondition { page.locator(".lane .wcard").count() == 3 }
        }
        When("하루 뒤로 걸으면") {
            page.locator(".boardhead md-icon-button").first().click()
            Then("걸친 밤일만 남고, 앞으로·오늘이 풀린다") {
                page.waitForCondition { page.locator(".lane .wcard").count() == 1 }
                page.locator(".lane .wcard .wwhat").textContent() shouldContain "overnight"
                page.locator(".boardhead md-icon-button[disabled]").count() shouldBe 0
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤 없이 레인 스트립이 스냅으로 선다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                page.locator(".lanes .lane").first().isVisible() shouldBe true
            }
        }
    }
})
