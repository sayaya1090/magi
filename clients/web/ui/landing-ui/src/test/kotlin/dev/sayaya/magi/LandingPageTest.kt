package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/**
 * 랜딩 페이지 관통 — 셸도 회선도 없는 페이지라 잴 것은 둘이다: <b>모양이 다 섰는가</b>와
 * <b>말을 갈면 페이지가 따라가는가</b>.
 *
 * 두 번째가 이 스펙의 값이다. 사전 시험(CopyTest)은 말이 있는지만 세고, 그것이 화면에 닿는지는
 * 세지 못한다 — 그 사이에 판이 하나 있다.
 */
@GwtHtml("landingtest.html")
internal class LandingPageTest : GwtTestSpec({
    Given("랜딩 페이지") {
        page.waitForSelector("#page #hero h1")
        When("페이지가 그려지면") {
            Then("절이 순서대로 서고, 위 띠가 그 절들을 가리킨다") {
                page.locator("#page > #top").count() shouldBe 1
                page.locator("#top .doors a").count() shouldBe 6
                page.locator("main > section").count() shouldBe 8
                page.locator("#hero h1").textContent() shouldBe "magi"
            }
            Then("카운슬은 셋이고, 저마다 제 색의 클래스를 입는다") {
                page.locator("#council .member").count() shouldBe 3
                page.locator("#council .m-melchior h3").textContent() shouldBe "Melchior"
                page.locator("#council .m-balthasar .lens").textContent() shouldBe "verification"
                page.locator("#council .m-casper h3").textContent() shouldBe "Casper"
            }
            Then("집계 규칙 다섯이 표에 서고, 왼쪽 칸은 설정에 적는 낱말 그대로다") {
                page.locator("#council table.tally tbody tr").count() shouldBe 5
                page.locator("#council table.tally tbody tr").first().locator("code").textContent() shouldBe "majority"
                page.locator("#council table.tally tbody tr").last().locator("code").textContent() shouldBe "veto:Name"
            }
            Then("기록 셋·컴패니언 넷·카드 여섯·그림 넷") {
                page.locator("#record .knows li").count() shouldBe 3
                page.locator("#fleet .fleetlist li").count() shouldBe 4
                page.locator("#features .cards .note").count() shouldBe 12
                page.locator("#fleet .shots .shot img").count() shouldBe 4
            }
            Then("자리 여덟이 서고, 사진이 있는 자리에만 사진이 선다") {
                page.locator("#clients .seat").count() shouldBe 8
                // 저장소에 사진이 있는 자리는 넷(터미널·콘솔·젯브레인·파워포인트)이다. 나머지 넷을
                // 비슷한 그림으로 채우지 않는다 — 그래서 이 수는 카드 수보다 작아야 맞다.
                page.locator("#clients .seat img").count() shouldBe 4
            }
            Then("다 된 것과 짓는 중인 것이 낱말로 갈린다 — 목록이 약속으로 읽히지 않게") {
                page.locator("#clients .chip.building").count() shouldBe 1
                page.locator("#clients .chip.shipped").count() shouldBe 7
            }
            Then("데모로 가는 문은 사이트 안의 부속 페이지다 — 저장소가 아니라") {
                page.locator("#hero .acts a").last().getAttribute("href") shouldBe "demo/"
                page.locator("#fleet .shots .shot a").first().getAttribute("href") shouldBe "demo/"
            }
            Then("기계가 낸 글은 그대로 붙여 넣을 수 있게 서 있다") {
                page.locator("#hero .snippet pre").textContent() shouldContain "curl -fsSL https://"
                page.locator("#what .term pre").textContent() shouldContain "council {complete: true}"
            }
        }
        When("말 고르개를 누르면") {
            Then("페이지가 한국어로 다시 서고, 문서의 lang 도 따라간다") {
                page.locator("#hero .tagline").textContent() shouldContain "never drops work"
                page.locator("#tongue").click()
                page.waitForCondition {
                    page.evaluate("document.documentElement.getAttribute('lang')") == "ko"
                }
                page.locator("#hero .tagline").textContent() shouldContain "코딩 에이전트"
                page.locator("#council .m-balthasar .lens").textContent() shouldBe "검증"
            }
            Then("고르개는 이제 돌아갈 말을 가리킨다 — 제 말로") {
                page.locator("#tongue").textContent() shouldBe "English"
            }
            Then("문서 링크가 그 말의 짝을 연다 — 저장소가 EN/KO 를 나란히 두기 때문이다") {
                page.locator("#start .acts a").first().getAttribute("href") shouldContain "MANUAL.ko.md"
            }
            Then("고른 말은 콘솔과 <b>같은 자리</b>에 적힌다 — 데모를 열어도 같은 말이도록") {
                page.evaluate("window.localStorage.getItem('lang')") shouldBe "ko"
            }
            Then("다시 누르면 영어로 돌아온다") {
                page.locator("#tongue").click()
                page.waitForCondition {
                    page.evaluate("document.documentElement.getAttribute('lang')") == "en"
                }
                page.locator("#tongue").textContent() shouldBe "한국어"
            }
        }
    }
})
