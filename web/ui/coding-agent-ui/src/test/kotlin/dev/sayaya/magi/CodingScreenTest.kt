package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.assertions.withClue
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain
import io.kotest.matchers.string.shouldNotContain

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
        }
        When("컴포저에 한 마디 적어 보내면") {
            page.locator("#dock .composer #t textarea").fill("keep going")
            page.locator("#dock .composer #send").click()
            Then("그 컴패니언(?d=)으로 간다 — 가짜가 창에 적는다") {
                page.waitForCondition { page.evaluate("window.__magi_test_sent") != null }
                page.evaluate("window.__magi_test_sent") shouldBe "keep going@/tmp/a1.sock"
            }
            Then("보낸 뒤 상자는 비어 있다") {
                page.locator("#dock .composer #t textarea").inputValue() shouldBe ""
            }
        }
        When("지난 일 층위(빈 past=목록)로 갈아타면") {
            page.evaluate("window.__magi_test_past('')")
            Then("지금-대화의 판들이 물러나고 목록이 선다 — 여는 길은 행이다") {
                page.waitForSelector("#agentdetail:not([hidden]) .hs")
                page.locator("#log[hidden]").count() shouldBe 1
                page.locator("#dock form[hidden]").count() shouldBe 1
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
                page.locator("#dock form:not([hidden])").count() shouldBe 1
            }
        }
        When("워크스페이스(왼쪽)가 그려지면") {
            Then("트리 카드와 git 카드가 각자 선다 — 하나의 스크롤로 묶지 않는다") {
                page.waitForSelector("#filecol #files .pane-files")
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
            Then("본문은 가운데의 카드로 열린다 — 18rem 기둥은 코드를 읽는 폭이 아니다") {
                page.waitForSelector("#fileview .filebody")
                page.evaluate("window.__magi_test_opened") shouldBe "src/main.go"
                page.locator("#fileview .filebody").textContent() shouldContain "package main"
                page.locator("#cframe, #conversation").first().isVisible() shouldBe true
            }
            Then("번호는 제 기둥에 서고 본문에는 없다 — 끌어 복사하면 코드만 딸려온다") {
                // 읽기 툴이 낸 `번호⇥본문`을 그대로 두면 붙여 넣은 모든 줄 앞에 번호가 붙는다.
                page.locator("#fileview .filebody .filegutter").textContent()?.trim() shouldBe "1\n2\n3"
                page.locator("#fileview .filebody .filecode").textContent() shouldContain "package main"
                (page.locator("#fileview .filebody .filecode").textContent() ?: "") shouldNotContain "1\tpackage"
            }
            Then("훑기가 주석을 가른다 — 파서가 아니라 표시다") {
                page.locator("#fileview .filebody .filecode .tok-note").first().textContent() shouldContain "// go"
            }
            Then("경로는 통째로 적힌다 — 사람이 복사해 명령에 붙이는 줄이다") {
                page.locator("#fileview .filebar .filedir").textContent() shouldBe "src/main.go"
            }
        }
        When("파일을 하나 더 열면") {
            page.locator("#files .treerow:not(.dir)").nth(1).click()
            Then("먼저 연 것은 닫히지 않는다 — 두 파일을 견주는 일이 화면을 오가는 일이 되지 않게") {
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_cards") as? String)?.contains("|") == true
                }
                // 카드는 Element이면서 닫힐 수 있는 것이다: id가 신원, title이 탭 이름, close()가 닫는 법.
                val cards = (page.evaluate("window.__magi_test_cards") as String).split("|")
                cards.size shouldBe 2
                cards.all { it.endsWith("+x") } shouldBe true
                cards.map { it.substringBefore("=") }.toSet().size shouldBe 2
                cards[0].substringAfter("=") shouldBe "main.go+x"
            }
            Then("하나를 닫아도 나머지는 그대로다") {
                page.evaluate("window.__magi_cards[0].close()")
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_cards") as? String)?.contains("|") == false
                }
            }
            page.locator("#files .treerow:not(.dir)").first().click()
            page.waitForSelector("#fileview .filebody")
        }
        When("고치기를 누르면") {
            page.locator("#fileview .filebar .fileacts md-text-button").first().click()
            Then("같은 그림에 캐럿이 생긴다 — 번호 기둥은 그대로다") {
                page.waitForSelector("#fileview .fileedit .fileeditarea")
                page.locator("#fileview .fileedit .filebody.editbody .filegutter").count() shouldBe 1
                page.locator("#fileview .fileedit .editghost").count() shouldBe 1
            }
            Then("저장은 고친 자리만 보낸다 — 패치는 거절될 수 있고 통짜는 남의 일을 덮는다") {
                page.locator("#fileview .fileedit .fileeditarea").fill("package main\n\nfunc main() { println(1) } // go\n")
                page.locator("#fileview .filebar .fileacts md-filled-button").click()
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_save") as? String)?.contains("patch:") == true
                }
                val sent = page.evaluate("window.__magi_test_save") as String
                sent shouldContain "src/main.go|patch:diff --git a/src/main.go"
                sent shouldContain "+func main() { println(1) } // go"
            }
            Then("저장하면 읽는 그림으로 돌아온다 — 디스크의 그 파일이 사실이다") {
                page.waitForSelector("#fileview .filebody .filecode")
                page.locator("#fileview .fileedit").count() shouldBe 0
            }
        }
        When("찾기를 누르면") {
            page.locator("#files .filefind md-text-button").first().click()
            Then("무엇을 어디서 찾을지 묻는 상자가 뜬다 — 좁은 기둥에 상자를 늘 펼쳐 두지 않는다") {
                page.waitForSelector("md-dialog.askline md-outlined-text-field")
                page.locator("md-dialog.askline .askwhere .wherechip").count() shouldBe 2
            }
            Then("이름으로 찾으면 결과가 트리를 대신한다 — 찾는 동안 판이 보이는 것은 결과다") {
                page.locator("md-dialog.askline md-outlined-text-field input, " +
                    "md-dialog.askline md-outlined-text-field textarea").first().fill("main")
                page.locator("md-dialog.askline md-filled-button").click()
                page.waitForSelector("#files .hits .treerow.hit")
                page.evaluate("window.__magi_test_find") shouldBe "name:main"
                page.locator("#files .hits .treerow.hit").count() shouldBe 2
                page.locator("#files .pane-files .treerow.dir").count() shouldBe 0
            }
            Then("찾는 중이면 무엇을 찾았는지 말하고, 다시 찾기와 지우기를 준다") {
                page.locator("#files .filefind .findnow").textContent() shouldContain "files.found_in_names"
                page.locator("#files .filefind md-text-button").count() shouldBe 2
            }
            Then("결과를 누르면 그 파일이 열린다") {
                page.locator("#files .hits .treerow.hit").first().click()
                page.waitForSelector("#fileview .filebody")
                page.evaluate("window.__magi_test_opened") shouldBe "src/main.go"
            }
            Then("지우면 트리가 돌아온다") {
                page.locator("#files .filefind md-text-button").last().click()
                page.waitForCondition { page.locator("#files .pane-files .treerow.dir").count() == 1 }
            }
        }
        When("행의 메뉴에서 지우면") {
            page.locator("#files .treeline").first().hover()
            page.locator("#files .treeline .rowmenu md-icon-button").first().click()
            Then("할 수 있는 여섯이 한 메뉴에 선다 — 18rem 기둥에 버튼 여섯은 서지 못한다") {
                page.waitForSelector("#files .treeline .rowmenu md-menu md-menu-item")
                page.locator("#files .treeline .rowmenu").first()
                    .locator("md-menu-item").count() shouldBe 6
            }
            Then("지우기는 무엇이 사라지는지 이름을 대고 묻는다 — 되돌릴 수 없어서") {
                page.locator("#files .treeline .rowmenu").first().locator("md-menu-item").last().click()
                page.waitForSelector("md-dialog.askconfirm")
                page.locator("md-dialog.askconfirm .asksay").textContent() shouldBe "files.delete_body"
                page.locator("md-dialog.askconfirm md-filled-tonal-button").click()
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
                page.locator("#files .rowmenu").first().evaluate(
                    "e => getComputedStyle(e).visibility") shouldBe "hidden"
                // 규칙 자체가 있는가 — 손끝이 오면 보이라고 적혀 있어야 한다.
                (page.evaluate(
                    "[...document.styleSheets].some(s => { try { return [...s.cssRules].some(r =>" +
                    " r.selectorText && r.selectorText.includes(':hover .gitacts')) } catch (e) { return false } })"
                ) as Boolean) shouldBe true
            }
        }
        When("행의 메뉴에서 새 파일을 만들면") {
            page.locator("#files .treeline").first().hover()
            page.locator("#files .treeline .rowmenu md-icon-button").first().click()
            page.waitForSelector("#files .treeline .rowmenu md-menu md-menu-item")
            page.locator("#files .treeline .rowmenu").first().locator("md-menu-item").first().click()
            Then("어디에 만들지 미리 채워 묻는다 — 누른 그 줄 아래가 자연스럽다") {
                page.waitForSelector("md-dialog.askline md-outlined-text-field")
                page.locator("md-dialog.askline md-outlined-text-field input, " +
                    "md-dialog.askline md-outlined-text-field textarea").first().fill("docs/new.md")
                page.locator("md-dialog.askline md-filled-button").click()
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_filedo") as String).startsWith("new-file")
                }
                page.evaluate("window.__magi_test_filedo") shouldBe "new-file|docs/new.md|"
            }
            // 다음 장면에 상자나 메뉴를 열어 둔 채 넘기지 않는다 — 열린 모달 위로는 아무것도
            // 누를 수 없어, 그 뒤 장면이 통째로 시간 초과가 된다(실측).
            page.evaluate("(() => { document.querySelectorAll('md-dialog').forEach(d => d.close && d.close());" +
                " document.querySelectorAll('md-menu').forEach(m => m.open = false); })()")
            page.waitForCondition { page.locator("md-dialog[open]").count() == 0 }
        }
        When("부모가 '지금은 답하는 자리다'라고 알리면") {
            page.evaluate("window.__magi_ask_test_before = document.querySelector('#dock #t')" +
                ".getAttribute('label')")
            // 쓰던 초고가 있는 채로 몫이 바뀐다 — 사람은 타이핑 중에도 질문을 받는다.
            page.locator("#dock .composer #t textarea").fill("half a request")
            page.evaluate("window.__magi_ask_publish({call:'call_9', kind:'question'," +
                " socket:'/tmp/a1.sock', peer:null})")
            Then("상자가 옷을 갈아입고, 쓰던 글은 지워지지 않고 맡겨진다") {
                page.waitForCondition {
                    page.locator("#dock #t").getAttribute("label") == "label.answer"
                }
                page.locator("#dock #send").textContent() shouldBe "action.answer"
                page.locator("#dock #cnote:not([hidden])").count() shouldBe 1
                // 맡긴 글은 화면에서 비워진다 — 답 자리에 남의 초고가 서 있으면 안 된다.
                page.locator("#dock .composer #t textarea").inputValue() shouldBe ""
            }
            Then("여기에 쓴 것은 부탁이 아니라 답으로 간다") {
                page.locator("#dock .composer #t textarea").fill("yes, drop it")
                page.locator("#dock .composer #send").click()
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_answered") as String?) != null
                }
                page.evaluate("window.__magi_test_answered") shouldBe "call_9/question/yes, drop it"
            }
        }
        When("질문이 걷히면") {
            page.evaluate("window.__magi_ask_publish(null)")
            Then("상자는 다시 부탁하는 자리가 되고, 맡겨 둔 초고가 돌아온다") {
                page.waitForCondition {
                    page.locator("#dock #t").getAttribute("label") == "label.ask"
                }
                page.locator("#dock #send").textContent() shouldBe "action.send"
                page.locator("#dock .composer #t textarea").inputValue() shouldBe "half a request"
                page.locator("#dock #cnote[hidden]").count() shouldBe 1
            }
            Then("그리고 그 뒤의 한 마디는 다시 부탁으로 간다 — 낡은 부름으로 새지 않는다") {
                page.locator("#dock .composer #t textarea").fill("next request")
                page.locator("#dock .composer #send").click()
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_sent") as String).startsWith("next request")
                }
            }
        }
        When("대화가 제 기둥에 놓이면") {
            Then("전사는 기둥 안에서 스크롤한다 — 페이지가 아니라 그것이 움직인다") {
                // 부모가 준 자리는 이미 높이가 정해져 있고, 자식은 그 안에서 남는 높이를 받는다.
                // 사이에 상자를 하나라도 끼우면 그 사슬이 끊겨 전사가 기둥 밖으로 흐른다
                // (실측: #log가 4190px로 자라 잘림 — 스크롤할 수 있는 것이 아무것도 없었다).
                page.waitForCondition {
                    (page.evaluate("document.getElementById('log').getBoundingClientRect().height")
                        as Number).toInt() > 0
                }
                (page.evaluate("(() => { const l = document.getElementById('log');" +
                    " const c = document.getElementById('stream').getBoundingClientRect().height;" +
                    " return l.getBoundingClientRect().height <= c + 1; })()") as Boolean) shouldBe true
                (page.evaluate("getComputedStyle(document.getElementById('log')).overflowY")) shouldBe "auto"
            }
            Then("컴포저는 도크의 안쪽 상자다 — 여백은 바깥 폼이 진다(운영의 두 겹)") {
                page.locator("#dock form > .composer").count() shouldBe 1
                (page.evaluate("getComputedStyle(document.querySelector('#dock .composer'))" +
                    ".paddingLeft")) shouldBe "0px"
            }
        }
        When("워크스페이스가 제 진짜 자리(18rem 열)에 놓이면") {
            // 라이브의 #cstage는 왼쪽 열을 18rem으로 잡는다(companion.css) — 테스트 페이지는
            // 몸통 폭이라 뭉갬이 재현되지 않는다. 그래서 열을 그 폭으로 세우고 잰다.
            // 실측으로 잡힌 결함이다: 셀렉트가 같은 줄에 서자 필드가 라벨 "F."만 남을 만큼 눌렸다.
            page.evaluate("document.getElementById('filecol').style.width = '18rem'")
            Then("찾기는 한 줄을 차지하지 않는다 — 누를 때만 상자가 뜬다") {
                page.waitForSelector("#files .filefind md-text-button")
                val h = (page.evaluate("document.querySelector('#files .filefind')" +
                    ".getBoundingClientRect().height") as Number).toDouble()
                // 찾는 중이 아니면 한 줄(누르는 버튼 하나)이다 — 찾는 중이면 무엇을 찾았는지
                // 말하느라 두 줄이 되는데, 그건 사람이 청한 결과다.
                withClue("찾기 줄이 ${h}px — 트리가 먼저 보여야 한다") {
                    (h <= 130.0) shouldBe true
                }
            }
            Then("줄 안의 것들이 열 밖으로 밀리지 않는다 — 눌리면 여기서 드러난다") {
                // scrollWidth로 재지 않는 이유: md-icon-button은 40px 상자 안에 48px 터치 타깃을
                // 그려서 늘 ±4px 넘친다(실측). 보이지도 않고 페이지로 새지도 않는 그 4px 때문에
                // 진짜 뭉갬을 못 보는 단언이 된다. 그래서 자식의 오른쪽 끝이 줄을 넘는지를 잰다.
                val pushedOut = page.evaluate(
                    "[...document.querySelectorAll('#files .findrow, #files .makerow')].flatMap(r => {" +
                    " const box = r.getBoundingClientRect();" +
                    " return [...r.children].filter(c => c.getBoundingClientRect().right > box.right + 1)" +
                    "   .map(c => r.className + '>' + c.tagName); }).join(', ')")
                withClue("열 밖으로 밀린 것: $pushedOut") { pushedOut shouldBe "" }
            }
            Then("판 자체도 제 열을 넘지 않는다 — 가로 스크롤바는 열에서도 결함이다") {
                val panel = page.evaluate("document.getElementById('files').scrollWidth + '>' + " +
                    "document.getElementById('files').clientWidth")
                withClue("판이 제 열보다 넓다: $panel") {
                    (page.evaluate("document.getElementById('files').scrollWidth <= " +
                        "document.getElementById('files').clientWidth + 4") as Boolean) shouldBe true
                }
            }
            Then("글자 없이 그림만 있는 컨트롤은 전부 이름을 갖는다 — 좁아서 아이콘이 된 것들이다") {
                // 글자 버튼을 아이콘 버튼으로 바꿀 때 이름이 함께 사라지는 것이 그 변경의 실패 방식이다.
                (page.evaluate(
                    "[...document.querySelectorAll('#files md-icon-button, #files button')]" +
                    ".filter(b => !b.textContent.trim())" +
                    ".every(b => (b.getAttribute('aria-label') || '').trim().length > 0)") as Boolean) shouldBe true
                (page.locator("#files .rowmenu md-icon-button").count() > 0) shouldBe true
            }
            page.evaluate("document.getElementById('filecol').style.width = ''")
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤이 없고, 전사와 컴포저가 그대로 쓸 만하다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                page.locator("#log .row").first().isVisible() shouldBe true
                page.locator("#dock .composer #t").isVisible() shouldBe true
            }
        }
    }
})
