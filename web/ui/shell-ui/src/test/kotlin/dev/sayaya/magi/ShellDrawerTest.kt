package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe

/**
 * 드로어의 계약: 마크업(레일·버거·스크림), 두 속성의 상태 기계(nav=폭, nav-wide=모양,
 * 닫힘은 250ms 뒤 모양), 선택 표시, 목적지 모듈 로드(가짜 로더가 window에 적는다).
 */
@GwtHtml("shelltest.html")
internal class ShellDrawerTest : GwtTestSpec({
    Given("셸이 선 화면") {
        When("첫 그리기가 끝나면") {
            Then("레일과 버거와 스크림이 서고, 문은 이식된 화면 수만큼만 있다") {
                page.waitForSelector("#rail")
                page.locator("#rail #railMenu").count() shouldBe 1
                page.locator("#scrim").count() shouldBe 1
                // 매일 다니는 문은 위, 접근 제어는 발치 — 운영의 그 자리(#railFoot).
                page.locator("#railNav .raili").count() shouldBe 2
                page.locator("#railFoot .raili").count() shouldBe 1
            }
            Then("주소의 목적지(fleet)가 선택돼 있고, 그 모듈이 옷을 입은 채 한 번 로드된다") {
                page.locator("#railNav .raili[selected]").count() shouldBe 1
                page.locator("#railNav .raili[aria-current=page]").count() shouldBe 1
                // 주소는 fleet이고 그 화면을 그리는 모듈은 companion이다 — 목록과 상세가
                // 한 모듈의 두 얼굴이라서(카탈로그가 그 둘을 가른다).
                page.evaluate("window.__magi_test_loads") shouldBe "[companion+css]"
            }
        }
        When("버거를 누르면") {
            page.locator("#railMenu").click()
            Then("드로어가 열리고(nav=open, 모양은 nav-wide) 라벨은 메뉴 레일이 말한다") {
                page.waitForSelector("body[nav=open]")
                page.waitForSelector("body[nav-wide]")
                // 라벨은 폭 트랜지션을 따라온다(원본의 지연) — 보일 때까지 기다려 잰다.
                // 언어 팩이 없는 테스트 페이지라 키가 폴백이다 — 구조가 계약이고 문구는 팩의 몫.
                page.locator("#railNav .raili .lbl").first().waitFor()
                page.locator("#railNav .raili .lbl").first().textContent() shouldBe "nav.companions"
            }
            Then("툴 레일(2단)은 속이 비어 펼쳐지지 않는다 — handbook 규칙, 아직 용례가 없다") {
                page.locator("#railPanel").isVisible() shouldBe false
                page.locator("#railPanel *").count() shouldBe 0
            }
        }
        When("스크림을 누르면") {
            page.locator("#scrim").click()
            Then("드로어가 닫히고 2단도 함께 걷힌다") {
                page.waitForCondition { page.locator("body[nav=open]").count() == 0 }
                page.locator("#railPanel").isVisible() shouldBe false
            }
        }
        When("명단(가짜: 둘, 하나는 기다림)이 마스트헤드에 닿으면") {
            Then("바가 서고, 수와 기다림 점프가 한 줄에 선다") {
                page.waitForSelector("#masthead #state .scount")
                page.locator("#masthead .mark").textContent() shouldBe "MAGI"
                page.locator("#state .jump").count() shouldBe 1
                page.locator("#state.asking").count() shouldBe 1
                page.locator("#state.live").count() shouldBe 1
            }
            Then("컴패니언 문의 배지가 기다림 수를 단다") {
                page.locator("#railBadge:not([hidden])").count() shouldBe 1
                page.evaluate("document.getElementById('railBadge').value") shouldBe "1"
            }
        }
        When("선 목적지를 다시 눌러도") {
            page.locator("#railNav .raili").first().click()
            Then("로드는 여전히 한 번이다") {
                // 주소는 fleet이고 그 화면을 그리는 모듈은 companion이다 — 목록과 상세가
                // 한 모듈의 두 얼굴이라서(카탈로그가 그 둘을 가른다).
                page.evaluate("window.__magi_test_loads") shouldBe "[companion+css]"
            }
        }
        When("화면이 이동의 문(GoSharing)으로 컴패니언을 청하면") {
            page.evaluate("window.__magi_go('/tmp/a1.sock', '')")
            Then("주소가 그 컴패니언(?d=)이 되고, 스트림이 그리로 조준된다") {
                page.waitForCondition { page.url().contains("d=") }
                page.evaluate("window.__magi_test_aim") shouldBe "/tmp/a1.sock"
            }
            Then("상세도 같은 모듈이다 — 자식(타입 UI)은 그 패널이 제 안에서 들인다") {
                page.evaluate("window.__magi_test_loads") shouldBe "[companion+css]"
            }
            Then("레일의 선택은 여전히 컴패니언 문이다 — 컴패니언은 그 문의 안이다") {
                page.locator("#railNav .raili[selected]").count() shouldBe 1
            }
        }
        When("뒤로가면 카탈로그로 돌아오고 조준이 풀린다") {
            page.goBack()
            Then("주소에 d가 없고 조준이 비었다") {
                page.waitForCondition { !page.url().contains("d=") }
                page.evaluate("window.__magi_test_aim") shouldBe ""
            }
        }
        When("컴패니언에 서면 크럼의 두 끝에 이름이 붙는다") {
            page.evaluate("window.__magi_go('/tmp/a1.sock', '')")
            page.waitForCondition { page.url().contains("d=") }
            Then("나가는 길은 .up, 서 있는 곳은 .leaf — 폰이 보이는 그 둘이다") {
                page.waitForSelector("#crumbs #back.up")
                page.locator("#crumbs .here.leaf").count() shouldBe 1
                // 화살표가 이름에 섞이지 않게 낱말이 이름을 이긴다.
                page.locator("#crumbs #back").getAttribute("aria-label") shouldBe "nav.companions"
            }
            page.evaluate("window.__magi_go_view('fleet')")
            page.waitForCondition { !page.url().contains("d=") }
            Then("목록으로 돌아오면 계단은 하나 — 그것이 leaf다") {
                page.waitForSelector("#crumbs #back.leaf")
                page.locator("#crumbs .up").count() shouldBe 0
            }
        }
        When("컴패니언에 다시 서서 지난 일 층위의 문(__magi_go_past)을 청하면") {
            page.evaluate("window.__magi_go('/tmp/a1.sock', '')")
            page.waitForCondition { page.url().contains("d=") }
            page.evaluate("window.__magi_go_past('')")
            Then("주소에 빈 past가 실린다 — 빈 값도 값이다(목록)") {
                page.waitForCondition { page.url().contains("past=") }
            }
            page.evaluate("window.__magi_go_past(null)")
            Then("null이면 지금 대화로 — past가 걷힌다") {
                page.waitForCondition { !page.url().contains("past=") }
                page.url().contains("d=") shouldBe true
            }
            // 다음 장면은 카탈로그에서 시작한다 — 히스토리에 기대지 않고 문으로 나간다.
            page.evaluate("window.__magi_go_view('fleet')")
            page.waitForCondition { !page.url().contains("d=") }
        }
        When("문에 도구 둘이 등록되면(접힌 드로어)") {
            // 직전 클릭들이 포인터를 레일 위에 두고 갔다 — 손끝이 남아 있으면 피크(expand)가
            // 정답이라, 접힌 기둥(collapse)을 재려면 손끝부터 치운다.
            page.mouse().move(700.0, 400.0)
            page.evaluate("window.__magi_test_provide_tools()")
            Then("접힌 기둥이 툴 레일이 된다 — 메뉴는 숨고, ←와 도구 아이콘들") {
                page.waitForSelector("#rail[tool=collapse]")
                page.locator("#rail[menu=hide]").count() shouldBe 1
                page.locator("#railNav").isVisible() shouldBe false
                page.locator("#railTool .raili.tooli").count() shouldBe 2
                page.locator("#railToolClose").count() shouldBe 1
            }
            Then("도구를 누르면 그 도구의 일이 돈다 — 주소는 그대로다") {
                page.locator("#railTool .raili.tooli").first().click()
                page.evaluate("window.__magi_test_tool_ran") shouldBe "hammer"
                page.url().contains("v=") shouldBe false
            }
        }
        When("←(메뉴 레일로)를 누르면") {
            page.locator("#railToolClose").click()
            Then("메뉴 기둥이 돌아오고 선택은 유지된다") {
                page.waitForSelector("#rail[menu=collapse]")
                page.locator("#rail[tool=hide]").count() shouldBe 1
                page.locator("#railNav").isVisible() shouldBe true
                page.locator("#railNav .raili[selected]").count() shouldBe 1
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("레일은 하단 바가 된다 — 버거는 사라지고, 문들이 가로로 선다(운영 css)") {
                page.waitForCondition { !(page.locator("#railMenu").isVisible()) }
                (page.evaluate("getComputedStyle(document.getElementById('railNav')).flexDirection")) shouldBe "row"
                (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean) shouldBe true
            }
            Then("컴패니언 화면에선 하단 바가 물러난다(body[at=agent])") {
                page.evaluate("window.__magi_go('/tmp/a1.sock', '')")
                page.waitForSelector("body[at=agent]")
                page.locator("#rail").isVisible() shouldBe false
                page.goBack()
                page.waitForSelector("body[at=list]")
            }
            page.setViewportSize(1280, 800)
        }
        When("도구 있는 문에서 드로어를 열면") {
            page.locator("#railMenu").click()
            Then("메뉴 레일이 라벨과 함께 서고, 툴 레일은 둘째 기둥이 된다(←의 닫힘은 열림이 걷는다)") {
                page.waitForSelector("body[nav=open]")
                page.waitForSelector("#rail[tool=expand]")
                page.locator("#rail[menu=expand]").count() shouldBe 1
                page.locator("#railTool .railpanel-head").textContent() shouldBe "nav.companions"
                page.locator("#railTool .raili.tooli").count() shouldBe 2
            }
        }
    }
})
