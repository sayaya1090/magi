package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/** 접근 관통 — 그룹 먼저·읽기 전용, 사람은 역할·범위·삭제, 범례 칩은 필터다. */
@GwtHtml("accesstest.html")
internal class AccessScreenTest : GwtTestSpec({
    Given("그룹 하나와 사람 둘(나=admin, sam=viewer→docs)") {
        When("화면이 그려지면") {
            Then("누구의 콘솔인지와 두 반의 머리, 그리고 그룹이 먼저다") {
                page.waitForSelector("#access .acc")
                page.locator("#access .instance b").textContent() shouldBe "you@devbox"
                page.locator("#access h3.rosterhead").count() shouldBe 3
                page.locator("#access .acclist").first().locator(".acc .who").textContent() shouldBe "@platform"
            }
            Then("능력 낱말은 번역 없이 태그로, 그룹의 범위는 줄에 남는다") {
                page.locator("#access .acc .captag[data-cap=answer]").count() shouldBe 2
                // 팩 없는 페이지의 tr는 치환 없이 키를 돌려준다 — 범위 줄의 존재가 계약이다.
                page.locator("#access .acclist").first().locator(".scope").count() shouldBe 1
            }
            Then("나에겐 당신 표가, 사람에겐 역할 메뉴와 범위 절이 붙는다") {
                page.locator("#access .acc.person.now .you").count() shouldBe 1
                page.locator("#access .acc.person md-outlined-select").count() shouldBe 2
                page.locator("#access .acc.person .scopes .scopechip").count() shouldBe 1
            }
        }
        When("범례의 admin 칩을 누르면") {
            page.locator("#access .caplegend .capchip[data-cap=admin]").click()
            Then("명부가 그 능력으로 좁혀지고, 전부 보기가 곁에 선다") {
                page.waitForSelector("#access .capnote")
                page.locator("#access .acc.person").count() shouldBe 1
                page.locator("#access .acc.person .who").textContent() shouldBe "you@devbox"
            }
            page.locator("#access .capnote md-text-button").click()
            page.waitForCondition { page.locator("#access .acc.person").count() == 2 }
        }
        When("sam의 범위 칩을 지우면(컴포넌트의 remove)") {
            page.locator("#access .acc.person .scopechip").first().evaluate("c => c.dispatchEvent(new Event('remove'))")
            Then("서버에 그대로 말하고, 모든 컴패니언으로 돌아온다") {
                page.waitForCondition { page.evaluate("window.__magi_test_set") != null }
                page.evaluate("window.__magi_test_set") shouldBe "sam@laptop|viewer|"
                page.locator("#access .acc.person .scopes .scopek").last().textContent() shouldBe "access.everywhere"
            }
        }
        When("범위에 이름을 적고 Enter를 치면") {
            page.locator("#access .acc.person .scopebox md-outlined-text-field input, #access .acc.person .scopebox md-outlined-text-field textarea").last().fill("build")
            page.locator("#access .acc.person .scopebox md-outlined-text-field input, #access .acc.person .scopebox md-outlined-text-field textarea").last().press("Enter")
            Then("그 이름으로 좁혀 다시 쓴다") {
                page.waitForCondition { (page.evaluate("window.__magi_test_set") as String).endsWith("build") }
                page.evaluate("window.__magi_test_set") shouldBe "sam@laptop|viewer|build"
            }
        }
        When("삭제를 한 번 누르면") {
            page.locator("#access .acc.person .drop").last().click()
            Then("눈에 보이는 말만이 아니라 읽히는 이름도 무장한다") {
                // 이 버튼에는 사람 이름이 적힌 aria-label이 달려 있다(action.remove_named).
                // 말만 갈면 화면은 "확인?"을 묻는데 읽는 기계에는 여전히 그냥 지우기 버튼이고,
                // 하필 이것이 남의 접근을 걷어내는 버튼이다.
                page.waitForSelector("#access .acc.person .drop.armed")
                // 읽는 기계가 닿는 자리는 호스트가 아니라 그림자 속 <button>이다 — md-*는
                // 호스트의 aria-label을 제 것으로 가져가 거기에 단다(실측).
                page.evaluate("document.querySelector('#access .acc.person .drop.armed')"
                        + ".shadowRoot.querySelector('button').getAttribute('aria-label')")
                        .toString() shouldContain "action.confirm"
            }
        }
        When("한 번 더 눌러 확인하면") {
            page.locator("#access .acc.person .drop.armed").click()
            Then("사람이 명부에서 빠진다") {
                page.waitForCondition { page.evaluate("window.__magi_test_removed_person") != null }
                page.evaluate("window.__magi_test_removed_person") shouldBe "sam@laptop"
                page.waitForCondition { page.locator("#access .acc.person").count() == 1 }
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤 없이 명부가 그대로 읽힌다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                page.locator("#access .acc").first().isVisible() shouldBe true
            }
        }
    }
})
