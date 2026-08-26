package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/**
 * 컴패니언 화면(타입 1 = 코딩 에이전트) 관통 — 가짜 포트의 고정 전사로 그린 DOM을 검사한다.
 * 언어 팩은 테스트 페이지에서 못 읽으므로(키 폴백) 문구가 아니라 구조와 키를 잰다.
 */
@GwtHtml("companiontest.html")
internal class CompanionScreenTest : GwtTestSpec({
    Given("고정 컨텍스트(타입 1)와 다섯 행의 전사") {
        When("화면이 그려지면") {
            Then("사실 줄이 서고 타입 이름이 실린다 — 이 모듈이 곧 타입의 화면이다") {
                page.waitForSelector("#companion .cfacts .cname")
                // 명단이 없는 단독 페이지라 이름은 소켓으로 폴백한다.
                page.locator("#companion .cfacts .cname").textContent() shouldBe "/tmp/a1.sock"
                page.locator("#companion .cfacts .ctype").textContent() shouldBe "type.coding"
            }
            Then("전사 여섯 행 — 목소리와 끝이 클래스에 실린다") {
                page.locator("#log .row").count() shouldBe 6
                page.locator("#log .row.user").count() shouldBe 1
                page.locator("#log .row.tool.toolok").count() shouldBe 1
                page.locator("#log .row.tool.toolfail").count() shouldBe 1
                page.locator("#log .row.pending").count() shouldBe 1
            }
            Then("기계 행들은 접혀 도착한다 — 요약이 결말(마크)과 답 첫 줄을 말한다") {
                page.locator("#log .row.toolok details.fold").count() shouldBe 1
                page.locator("#log .row.toolok summary .mk.ok").count() shouldBe 1
                page.locator("#log .row.toolok summary").textContent() shouldContain "bash"
                page.locator("#log .row.toolok summary").textContent() shouldContain "ok: 12 packages"
                page.locator("#log .row.thinking details.fold").count() shouldBe 1
            }
            Then("실패한 편집은 열려서 도착하고, 속은 경로와 줄마다 클래스가 붙은 디프다") {
                page.locator("#log .row.toolfail details.fold[open]").count() shouldBe 1
                page.locator("#log .row.toolfail .foldbody pre.diff .dadd").count() shouldBe 1
                page.locator("#log .row.toolfail .foldbody pre.diff .ddel").count() shouldBe 1
                page.locator("#log .row.toolfail .foldbody pre.diff .dhunk").count() shouldBe 1
                page.locator("#log .row.toolfail .foldbody").textContent() shouldContain "main.go"
            }
            When("성공한 툴 행을 눌러 펼치면") {
                page.locator("#log .row.toolok summary").click()
                Then("물은 것과 답한 것이 각자 라벨 아래 선다") {
                    page.locator("#log .row.toolok details[open]").count() shouldBe 1
                    page.locator("#log .row.toolok .foldbody .foldk").count() shouldBe 2
                    page.locator("#log .row.toolok .foldbody pre").last().textContent() shouldContain "warnings: 0"
                }
            }
            Then("시각은 행의 홈통에 붙는다") {
                page.locator("#log .row.user .who .when").count() shouldBe 1
            }
            Then("턴이 열려 있으니 진행 바가 돈다 — 소리 내지 않고(aria-hidden)") {
                page.locator("#turnbar.on").count() shouldBe 1
                page.locator("#turnbar[aria-hidden=true]").count() shouldBe 1
            }
        }
        When("컴포저에 한 마디 적어 보내면") {
            page.locator("#companion .composer #say textarea").fill("keep going")
            page.locator("#companion .composer #send").click()
            Then("그 컴패니언(?d=)으로 간다 — 가짜가 창에 적는다") {
                page.waitForCondition { page.evaluate("window.__magi_test_sent") != null }
                page.evaluate("window.__magi_test_sent") shouldBe "keep going@/tmp/a1.sock"
            }
            Then("보낸 뒤 상자는 비어 있다") {
                page.locator("#companion .composer #say textarea").inputValue() shouldBe ""
            }
        }
    }
})
