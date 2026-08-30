package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/**
 * 데모에서 <b>행동이 자기를 보고하는가</b>.
 *
 * 띠의 첫 줄은 "every action reports what it would have sent"라고 약속한다. 화면이 없는
 * 페이지라 그 약속만 잰다: 목을 회선의 이음매(window.__magi_demo_mock)로 직접 부르고, 창의
 * 띠에 무엇이 적혔는지 본다. 띠는 페이지가 세우는 것이므로 여기서는 손으로 하나 세운다 —
 * 클래스 이름이 계약의 전부다.
 */
@GwtHtml("demotest.html")
internal class DemoBannerTest : GwtTestSpec({
    Given("데모의 목") {
        page.waitForCondition { page.evaluate("!!window.__magi_demo_mock") == true }
        page.evaluate(
            "document.body.insertAdjacentHTML('afterbegin','<div class=demo-banner></div>');" +
            "window.__t = (p,b) => window.__magi_demo_mock(p, b ? {body: new URLSearchParams(b)} : {});" +
            "window.__band = () => document.querySelector('.demo-banner').textContent"
        )
        When("아무도 이름 대지 않은 쓰기가 오면") {
            Then("무엇이 데몬으로 갔을 것인지 띠에 적힌다 — 보낸 몸까지") {
                page.evaluate("window.__t('/forget', {name:'skill-tests-before-done'})")
                page.waitForCondition { page.evaluate("window.__band()") != "" }
                (page.evaluate("window.__band()") as String)
                    .shouldContain("would have sent: POST /forget name=skill-tests-before-done")
            }
            Then("답은 그대로 빈 몸이다 — 화면은 늘 하던 대로 다음을 묻는다") {
                page.evaluate("window.__t('/forget', {name:'x'}).then(r => r.text()).then(t => window.__body = t)")
                page.waitForCondition { page.evaluate("window.__body") != null }
                page.evaluate("window.__body") shouldBe ""
            }
        }
        When("회의의 행동이 오면") {
            Then("POST 경로가 아니라 그것이 무슨 짓인지 적힌다") {
                page.evaluate("window.__t('/meet-close', {id:'m1'})")
                page.waitForCondition { (page.evaluate("window.__band()") as String).contains("discussion") }
                page.evaluate("window.__band()") shouldBe
                    "demo — would have ended the discussion and asked each of them what they will do"
            }
            Then("넷이 서로 다른 말을 한다 — 같은 짓이 아니다") {
                page.evaluate("window.__t('/meet-hand', {id:'m1',who:'/demo/api.sock'})")
                page.waitForCondition { (page.evaluate("window.__band()") as String).contains("conclusion") }
                (page.evaluate("window.__band()") as String).shouldContain("as work, in its own session")
            }
        }
        When("읽기가 오면") {
            Then("띠는 그대로다 — 아무도 아무것도 보내지 않았다") {
                page.evaluate("window.__mark = window.__band(); window.__t('/fleet')")
                page.evaluate("window.__band()") shouldBe page.evaluate("window.__mark")
            }
        }
        When("글자를 칠 때마다 오는 길이면") {
            Then("띠는 그대로다 — 방문자가 친 것이 띠에 되비치지 않는다") {
                page.evaluate("window.__mark = window.__band();" +
                        "window.__t('/complete', {text:'func main() {'});" +
                        "window.__t('/suggest', {prefix:'run the tests'})")
                page.evaluate("window.__band()") shouldBe page.evaluate("window.__mark")
            }
        }
    }
})
