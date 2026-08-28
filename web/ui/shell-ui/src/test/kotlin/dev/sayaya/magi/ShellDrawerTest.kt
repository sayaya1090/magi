package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.assertions.withClue
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
                // 매일 다니는 문은 위(컴패니언·지식·회의), 접근 제어는 발치 — 운영의 그 자리.
                page.locator("#railNav .raili").count() shouldBe 3
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
        When("가짜 회선이 턴이 열렸다고 말하면") {
            Then("턴바는 창의 것이다 — body의 직계로 서서 창 폭을 가로지른다") {
                page.waitForSelector("#turnwrap:not([hidden])")
                // 화면 기둥 안에 서면 position:fixed의 기준 상자가 그 기둥이 된다(실측으로
                // 한 번 밟은 함정) — 그래서 부모의 직계인지를 잰다.
                page.evaluate("document.getElementById('turnwrap').parentElement.tagName") shouldBe "BODY"
                page.locator("#turnwrap md-linear-progress#turnbar").count() shouldBe 1
                // 4초에서 시작해 이 창의 시계로 흐른다 — 초는 재지 말고 모양(s)만 잰다.
                Regex("\\d+s").matches(page.locator("#turnfor").textContent() ?: "") shouldBe true
            }
            Then("창 폭을 다 쓴다 — 기둥 폭이 아니라") {
                val w = page.evaluate("document.getElementById('turnwrap').getBoundingClientRect().width") as Number
                // 레이아웃 뷰포트로 잰다: window.innerWidth는 고전 스크롤바(리눅스 헤드리스)를
                // 포함해서, 창 전체를 덮은 fixed 요소가 15px 모자라 보인다(CI에서만 실패했다).
                val win = page.evaluate("document.documentElement.clientWidth") as Number
                // 고전 스크롤바(리눅스 헤드리스)는 어느 폭에도 잡히지 않으면서 fixed 요소의
                // 오른쪽을 15px 밀어낸다 — 그만큼은 봐준다. 재려는 것은 "창이냐 기둥이냐"이고,
                // 기둥이면 절반이다.
                withClue("턴바 ${w.toDouble()}px / 레이아웃 뷰포트 ${win.toDouble()}px") {
                    (w.toDouble() >= win.toDouble() - 24) shouldBe true
                }
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
        When("멈춘 컴패니언(명단에 있고 답하지 않는다) 곁에 서면") {
            page.evaluate("window.__magi_go('/tmp/gone.sock', '')")
            page.waitForCondition { page.url().contains("gone.sock") }
            Then("점이 끊김을 말한다 — 회선은 멀쩡하다(가짜 회선은 link(true)다)") {
                // 재는 것은 바로 이것이다: 서버는 서 있고 데몬만 죽었다. 점이 회선 하나만
                // 읽으면 여기서 초록으로 남는다 — 운영이 세 번째 사실을 배운 그 자리.
                page.waitForSelector("#masthead #state.lost")
                page.locator("#state.live").count() shouldBe 0
                // 컴패니언 곁에서 이 줄은 점 하나가 전부다(수는 걷힌다) — 그래서 말로도 적는다.
                // 팩이 없는 페이지라 키가 곧 문구다.
                page.locator("#state").getAttribute("aria-label") shouldBe "state.lost"
            }
            Then("답하는 컴패니언으로 옮기면 점이 돌아온다 — 사실이 바뀐 것이지 회선이 아니다") {
                page.evaluate("window.__magi_go('/tmp/a1.sock', '')")
                page.waitForSelector("#masthead #state.live")
                page.locator("#state.lost").count() shouldBe 0
                page.locator("#state").getAttribute("aria-label") shouldBe "state.live"
            }
            Then("목록으로 나오면 다시 회선만의 것이다 — 하나가 멈춘 것을 이 점이 말할 수는 없다") {
                page.evaluate("window.__magi_go_view('fleet')")
                page.waitForCondition { !page.url().contains("d=") }
                page.locator("#state.live").count() shouldBe 1
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
        When("⌘K를 누르면") {
            page.keyboard().press("Meta+k")
            Then("팔레트가 서고, 이름 없이도 갈 수 있는 곳들이 이미 있다") {
                page.waitForSelector("#palDialog[open] #palField")
                (page.locator("#palList .palrow").count() > 0) shouldBe true
                // 목록이라고 말한다 — 스크린리더에게도 목록이어야 고를 수 있다.
                (page.locator("#palList").getAttribute("role")) shouldBe "listbox"
                (page.locator("#palList .palrow").first().getAttribute("aria-selected")) shouldBe "true"
            }
            Then("적으면 좁혀지고, 앞에서 맞은 것이 위로 온다") {
                page.locator("#palDialog #palField input, #palDialog #palField textarea").first().fill("shared")
                page.waitForCondition { page.locator("#palList .palrow").count() == 1 }
                // 팩이 없는 페이지라 이름이 곧 키다 — 문구가 아니라 "앞에서 맞은 것이 위"를 잰다.
                page.locator("#palList .palrow .palname").first().textContent() shouldBe "nav.shared"
            }
            Then("화살표가 고르는 자리를 옮긴다") {
                page.locator("#palDialog #palField input, #palDialog #palField textarea").first().fill("")
                page.waitForCondition { page.locator("#palList .palrow").count() > 1 }
                page.keyboard().press("ArrowDown")
                page.waitForCondition {
                    page.locator("#palList .palrow").nth(1).getAttribute("aria-selected") == "true"
                }
            }
            Then("고르면 그리로 가고 상자는 걷힌다") {
                page.locator("#palDialog #palField input, #palDialog #palField textarea").first().fill("shared")
                page.waitForCondition { page.locator("#palList .palrow").count() == 1 }
                page.keyboard().press("Enter")
                page.waitForCondition { page.url().contains("v=skills") }
                page.waitForCondition { page.locator("#palDialog[open]").count() == 0 }
                page.evaluate("window.__magi_go_view('fleet')")
                page.waitForCondition { !page.url().contains("v=") }
            }
        }
        When("화면이 제 것을 팔레트에 더하면") {
            page.evaluate("(() => { const e = {kind:'pal.kind_file', name:'main.go'," +
                " hint:'cmd/main.go', run: () => { window.__magi_palette_ran = 'main.go' }};" +
                " window.__magi_palette = [e];" +
                " const l = window.__magi_palette_obs; if (l) l([e]); })()")
            // 팔레트가 이미 서 있으면 그 자리에서 갱신된다 — 열기 전이면 열 때 모은다.
            Then("셸이 모르는 그 항목도 함께 찾힌다 — 자식의 기능을 셸이 알 필요는 없다") {
                page.keyboard().press("Meta+k")
                page.waitForSelector("#palDialog[open] #palField")
                page.locator("#palDialog #palField input, #palDialog #palField textarea").first().fill("main.go")
                page.waitForCondition { page.locator("#palList .palrow").count() >= 1 }
                page.locator("#palList .palrow .palname").first().textContent() shouldBe "main.go"
                page.keyboard().press("Enter")
                page.waitForCondition { page.evaluate("window.__magi_palette_ran") == "main.go" }
            }
            // 다음 장면에 상자를 열어 둔 채 넘기지 않는다 — 화면을 덮은 채로는 아무것도 못 잰다.
            // 이미 걷힌 상자를 또 닫지 않는다 — md-dialog의 close는 비동기다.
            page.evaluate("(() => { window.__magi_palette = []; const d = document.getElementById('palDialog');" +
                " if (d && d.open && d.close) d.close(); })()")
            // 겉의 open 속성이 걷혔다고 다 걷힌 것이 아니다: md-dialog는 닫는 애니메이션이 끝난
            // <b>뒤에</b> 안쪽 <dialog>를 닫는다. 그 사이에 다시 show()를 하면 새로 연 상자를
            // 늦게 도착한 close의 꼬리가 도로 닫는다(실측: open→showModal.ok→close("")→closed가
            // 1ms 안에 줄지어 찍혔고 ⌘K는 조용히 아무 일도 하지 않았다). 안쪽까지 닫힌 것을 본다.
            page.waitForCondition {
                page.evaluate("(() => { const d = document.getElementById('palDialog');" +
                    " const i = d && d.shadowRoot && d.shadowRoot.querySelector('dialog');" +
                    " return !!d && !d.open && !(i && i.open) })()") == true
            }
        }
        // ── 좁은 화면의 나가는 길 ✕ (운영 closeX) ────────────────────────────────
        When("상자를 열어 두고 나가는 길을 재면") {
            // 앞 장면이 상자를 닫은 직후다. 여기서 곧바로 다시 열면 md-dialog가 제 닫기 꼬리로
            // 방금 연 상자를 도로 닫는다(실측 순서: open@1788 → showModal.ok@1789 → close("")@1789
            // → closed@1789 → opened@2294 — ⌘K는 조용히 아무 일도 하지 않은 것처럼 보인다).
            // 겉의 open 속성도, 그림자 안쪽 <dialog>.open도 그 꼬리가 남았는지 말해 주지 않는다.
            // 그러니 열릴 때까지 문을 다시 두드린다 — 사람이 하는 그대로고, show()는 이미 열려
            // 있으면 곧장 돌아서므로 두 번 눌러도 상자가 겹치지 않는다.
            page.waitForCondition {
                if (page.locator("#palDialog[open] #palField").count() == 0) {
                    page.keyboard().press("Meta+k")
                    page.waitForTimeout(300.0)
                }
                page.locator("#palDialog[open] #palField").count() > 0
            }
            Then("✕는 머리글이 아니라 내용 슬롯에 있다") {
                // 머리글 슬롯에 두면 md-dialog가 거기서 제 이름을 가져가 상자가 "닫기 (제목)"으로
                // 제 이름을 댄다(운영 실측 다섯 자리). 그래서 이 슬롯이 계약이다.
                page.locator("#palDialog > .dlgclose").count() shouldBe 1
                page.locator("#palDialog > .dlgclose").getAttribute("slot") shouldBe "content"
                page.locator("#palDialog [slot=headline] md-icon-button").count() shouldBe 0
                // 이름은 겉에 남지 않는다: Material이 호스트의 aria-label을 제 안쪽 <button>으로
                // 옮기고 겉에는 data-aria-label만 남긴다(실측 속성 목록: class,slot,data-aria-label,value).
                // 그래서 두 자리를 다 본다 — 운영 콘솔을 재는 프로브도 같은 이유로 그렇게 한다.
                // 팩 없는 페이지라 키가 곧 말이고, 여기서 재는 것은 이름이 붙었다는 사실뿐이다.
                page.evaluate("(() => { const x = document.querySelector('#palDialog > .dlgclose');" +
                    " return x.getAttribute('aria-label') || x.getAttribute('data-aria-label') })()") shouldBe "action.close"
            }
            Then("넓은 창에서는 서지 않는다 — 바깥을 눌러 닫을 바깥이 아직 있다") {
                page.evaluate("getComputedStyle(document.querySelector('#palDialog .dlgclose')).display") shouldBe "none"
            }
            Then("폰 폭에서는 서고, 그림이 실제로 그려진다") {
                page.setViewportSize(390, 844)
                page.waitForCondition {
                    page.evaluate("getComputedStyle(document.querySelector('#palDialog .dlgclose')).display") != "none"
                }
                // 표를 slot="icon"으로 달면 md-icon-button의 그림자에 그런 슬롯이 없어 0×0으로
                // 접힌다(실측) — 자식으로 넣은 같은 그림은 24가 된다. 그 수치가 계약이다.
                ((page.evaluate("(() => { const s = document.querySelector('#palDialog .dlgclose svg');" +
                    " return s ? Math.round(s.getBoundingClientRect().width) : 0 })()") as Number).toInt()) shouldBe 24
            }
            Then("누르면 상자가 걷힌다") {
                page.locator("#palDialog .dlgclose").click()
                page.waitForCondition { page.locator("#palDialog[open]").count() == 0 }
                page.setViewportSize(1280, 800)
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
