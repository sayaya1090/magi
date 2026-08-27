package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.ints.shouldBeGreaterThanOrEqual
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/** 맵 관통 — 두 경계의 상자, 팀 머리, 노드 어휘, 그리고 측정된 와이어. */
@GwtHtml("maptest.html")
internal class MapScreenTest : GwtTestSpec({
    Given("여기(you@mac, core 둘)와 침묵한 buildbox(들인 것), 오간 것 둘") {
        When("화면이 그려지면") {
            Then("머신 상자 둘 — 내 것이 먼저, 침묵은 상자 위의 나쁜 소식으로") {
                page.waitForSelector("#map .machine")
                page.locator("#map .machine").count() shouldBe 2
                page.locator("#map .machine .machinename").first().textContent() shouldBe "mac"
                page.locator("#map .machine .placeseen.down").count() shouldBe 1
            }
            Then("계정 상자가 신뢰의 말을 달고, 팀은 머리로 선다") {
                page.locator("#map .place.own .placetrust").textContent() shouldBe "map.trust_own"
                page.locator("#map .teamlabel").first().textContent() shouldBe "core"
            }
            Then("노드는 같은 다섯 상태의 같은 말 — 딴 머신 것은 링크가 아니다") {
                page.locator("#map .node").count() shouldBe 3
                page.locator("#map a.node").count() shouldBe 2
                page.locator("#map div.node.faroff").count() shouldBe 1
                page.locator("#map .node .nodehub").count() shouldBe 1
            }
            Then("와이어 둘 — 도는 중 하나, 닿을 수 없음 하나. 범례가 셋을 말한다") {
                page.waitForCondition { page.locator("#map .wires path").count() >= 2 }
                page.locator("#map .wires path.flight").count() shouldBe 1
                page.locator("#map .wires path.down").count() shouldBe 1
                page.locator("#map .maplegend .mapkey").count() shouldBe 3
            }
            Then("표로 돌아가는 길이 머리에 산다") {
                page.locator("#map .sectionhead .astable").count() shouldBe 1
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤이 없고 상자들이 그대로 읽힌다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                page.locator("#map .machine").first().isVisible() shouldBe true
            }
        }
    }
})
