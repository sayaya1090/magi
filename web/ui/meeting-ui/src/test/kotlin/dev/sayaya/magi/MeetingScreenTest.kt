package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/**
 * 회의실 — 여는 화면과 방. 언어 팩이 없는 페이지라 문구가 아니라 구조와 키를 잰다.
 * 방의 단계는 가짜가 창에 연 문(window.__magi_test_room)으로 바꾼다: 회의는 남이 말해서
 * 바뀌는 화면이라, 그 바뀜을 만들 수 있어야 단계별 화면을 잰다.
 */
@GwtHtml("meetingtest.html")
internal class MeetingScreenTest : GwtTestSpec({
    Given("회의실") {
        When("여는 화면이 그려지면") {
            Then("무엇을 물을지와 누구에게가 서고, 부를 수 없는 이는 없다") {
                page.waitForSelector("#meet .meetbox .meettopicfield")
                page.locator("#meet .meetwho md-filter-chip").count() shouldBe 2
                // 남의 기계의 컴패니언(elsewhere)은 이 콘솔이 걸어 본 적 없는 행이다.
                page.locator("#meet .meetwho md-filter-chip[label=gamma]").count() shouldBe 0
                // 팀은 색으로 갈린다(tm0/tm1) — 색이 유일한 말이 아니라 팀 이름은 툴팁에도 있다.
                page.locator("#meet .meetwho md-filter-chip.tm0").count() shouldBe 1
                (page.locator("#meet .meetwho md-filter-chip").first().getAttribute("data-tip"))
                    .shouldContain("meet.of_team")
            }
            Then("아직 열 수 없다 — 무엇이 빠졌는지 말한다") {
                page.locator("#meet .meetgo[disabled]").count() shouldBe 1
                page.locator("#meet .meetnote").textContent() shouldBe "meet.need_two"
            }
            Then("지금 도는 방과 끝난 방이 따로 선다") {
                page.locator("#meet .meetlist .meetrow").count() shouldBe 2
                page.locator("#meet .meetlist .meetrow .meettitle").first().textContent()
                    .shouldContain("which store")
                // 한 줄이 그 방의 단계를 말한다: 도는 방은 몇 바퀴째인지, 끝난 방은 어떻게 끝났는지.
                page.locator("#meet .meetlist .meetrow .meetmeta").first().textContent()
                    .shouldContain("meet.round")
                page.locator("#meet .meetlist .meetrow .meetmeta").last().textContent()
                    .shouldContain("meet.done_spent")
            }
        }
        When("주제를 적고 둘을 고르면") {
            page.locator("#meet .meettopicfield textarea").fill("which store for the queue?")
            page.locator("#meet .meetwho md-filter-chip").nth(0).click()
            page.locator("#meet .meetwho md-filter-chip").nth(1).click()
            Then("열 수 있게 되고, 빠진 것을 말하던 줄은 조용해진다") {
                page.waitForCondition { page.locator("#meet .meetgo[disabled]").count() == 0 }
                page.locator("#meet .meetnote").textContent() shouldBe ""
            }
            Then("열면 주제와 고른 이들이 그대로 간다") {
                page.locator("#meet .meetgo").click()
                page.waitForCondition { page.evaluate("window.__magi_test_convened") != null }
                page.evaluate("window.__magi_test_convened") shouldBe
                    "which store for the queue?|/tmp/a1.sock,/tmp/b1.sock"
            }
        }
        When("방으로 들어가면(주소의 ?m=)") {
            page.evaluate("history.replaceState(null,'','?v=meet&m=m1'); window.dispatchEvent(new PopStateEvent('popstate'))")
            Then("주제와 명단이 붙박이로 서고, 오간 말이 그 아래 흐른다") {
                page.waitForSelector("#meet .meethead .meettopic")
                page.locator("#meet .meethead .meettopic").textContent() shouldContain "which store"
                page.locator("#meet .meetroster md-filter-chip").count() shouldBe 3
                // 사람도 명단에 있다 — 색이 아니라 자리로 갈린다.
                page.locator("#meet .meetroster md-filter-chip.person").count() shouldBe 1
                page.locator("#meet .meetsaid .meetline").count() shouldBe 2
                page.locator("#meet .meetsaid .meetlap").count() shouldBe 1
            }
            Then("넘긴 차례는 넘겼다고 적는다 — 빈 줄로 두지 않는다") {
                page.locator("#meet .meetsaid .meetline.passed").count() shouldBe 1
                page.locator("#meet .meetsaid .meetline.passed .meettext").textContent()
                    .shouldContain("meet.passed_why")
            }
            Then("말한 이마다 색자리가 다르다") {
                page.locator("#meet .meetsaid .meetline.sp0").count() shouldBe 1
                page.locator("#meet .meetsaid .meetline.sp1").count() shouldBe 1
            }
            Then("그 한 마디를 하는 동안 무엇을 했는지는 눌러야 열린다") {
                page.locator("#meet .meetsaid .meetline").first().locator(".meetworkrows[hidden]").count() shouldBe 1
                page.locator("#meet .meetsaid .meetline").first().locator(".meetworkgo").click()
                page.waitForSelector("#meet .meetsaid .meetline .meetworkrows:not([hidden])")
                // 그 차례의 도구질만 — 결론(assistant)은 이미 회의 줄에 있다.
                page.locator("#meet .meetworkrows .row").count() shouldBe 2
                page.locator("#meet .meetworkrows .row.assistant").count() shouldBe 0
            }
        }
        When("한 마디를 쓰면") {
            page.locator("#meet .meetsay #meetSay textarea").fill("sqlite is enough")
            Then("쓰는 동안 바닥을 쥔다 — 그래야 그 사이 아무도 끼어들지 않는다") {
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_said") as String?)?.endsWith("|true") == true
                }
                page.evaluate("window.__magi_test_said") shouldBe "m1|||true"
            }
            Then("보내면 그 방으로 간다") {
                page.locator("#meet .meetsay md-filled-button").click()
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_said") as String).contains("sqlite")
                }
                page.evaluate("window.__magi_test_said") shouldBe "m1|sqlite is enough||false"
            }
        }
        When("명단의 한 사람을 누르면") {
            page.locator("#meet .meetroster md-filter-chip").first().click()
            Then("그 사람을 지명한다 — 고른 것은 하나뿐이다") {
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_said") as String).contains("|alpha|")
                }
                page.evaluate("window.__magi_test_said") shouldBe "m1||alpha|false"
            }
        }
        When("마무리하면") {
            page.locator("#meet .meetsay md-text-button").last().click()
            Then("결론이 서고, 누구에게 무엇이 남았는지 말한다") {
                page.waitForSelector("#meet .meettasks .meettask")
                page.locator("#meet .meettasks .meettask").count() shouldBe 2
                // 남은 것이 없는 사람도 적는다 — 빠뜨린 것과 없는 것은 다르다.
                page.locator("#meet .meettasks .meettask.nothing").count() shouldBe 1
                page.locator("#meet .meettasks .meettask.nothing .meettaskwhat").textContent()
                    .shouldBe("meet.task_none")
            }
            Then("건네면 건넸다고 말하고, 그리로 가는 길도 준다") {
                page.locator("#meet .meettasks .meettask md-text-button").first().click()
                page.waitForCondition { page.evaluate("window.__magi_test_handed") != null }
                page.evaluate("window.__magi_test_handed") shouldBe "m1|alpha"
                page.waitForSelector("#meet .meettasks .meetsent .sentgo")
                (page.locator("#meet .meettasks .meetsent .sentgo").getAttribute("href"))
                    .shouldContain("/tmp/a1.sock")
            }
            Then("끝난 방은 다시 열 수 있다 — 무엇이 남았는지 적어서") {
                page.locator("#meet .meetsay md-outlined-text-field textarea").last().fill("the ops half is unanswered")
                page.locator("#meet .meetsay md-filled-tonal-button").click()
                page.waitForCondition { page.evaluate("window.__magi_test_reopened") != null }
                page.evaluate("window.__magi_test_reopened") shouldBe "m1|the ops half is unanswered"
            }
            Then("끝난 방에서는 명단을 눌러 지명할 수 없다") {
                page.locator("#meet .meetroster md-filter-chip[disabled]").count() shouldBe 3
            }
        }
        When("없는 방을 대면") {
            page.evaluate("history.replaceState(null,'','?v=meet&m=gone'); window.dispatchEvent(new PopStateEvent('popstate'))")
            Then("사라졌다고 말하고, 무엇을 할 수 있는지도 말한다") {
                page.waitForSelector("#meet .empty")
                page.locator("#meet .empty .emptywhat").textContent() shouldBe "meet.gone"
                page.locator("#meet .empty .emptyhow").textContent() shouldBe "meet.gone_how"
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤이 없다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
            }
        }
    }
})
