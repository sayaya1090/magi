package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/**
 * 컴패니언 화면(타입 1 = 코딩 에이전트) 관통 — 가짜 포트의 고정 전사로 그린 DOM을 검사한다.
 * 언어 팩은 테스트 페이지에서 못 읽으므로(키 폴백) 문구가 아니라 구조와 키를 잰다.
 */
@GwtHtml("codingtest.html")
internal class CodingScreenTest : GwtTestSpec({
    Given("고정 컨텍스트(타입 1)와 여섯 행의 전사") {
        When("화면이 그려지면") {
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
            page.locator("#conversation .composer #t textarea").fill("keep going")
            page.locator("#conversation .composer #send").click()
            Then("그 컴패니언(?d=)으로 간다 — 가짜가 창에 적는다") {
                page.waitForCondition { page.evaluate("window.__magi_test_sent") != null }
                page.evaluate("window.__magi_test_sent") shouldBe "keep going@/tmp/a1.sock"
            }
            Then("보낸 뒤 상자는 비어 있다") {
                page.locator("#conversation .composer #t textarea").inputValue() shouldBe ""
            }
        }
        When("지난 일 층위(빈 past=목록)로 갈아타면") {
            page.evaluate("window.__magi_test_past('')")
            Then("지금-대화의 판들이 물러나고 목록이 선다 — 여는 길은 행이다") {
                page.waitForSelector("#agentdetail:not([hidden]) .hs")
                page.locator("#log[hidden]").count() shouldBe 1
                page.locator("#conversation form[hidden]").count() shouldBe 1
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
                page.locator("#conversation form:not([hidden])").count() shouldBe 1
            }
        }
        When("워크스페이스(왼쪽)가 그려지면") {
            Then("트리 카드와 git 카드가 각자 선다 — 하나의 스크롤로 묶지 않는다") {
                page.waitForSelector("#leftslot #files .pane-files")
                page.locator("#files .filescard").count() shouldBe 2
                page.locator("#files .pane-files .treerow").count() shouldBe 3
            }
            Then("git은 브랜치와 앞선 수를, 변경은 두 무리로 말한다") {
                page.locator("#files .pane-git .gitbranch").textContent() shouldBe "main"
                page.locator("#files .pane-git .gitab.ahead").textContent() shouldContain "2"
                page.locator("#files .pane-git .gitgroup").count() shouldBe 2
                page.locator("#files .pane-git .gitline").count() shouldBe 2
            }
            Then("첫 걸음은 뿌리 하나만 읽는다 — 열지도 않은 가지를 걷지 않는다") {
                page.evaluate("window.__magi_test_dirs") shouldBe "."
            }
        }
        When("디렉토리를 펼치면") {
            page.locator("#files .treerow.dir").first().click()
            Then("그 디렉토리 하나만 더 읽고, 자식들이 한 칸 안으로 선다") {
                page.waitForCondition { page.locator("#files .pane-files .treerow").count() == 5 }
                page.evaluate("window.__magi_test_dirs") shouldBe ".,src"
                page.locator("#files .treerow[style*='--d: 1']").count() shouldBe 2
            }
        }
        When("파일을 누르면") {
            page.locator("#files .treerow:not(.dir)").first().click()
            Then("같은 왼쪽에서 본문이 열린다 — 가운데는 대화의 자리다") {
                page.waitForSelector("#files .fileview .filebody")
                page.evaluate("window.__magi_test_opened") shouldBe "src/main.go"
                page.locator("#files .fileview .filebody").textContent() shouldContain "package main"
                page.locator("#cframe, #conversation").first().isVisible() shouldBe true
            }
            page.locator("#files .fileview .fileclose").click()
            Then("닫으면 트리만 남는다") {
                page.waitForCondition { page.locator("#files .fileview").count() == 0 }
            }
        }
        When("이름으로 찾으면") {
            page.locator("#files #wsfind input, #files #wsfind textarea").first().fill("main")
            Then("결과가 트리를 대신한다 — 찾는 동안 판이 보이는 것은 결과다") {
                page.waitForSelector("#files .hits .treerow.hit")
                page.evaluate("window.__magi_test_find") shouldBe "name:main"
                page.locator("#files .hits .treerow.hit").count() shouldBe 2
                page.locator("#files .pane-files .treerow.dir").count() shouldBe 0
            }
            Then("결과를 누르면 그 파일이 열린다") {
                page.locator("#files .hits .treerow.hit").first().click()
                page.waitForSelector("#files .fileview .filebody")
                page.evaluate("window.__magi_test_opened") shouldBe "src/main.go"
            }
            page.locator("#files .fileview .fileclose").click()
            page.locator("#files .findclear").click()
            Then("지우면 트리가 돌아온다") {
                page.waitForCondition { page.locator("#files .pane-files .treerow.dir").count() == 1 }
            }
        }
        When("git 줄의 스테이지를 누르면") {
            // 손잡이는 손끝이 왔을 때만 보인다(운영 규칙) — 그 노출은 아래에서 따로 재고,
            // 여기서는 눌렀을 때 무엇이 되는지를 잰다: 헤드리스의 hover 상태에 이 단언을
            // 매달면 재는 것이 바뀐다(가려짐 대기 30초로 실측).
            page.locator("#files .gitline .act.stage").first().evaluate("b => b.click()")
            Then("그 경로가 커밋이 실어 갈 쪽으로 간다") {
                page.waitForCondition { page.evaluate("window.__magi_test_gitdo") != null }
                page.evaluate("window.__magi_test_gitdo") shouldBe "stage|README.md|"
            }
        }
        When("메시지를 적고 커밋하면") {
            page.locator("#files #gitmsg textarea").fill("fix the retry window")
            page.locator("#files #gitcommitgo").click()
            Then("그 메시지로 커밋한다 — 실린 것이 있을 때만 눌린다") {
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_gitdo") as String).startsWith("commit")
                }
                page.evaluate("window.__magi_test_gitdo") shouldBe "commit||fix the retry window"
            }
        }
        When("파일을 지우려 두 번 누르면") {
            page.locator("#files .treeline .act.drop").first().evaluate("b => b.click()")
            Then("먼저 확인으로 무장한다") {
                page.locator("#files .act.drop.armed").count() shouldBe 1
            }
            page.locator("#files .act.drop.armed").evaluate("b => b.click()")
            Then("그 파일이 지워지고 다시 걷는다") {
                page.waitForCondition { page.evaluate("window.__magi_test_filedo") != null }
                (page.evaluate("window.__magi_test_filedo") as String).startsWith("delete|") shouldBe true
            }
        }
        When("행의 손잡이는 읽는 자리를 차지하지 않는다") {
            Then("쉬는 동안은 숨어 있다 — 손끝이나 포커스가 왔을 때만 나온다(CSS 계약)") {
                // 앞 장면이 남긴 손끝과 포커스를 먼저 치운다 — 둘 다 이 규칙을 켜는 조건이라,
                // 남겨 두면 "쉬는 동안"을 재는 것이 아니게 된다(실측: visible로 나왔다).
                page.mouse().move(5.0, 5.0)
                page.evaluate("document.activeElement && document.activeElement.blur()")
                page.locator("#files .gitacts").first().evaluate(
                    "e => getComputedStyle(e).visibility") shouldBe "hidden"
                page.locator("#files .rowacts").first().evaluate(
                    "e => getComputedStyle(e).visibility") shouldBe "hidden"
                // 규칙 자체가 있는가 — 손끝이 오면 보이라고 적혀 있어야 한다.
                (page.evaluate(
                    "[...document.styleSheets].some(s => { try { return [...s.cssRules].some(r =>" +
                    " r.selectorText && r.selectorText.includes(':hover .gitacts')) } catch (e) { return false } })"
                ) as Boolean) shouldBe true
            }
        }
        When("새 파일 이름을 적고 만들면") {
            page.locator("#files #wsnew input, #files #wsnew textarea").first().fill("docs/new.md")
            page.locator("#files .makefile").click()
            Then("그 경로로 만든다") {
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_filedo") as String).startsWith("new-file")
                }
                page.evaluate("window.__magi_test_filedo") shouldBe "new-file|docs/new.md|"
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤이 없고, 전사와 컴포저가 그대로 쓸 만하다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                page.locator("#log .row").first().isVisible() shouldBe true
                page.locator("#conversation .composer #t").isVisible() shouldBe true
            }
        }
    }
})
