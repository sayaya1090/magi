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
            Then("사실판(#detail)이 서고, 접는 바의 요약이 상태와 워크스페이스를 말한다") {
                page.waitForSelector("#companion #detail:not([hidden])")
                page.locator("#detail .foldbar .sum").textContent() shouldContain "/Users/you/work/app"
            }
            Then("질문이 오는 순서의 필드들 — 상태·짐, 스텝, 역할, 호스트, 세션, 결재") {
                page.locator("#detail .f[data-k=\"field.status\"] .v").textContent() shouldContain "load.in_hand"
                page.locator("#detail .f[data-k=\"field.steps\"] .v").textContent() shouldBe "7"
                page.locator("#detail .f[data-k=\"field.role\"] .v").textContent() shouldContain "build green"
                page.locator("#detail .f[data-k=\"field.host\"] .v").textContent() shouldContain "pid 4242"
                page.locator("#detail .f[data-k=\"field.session\"] .v").textContent() shouldBe "s_demo1"
                page.locator("#detail .f[data-k=\"field.permission\"] .v").textContent() shouldBe "perm.ask"
            }
            Then("컨텍스트 줄 — 잰 것과 셈한 것을 섞지 않고, 창을 아는 때만 바가 선다") {
                page.locator("#detail .f[data-k=\"field.context\"] .v").textContent() shouldContain "82,000 / 100,000 tokens"
                page.locator("#detail .f[data-k=\"field.context\"] small").textContent() shouldContain "context.measured"
                page.locator("#detail .f[data-k=\"field.context\"] .bar i").count() shouldBe 1
                page.locator("#detail .f[data-k=\"field.summarised_away\"] .v").textContent() shouldContain "context.folds"
            }
            Then("접는 바를 누르면 접히고, 그 선호가 기억된다") {
                page.locator("#detail .foldbar").click()
                page.waitForSelector("#detail[folded]")
                page.evaluate("localStorage.getItem('facts')") shouldBe "folded"
                page.locator("#detail .foldbar").click()
                page.waitForCondition { page.locator("#detail[folded]").count() == 0 }
            }
            Then("지금 접기를 누르면 그 컴패니언으로 간다") {
                page.locator("#detail .f[data-k=\"field.context\"] md-text-button.fold").click()
                page.waitForCondition { page.evaluate("window.__magi_test_compacted") != null }
                page.evaluate("window.__magi_test_compacted") shouldBe "/tmp/a1.sock"
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
            Then("턴이 열려 있으니 운영의 그 턴바가 돈다 — 바는 조용하고(aria-hidden) 숫자가 말한다") {
                page.waitForSelector("#turnwrap:not([hidden])")
                // aria-hidden은 md 컴포넌트가 섀도로 위임하며 호스트 속성을 걷어간다(실측:
                // 속성 셀렉터는 영원히 0) — 드로어 때와 같은 함정이라 존재만 잰다.
                page.waitForSelector("#turnwrap md-linear-progress#turnbar")
                // 12초에서 시작해 화면의 시계로 흐른다 — 초는 재지 말고 모양(s)만 잰다.
                Regex("\\d+s").matches(page.locator("#turnfor").textContent() ?: "") shouldBe true
            }
        }
        When("컴포저에 한 마디 적어 보내면") {
            page.locator("#companion .composer #t textarea").fill("keep going")
            page.locator("#companion .composer #send").click()
            Then("그 컴패니언(?d=)으로 간다 — 가짜가 창에 적는다") {
                page.waitForCondition { page.evaluate("window.__magi_test_sent") != null }
                page.evaluate("window.__magi_test_sent") shouldBe "keep going@/tmp/a1.sock"
            }
            Then("보낸 뒤 상자는 비어 있다") {
                page.locator("#companion .composer #t textarea").inputValue() shouldBe ""
            }
        }
        When("지난 일 층위(빈 past=목록)로 갈아타면") {
            page.evaluate("window.__magi_test_past('')")
            Then("지금-대화의 판들이 물러나고 목록이 선다 — 여는 길은 행이다") {
                page.waitForSelector("#agentdetail:not([hidden]) .hs")
                page.locator("#log[hidden]").count() shouldBe 1
                page.locator("#companion form[hidden]").count() shouldBe 1
                page.locator("#agentdetail .hs").count() shouldBe 2
                page.locator("#agentdetail .hs.now .when").textContent() shouldBe "state.working"
                page.locator("#agentdetail .hs").last().locator(".what").textContent() shouldContain "retry storm"
            }
        }
        When("한 세션(past=id)으로 들어가면") {
            page.evaluate("window.__magi_test_past('s_old')")
            Then("그 세션의 전사가 fetch로 선다 — 스트림이 아니다") {
                page.waitForSelector("#agentdetail .dlog .row")
                page.evaluate("window.__magi_test_past_read") shouldBe "s_old"
                page.locator("#agentdetail .dlog .row").count() shouldBe 2
                page.locator("#agentdetail .dlog .row.user .txt").textContent() shouldBe "old prompt"
            }
            Then("머리의 돌아가는 길이 목록을 가리킨다") {
                page.locator("#agentdetail .sectionhead .backpast").count() shouldBe 1
            }
        }
        When("지금 대화로 돌아오면") {
            page.evaluate("window.__magi_test_past(null)")
            Then("전사와 컴포저가 돌아온다") {
                page.waitForSelector("#log:not([hidden])")
                page.locator("#agentdetail[hidden]").count() shouldBe 1
                page.locator("#companion form:not([hidden])").count() shouldBe 1
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤이 없고, 전사와 컴포저가 그대로 쓸 만하다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                page.locator("#log .row").first().isVisible() shouldBe true
                page.locator("#companion .composer #t").isVisible() shouldBe true
            }
        }
    }
})
