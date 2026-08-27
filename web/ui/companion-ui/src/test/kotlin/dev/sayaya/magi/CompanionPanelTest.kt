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
            Then("뼈대는 운영 콘솔의 그 이름들이다 — 세 기둥과 그 안의 사실판") {
                // 이름이 계약인 이유: 창 높이 앵커·기둥 접기·도크 여백이 전부 console.css에
                // 이 이름들로 적혀 있다. 새 이름을 쓰면 그 규칙이 통째로 비켜간다(실측: 1024px
                // 창에서 대화가 224px, 전사는 4천 픽셀로 자라 잘림).
                page.waitForSelector("#agentview #stream #detail:not([hidden])")
                page.locator("#agentview #filecol").count() shouldBe 1
                page.locator("#agentview #sidecol #side").count() shouldBe 1
                // 사실판은 가운데 기둥 안에 선다 — 무대 위가 아니라(운영의 그 자리).
                page.locator("#stream > #detail").count() shouldBe 1
            }
            Then("기둥 둘은 닫힌 채로 온다 — 처음 온 사람에게 이 화면은 대화다") {
                page.waitForSelector("body[files=shut][side=shut]")
                // 닫힘은 폭 0이다: 대화가 창을 다 갖는다(운영 규칙 그대로).
                (page.evaluate("getComputedStyle(document.getElementById('agentview'))" +
                    ".gridTemplateColumns.split(' ')[0]")) shouldBe "0px"
            }
            Then("도크가 서고, 비어 있는 동안은 바닥을 한 뼘도 먹지 않는다") {
                val where = page.locator("#dock").count().toString() + "/" +
                    page.locator("#dock .bay").count() + "/" + page.locator("#agentview").count()
                io.kotest.assertions.withClue("dock/bay/agentview = $where") {
                    (page.locator("#dock .bay").count() > 0) shouldBe true
                }
                // 실측이 계약이다: page.css의 본문 바닥 여백이 var(--dock, 아주 큰 기본값)이라
                // 재지 않으면 160px이 깔린다(운영은 32px — 이 셸에서 실측으로 잡힌 그 차이).
                // 자식이 아직 아무것도 밀지 않았으면 도크는 빈 상자이고, 그 값은 0이다.
                page.waitForCondition {
                    (page.evaluate("getComputedStyle(document.documentElement)" +
                        ".getPropertyValue('--dock').trim()")) == "0px"
                }
            }
            Then("사실판은 부모의 것 — 상태와 워크스페이스를 요약한다") {
                page.locator("#detail .foldbar .sum").textContent() shouldContain "/Users/you/work/app"
                page.locator("#detail .f[data-k=\"field.steps\"] .v").textContent() shouldBe "7"
            }
            Then("손잡이를 누르면 그 기둥이 열린다 — 닫힌 기둥의 속은 보이지 않는 것이 옳다") {
                // 손잡이는 마스트헤드에 선다(셸이 내준 자리) — 여는 것은 이 화면의 기둥이지만
                // 손잡이 자체는 이 창을 어떻게 배치할지에 대한 것이라서.
                page.locator("#masthead #chrome #sideToggle").count() shouldBe 1
                page.locator("#masthead #chrome #filesToggle").count() shouldBe 1
                // 닫힌 동안 오른쪽 기둥은 폭이 0이고, 그 속의 계획은 화면에 없다.
                page.locator("#side #plan").isVisible() shouldBe false
                page.locator("#sideToggle").click()
                page.waitForSelector("body[side=open]")
                page.waitForCondition {
                    (page.evaluate("document.getElementById('sidecol').getBoundingClientRect().width")
                        as Number).toInt() > 100
                }
            }
            Then("오른쪽 판은 계획을 개수로 말한다 — 막대는 아는 만큼만") {
                page.waitForSelector("#side #plan:not([hidden])")
                page.locator("#side #plan .td").count() shouldBe 3
                page.locator("#side #plan .td.completed").count() shouldBe 1
                page.locator("#side #plan .plancount").textContent() shouldContain "plan.progress"
            }
            Then("손잡이의 상태는 기억된다 — 다시 닫으면 닫힌 채로 남는다") {
                page.locator("#sideToggle").click()
                page.waitForSelector("body[side=shut]")
                (page.evaluate("window.localStorage.getItem('magi.side')")) shouldBe "shut"
                // 손잡이 자신도 같은 사실을 말한다. 상태는 md-* 컴포넌트의 프로퍼티로 잰다:
                // aria-*는 이 컴포넌트가 제 그림자 안쪽으로 옮겨 호스트에서는 사라진다(실측).
                (page.evaluate("document.getElementById('sideToggle').selected")) shouldBe false
                (page.evaluate("document.getElementById('sideToggle').title")) shouldBe "side.show"
            }
            Then("넓은 화면엔 탭이 없다 — 나란히 있는 것을 고를 이유가 없다") {
                page.locator("#ptabs[hidden]").count() shouldBe 1
                (page.evaluate("document.body.hasAttribute('panel')") as Boolean) shouldBe false
            }
            Then("자식이 밀면 세 자리가 채워진다 — 부모는 무엇이 오는지 모른다") {
                page.evaluate("window.__magi_pane('centre', f => { f.textContent = 'child centre'; return true; })")
                page.evaluate("window.__magi_pane('left', f => { f.textContent = 'child left'; return true; })")
                page.evaluate("window.__magi_pane('dock', f => { f.textContent = 'child dock'; return true; })")
                page.waitForCondition { page.locator("#stream .cfill").textContent() == "child centre" }
                page.locator("#filecol .cfill").textContent() shouldBe "child left"
                // 도크는 창 바닥의 고정 상자다 — 자식은 그것을 모른 채 그 자리를 채운다.
                page.locator("#dock .bay .cfill").textContent() shouldBe "child dock"
                // 자식이 도크를 채우자 본문의 바닥이 그만큼 물러난다 — 자식은 그런 일이
                // 일어난 줄도 모른다(그 상자가 창 바닥에 고정돼 있다는 것이 부모의 사실이라서).
                page.waitForCondition {
                    (page.evaluate("parseFloat(getComputedStyle(document.documentElement)" +
                        ".getPropertyValue('--dock')) || 0") as Number).toInt() > 0
                }
            }
            Then("자식이 채우는 껍데기는 배치에서 없는 셈이다 — 높이 사슬이 끊기지 않게") {
                (page.evaluate("getComputedStyle(document.querySelector('#stream .cfill')).display"))
                    .shouldBe("contents")
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("탭 넷이 서고 한 번에 하나만 보인다 — 기본은 대화다(운영의 그 넷)") {
                page.waitForSelector("#ptabs:not([hidden])")
                page.locator("#ptabs md-primary-tab").count() shouldBe 4
                (page.evaluate("document.body.getAttribute('panel')")) shouldBe "talk"
                page.locator("#stream .cfill:not([hidden])").count() shouldBe 1
                page.locator("#detail[hidden]").count() shouldBe 1
                page.locator("#filecol[hidden]").count() shouldBe 1
            }
            Then("정보 탭은 사실판을 보인다") {
                page.locator("#ptab-facts").click()
                page.waitForSelector("#detail:not([hidden])")
                page.locator("#stream .cfill[hidden]").count() shouldBe 1
            }
            Then("작업공간 탭은 왼쪽 기둥을, 진행 탭은 오른쪽 판을 보인다") {
                page.locator("#ptab-files").click()
                page.waitForSelector("#filecol:not([hidden])")
                page.locator("#detail[hidden]").count() shouldBe 1
                page.locator("#ptab-plan").click()
                page.waitForSelector("#sidecol:not([hidden])")
                page.locator("#filecol[hidden]").count() shouldBe 1
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
                page.locator("#stream:not([hidden])").count() shouldBe 1
                page.locator("#filecol:not([hidden])").count() shouldBe 1
                page.locator("#sidecol:not([hidden])").count() shouldBe 1
            }
        }
    }
})