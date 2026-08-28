package dev.sayaya.magi

import com.microsoft.playwright.Page
import com.microsoft.playwright.options.AriaRole
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
    Given("고정 컨텍스트(타입 1)와 여덟 행의 전사") {
        When("화면이 그려지면") {
            Then("전사 여덟 행 — 목소리와 끝이 클래스에 실린다") {
                page.locator("#log .row").count() shouldBe 8
                page.locator("#log .row.user").count() shouldBe 1
                page.locator("#log .row.tool.toolok").count() shouldBe 1
                page.locator("#log .row.tool.toolfail").count() shouldBe 1
                page.locator("#log .row.pending").count() shouldBe 1
            }
            Then("모델이 쓴 글은 마크다운으로 그려진다 — 원문이 아니라 읽으라고 쓴 모양으로") {
                val said = page.locator("#log .row.assistant .txt").first()
                said.locator("strong").count() shouldBe 1
                said.locator("table thead th").count() shouldBe 2
                said.locator("table tbody td").count() shouldBe 2
                said.locator("ul li").count() shouldBe 2
                said.locator("pre code[data-lang=go]").count() shouldBe 1
            }
            Then("링크는 검사받는다 — javascript: 주소에는 href가 실리지 않는다") {
                val links = page.locator("#log .row.assistant .txt a")
                links.count() shouldBe 2
                links.nth(0).getAttribute("href") shouldBe "https://e.com"
                withClue("전사는 javascript: 주소를 실어 나를 수 있고, 그것을 실행할 수 있는 노드는 앵커뿐이다") {
                    links.nth(1).getAttribute("href") shouldBe null
                }
            }
            Then("원문의 HTML은 마크업이 아니라 글자로 선다") {
                val txt = page.locator("#log .row.assistant .txt").first()
                txt.textContent() shouldContain "<b>raw</b>"
                withClue("여기서 HTML 문자열이 만들어지면 새니타이저가 옳거나 그를 자리가 생긴다") {
                    txt.locator("b").count() shouldBe 0
                }
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
            Then("카운슬의 행은 제 자리와 제 표를 입는다 — 같은 갈색 아홉 줄이 아니라") {
                // 운영은 자리마다 색이 다르고 찬성 행이 다르게 접힌다. 그 CSS가 읽는 것은
                // 클래스뿐이라, 행이 그것을 입지 않으면 규칙 전부가 조용히 빗나간다.
                page.locator("#log .row.council").count() shouldBe 2
                page.locator("#log .row.council.v-continue.seated.m-melchior").count() shouldBe 1
                page.locator("#log .row.council.v-done.seated.m-balthasar").count() shouldBe 1
            }
            When("한 자리의 이름을 눌러 그 표를 열면") {
                page.locator("#log .row.council button.whoin").first().click()
                Then("표 자체가 읽힌다 — 결정·렌즈·확신이 한 칩에") {
                    page.waitForSelector("#fileview .dinsp")
                    page.evaluate("window.__magi_test_council") shouldBe "2@-"
                    page.locator("#fileview .filebar .filedir").textContent() shouldBe "Melchior"
                    // tr()이 키로 폴백하는 페이지라 문구가 아니라 고른 키를 잰다.
                    page.locator("#fileview .filebar .dchip").textContent() shouldBe
                            "council.reject · correctness · 90%"
                }
                Then("증거 다음에 표의 넷이 선다 — 이유·다음·유지·근거") {
                    val keys = page.locator("#fileview .dinsp .dk")
                    val said = (0 until keys.count()).joinToString("|") { keys.nth(it).textContent() ?: "" }
                    said shouldContain "detail.rationale|detail.next|detail.keep|detail.grounds"
                    val body = page.locator("#fileview .dinsp").textContent() ?: ""
                    body shouldContain "the report summarises instead of quoting"
                    body shouldContain "paste the exact output"
                    body shouldContain "the build fix already landed"
                    body shouldContain "\"bash ls -la: exit 0\""
                }
                Then("라벨은 카드 안에서도 입혀진다 — 운영의 그 규칙이 id 하나에 걸려 있었다") {
                    // 잰 값이 아니라 <b>다름</b>을 잰다(색·크기는 토큰이라 테마마다 다르다):
                    // 규칙이 빗나가면 라벨은 본문과 글자 크기도 색도 같아진다 — 실측으로 그랬다.
                    val said = page.evaluate("(() => { const k = getComputedStyle(" +
                            "document.querySelector('#fileview .dinsp .dk')), d = getComputedStyle(" +
                            "document.querySelector('#fileview .dinsp .dbody'));" +
                            " return [k.fontSize === d.fontSize, k.color === d.color," +
                            " k.fontFamily.includes('mono')].join(','); })()")
                    withClue("라벨이 본문과 같은 크기·색이면 이 카드에는 제목이 없는 것이다") {
                        said shouldBe "false,false,true"
                    }
                }
                page.evaluate("window.__magi_cards[window.__magi_cards.length - 1].close()")
            }
            When("아무 것도 대지 않은 표를 열면") {
                page.locator("#log .row.council button.whoin").last().click()
                Then("근거 없음을 말로 적는다 — 빈 자리로 두지 않는다") {
                    page.waitForCondition {
                        page.locator("#fileview .filebar .filedir").textContent() == "Balthasar"
                    }
                    page.locator("#fileview .filebar .dchip").textContent() shouldBe "council.accept"
                    (page.locator("#fileview .dinsp").textContent() ?: "") shouldContain "detail.no_grounds"
                }
                page.evaluate("window.__magi_cards[window.__magi_cards.length - 1].close()")
            }
            Then("산문 행에는 적힌 그대로를 복사하는 문이 늘 서 있다 — 손끝이 와야 나오는 것이 아니라") {
                // 골라서 복사하면 그려진 글이 나온다(표는 칸이 붙고 코드 울타리는 사라진다).
                // 사람이 화면에서 달리 얻을 수 없는 것이라 두 산문 행에만 둔다.
                // 산문 행마다 하나씩, 그 행의 홈통에.
                (page.evaluate("[...document.querySelectorAll('#log .row.user, #log .row.assistant')]" +
                    ".every(r => r.querySelectorAll('.who .copy').length === 1)") as Boolean) shouldBe true
                (page.locator("#log .row.user").count() > 0) shouldBe true
                page.locator("#log .row.toolok .copy").count() shouldBe 0
                page.locator("#log .row.user .who .copy").first().getAttribute("aria-label") shouldBe "action.copy"
            }
            Then("시각은 행의 홈통에 붙는다") {
                page.locator("#log .row.user .who .when").count() shouldBe 1
            }
        }
        When("컴포저에 쓰다 말면") {
            page.locator("#dock .composer #t textarea").fill("run the build")
            Then("다음 말을 흐리게 내밀고, Tab이 그것을 이어붙인다") {
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_suggest") as? String) == "run the build"
                }
                page.waitForSelector("#dock .sughint:not([hidden])")
                page.locator("#dock .sughint").textContent() shouldBe "and then some"
                page.locator("#dock .composer #t textarea").press("Tab")
                page.evaluate("document.querySelector('#dock .composer #t').value") shouldBe
                    "run the build and then some"
                page.locator("#dock .sughint:not([hidden])").count() shouldBe 0
            }
            page.locator("#dock .composer #t textarea").fill("")
        }
        // 컴포저의 이어쓰기도 같은 취향을 읽는다 — 설정이 적고 이 화면이 읽는 낱말 하나다.
        // 편집기의 청하기 버튼과 달리 사람이 누를 자리가 없으니, 끈 사람에게 이 도움은 없다.
        When("이어쓰기를 끈 채 컴포저에 쓰다 말면") {
            page.evaluate("window.localStorage.setItem('suggest','off'); window.__magi_test_suggest = null")
            page.locator("#dock .composer #t textarea").fill("build it twice")
            Then("아무것도 묻지 않는다") {
                // 멈춤 뒤 400ms에 묻는 것이라, 그보다 넉넉히 기다린 뒤에 없음을 재야 한다.
                page.waitForTimeout(1000.0)
                page.evaluate("window.__magi_test_suggest") shouldBe null
            }
            page.evaluate("window.localStorage.removeItem('suggest')")
            page.locator("#dock .composer #t textarea").fill("")
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
                page.locator("#agentdetail .dlog .row").count() shouldBe 3
                page.locator("#agentdetail .dlog .row.user .txt").textContent() shouldBe "old prompt"
            }
            Then("머리의 돌아가는 길이 목록을 가리킨다") {
                page.locator("#agentdetail .sectionhead .backpast").count() shouldBe 1
            }
            Then("끝난 일의 표도 열리고 — 근거는 <b>그 세션에</b> 묻는다") {
                // 운영이 되밟은 자리다: 표를 여는 길이 층위를 전부 지우는 바람에 지난 세션의
                // 라운드를 지금 대화에 물었고, 근거는 null로 와서 이름 하나만 남은 화면이
                // 됐다("일이 끝나면 카운슬의 근거에 닿을 수 없다"). 여기서 표는 층위를
                // 갈아타는 것이 아니라 보고 있는 전사 안에서 카드로 서므로 그 세션을 잃을
                // 자리가 없다 — 잃지 않는다는 것을 재는 것은 그래도 이 줄이 해야 한다.
                page.locator("#agentdetail .dlog .row.council button.whoin").click()
                page.waitForSelector("#fileview .dinsp")
                page.evaluate("window.__magi_test_council") shouldBe "5@s_old"
                page.locator("#fileview .filebar .filedir").textContent() shouldBe "Casper"
                page.evaluate("window.__magi_cards[window.__magi_cards.length - 1].close()")
            }
            Then("컴포저는 남는다 — 다만 어느 대화에 닿는지를 이름으로 말한다") {
                // 목록과 다른 점: 보고 있는 그 대화가 곧 말이 갈 곳이다. 컴패니언이 아직 거기
                // 있지 않을 뿐이고, 그건 상자를 치울 이유가 아니라 보내기 전에 묻는 이유다.
                page.waitForCondition {
                    page.locator("#dock #t").getAttribute("label") == "move.into"
                }
                page.locator("#dock form[hidden]").count() shouldBe 0
                page.locator("#dock #cnote").textContent() shouldBe "move.will_ask"
                page.locator("#dock #cnote[hidden]").count() shouldBe 0
                page.locator("#dock #send[disabled]").count() shouldBe 0
            }
        }
        When("옮기기가 거부되면") {
            page.evaluate("window.__magi_test_resume_refuses = 's_old is not a conversation of this workspace'")
            page.locator("#dock .composer #t textarea").fill("carry this on")
            page.locator("#dock .composer #send").click()
            page.waitForSelector("md-dialog.askconfirm")
            Then("그림판이 없는 빌드에서는 낱말만 남는다 — 낱자를 대신 세우지 않는다") {
                // 스프라이트는 구 콘솔이 굽는 것이라 이 페이지에는 없다(라이선스 조건). 그때
                // 운영 withMark는 그냥 돌아선다 — 표 없는 버튼이지 "☰ 취소"가 아니다.
                page.locator("md-dialog.askconfirm md-text-button svg").count() shouldBe 0
                page.locator("md-dialog.askconfirm md-text-button.armed").count() shouldBe 1
            }
            page.locator("md-dialog.askconfirm md-text-button.armed").last().click()
            Then("옮기기만 시도되고 보내기는 따라가지 않는다 — 쓰던 말은 돌아온다") {
                page.waitForCondition { page.evaluate("window.__magi_test_resumed") == "s_old" }
                // 직전 블록이 지금-대화로 보낸 그 한 마디 그대로다: 새로 보낸 것이 없다.
                page.evaluate("window.__magi_test_sent") shouldBe "keep going@/tmp/a1.sock"
                page.waitForCondition {
                    page.locator("#dock .composer #t textarea").inputValue() == "carry this on"
                }
                page.locator("#agentdetail[hidden]").count() shouldBe 0
            }
        }
        When("옮기고 보내기를 누르면") {
            page.evaluate("delete window.__magi_test_resume_refuses; window.__magi_test_resumed = null")
            // 이 페이지에는 그림판이 없다(스프라이트는 구 콘솔이 굽는다) — 표가 어디에 붙는지를
            // 재려면 잴 표가 있어야 하므로, 이 장면 동안만 두 그림을 심는다.
            page.evaluate(
                """
                const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
                s.id = 'isprite';
                s.innerHTML = '<symbol id="i-ss-paper-plane"></symbol><symbol id="i-sl-xmark"></symbol>';
                document.body.prepend(s);
                """
            )
            page.locator("#dock .composer #send").click()
            page.waitForSelector("md-dialog.askconfirm")
            Then("무엇이 어디로 옮겨 가는지 두 이름을 대고 묻는다") {
                val ask = page.locator("md-dialog.askconfirm").last()
                // 말은 감싼 것 없이 슬롯에 바로 온다(운영 #stopBody와 같은 모양) — 묶음을
                // 하나 끼우면 textContent는 그대로라 눈치채지 못하므로, 자식 수로 잰다.
                ask.locator("[slot=content]").textContent() shouldBe "move.body"
                ask.locator("[slot=content] > *").count() shouldBe 0
                ask.locator("md-text-button.armed").textContent().trim() shouldBe "action.move_and_send"
                // 되돌릴 수 없는 쪽은 채운 버튼이 아니라 armed이고, 상자는 alert다(운영 계약).
                ask.locator("md-text-button").count() shouldBe 2
                ask.getAttribute("type") shouldBe "alert"
                // 표는 자식이 아니라 슬롯에 온다 — 자식으로 붙이면 라벨 크기를 물려받아 작아진다.
                ask.locator("md-text-button.armed svg[slot=icon]").count() shouldBe 1
                ask.locator("md-text-button:not(.armed)").textContent().trim() shouldBe "action.cancel"
                ask.locator("md-text-button:not(.armed) svg[slot=icon]").count() shouldBe 1
            }
            page.locator("md-dialog.askconfirm md-text-button.armed").last().click()
            page.evaluate("document.getElementById('isprite').remove()")
            Then("옮기고, 그 다음에 보내고, 읽던 층위를 접는다") {
                page.waitForCondition { page.evaluate("window.__magi_test_resumed") == "s_old" }
                page.waitForCondition {
                    page.evaluate("window.__magi_test_sent") == "carry this on@/tmp/a1.sock"
                }
                page.waitForCondition { page.locator("#agentdetail[hidden]").count() == 1 }
                page.locator("#dock .composer #t textarea").inputValue() shouldBe ""
            }
        }
        When("컴패니언이 아직 말하고 있는데 다른 세션에 서면") {
            page.evaluate("window.__magi_test_fleet('working', 's_now')")
            page.evaluate("window.__magi_test_past('s_old')")
            Then("상자는 받지 않는다 — 배달하지 못할 것을 받는 상자는 두지 않는다") {
                page.waitForCondition {
                    page.locator("#dock #t").getAttribute("label") == "move.busy"
                }
                page.locator("#dock #cnote").textContent() shouldBe "move.busy_why"
                page.locator("#dock #send[disabled]").count() shouldBe 1
                page.locator("#dock #t[disabled]").count() shouldBe 1
            }
            page.evaluate("window.__magi_test_fleet('idle', 's_now')")
        }
        When("지금 대화로 돌아오면") {
            page.evaluate("window.__magi_test_past(null)")
            Then("전사와 컴포저가 돌아온다") {
                page.waitForSelector("#log:not([hidden])")
                page.locator("#agentdetail[hidden]").count() shouldBe 1
                page.locator("#dock form:not([hidden])").count() shouldBe 1
            }
            Then("씌웠던 라벨도 벗겨진다 — 아무도 보지 않는 세션의 이름이 남지 않게") {
                page.waitForCondition {
                    page.locator("#dock #t").getAttribute("label") == "label.ask"
                }
                page.locator("#dock #cnote[hidden]").count() shouldBe 1
                page.locator("#dock #send[disabled]").count() shouldBe 0
            }
        }
        When("자식 하나(?sub=)로 들어가면") {
            // 지난 일과 나란한 또 하나의 층위다 — 둘 다 지금-대화 자리를 대신한다. 다른 것은
            // <b>상자</b>다: 한 세션을 열어 둔 화면은 보고 있는 그 대화가 곧 말이 갈 곳이라
            // 컴포저가 남지만, 자식은 다시 부를 수 없다. 그 갈림이 여기서 재는 것이다.
            page.evaluate("window.__magi_test_sub('s_kid')")
            Then("그 아이의 자리가 서고 — 컴포저는 물러난다(한 세션을 연 화면과 갈리는 자리)") {
                page.waitForSelector("#agentdetail:not([hidden]) .dlog .row")
                page.locator("#log[hidden]").count() shouldBe 1
                page.locator("#dock form[hidden]").count() shouldBe 1
            }
            Then("머리는 그 아이의 <b>역할</b>을 말한다 — 없을 때만 '자식'이라는 낱말이다") {
                page.locator("#agentdetail .sectionhead span").first().textContent() shouldBe "scout"
                page.locator("#agentdetail .sectionhead .backpast").count() shouldBe 1
            }
            Then("도는 중인지와 어느 모델인지 한 줄 — 다시 부를 수 없으니 사실만 적는다") {
                page.locator("#agentdetail .dnote").first().textContent() shouldBe
                    "detail.finished \u00b7 qwen3-coder-next"
            }
            Then("무엇을 하라고 보내졌는지 — 그것이 이 화면의 절반이다") {
                page.locator("#agentdetail .dk").first().textContent() shouldBe "detail.asked"
                page.locator("#agentdetail .dv").first().textContent() shouldBe "find the empty states"
            }
            Then("전사는 <b>그 아이디로</b> 읽고, 지난 일과 같은 행으로 그린다") {
                // 자식의 전사도 전사다 — 이 콘솔에서 전사가 읽히는 방식은 하나여야 한다
                // (운영 drawChild의 그 판단). 그래서 여기서 세는 행은 지난 세션의 그 행이다.
                page.evaluate("window.__magi_test_past_read") shouldBe "s_kid"
                page.locator("#agentdetail .dlog .row").count() shouldBe 3
                page.locator("#agentdetail .dlog .row.user .txt").textContent() shouldBe "old prompt"
            }
            Then("돌아가는 길은 셸에 <b>청한다</b> — 주소를 화면이 직접 쓰지 않는다") {
                // 문이 없으면 GoSharing.sub는 조용히 아무 일도 하지 않는다. 이 페이지가 셸을
                // 대신해 문을 걸고(codingtest.html, __magi_go_past 옆) 청한 값을 적어 둔다 —
                // 셸 쪽 문(주소에 sub가 실리고 null이면 걷힌다)은 ShellDrawerTest가 잰다.
                page.locator("#agentdetail .sectionhead .backpast").click()
                page.waitForSelector("#log:not([hidden])")
                page.evaluate("window.__magi_test_went_sub") shouldBe "null"
                page.locator("#agentdetail[hidden]").count() shouldBe 1
                page.locator("#dock form:not([hidden])").count() shouldBe 1
            }
        }
        When("명단에 없는 아이로 들어가면") {
            // 낡은 링크로만 닿는 자리가 아니다 — 들어가는 문(SideElement의 칩)은 /jobs의
            // children을 보고, 이 화면의 머리는 /subagents를 본다. 뒤엣것은 회의 자식을
            // 빼내므로, 회의가 도는 동안 칩을 누른 사람이 바로 이 화면에 선다.
            page.evaluate("window.__magi_test_sub('s_ghost')")
            page.waitForSelector("#agentdetail:not([hidden]) .sectionhead")
            Then("못 찾았다고 <b>적는다</b> — 줄을 조용히 빼면 이름 없는 자식이 서 있을 뿐이다") {
                page.waitForCondition {
                    page.locator("#agentdetail .dnote").first().textContent() == "detail.not_in_roster"
                }
                page.locator("#agentdetail .sectionhead span").first().textContent() shouldBe "detail.subagent"
            }
        }
        When("그 아이의 전사를 읽지 못하면") {
            // 서버가 거절해도(`not this companion's to read`) 값은 null 하나다 —
            // Console.fetchList가 거부·불통·깨진 본문을 다 그렇게 접는다.
            page.evaluate("window.__magi_test_sub('s_unread')")
            Then("읽지 못했다고 적는다 — 「아직 아무 말도 안 했다」로 바꿔 적지 않는다") {
                page.waitForCondition {
                    page.locator("#agentdetail .dlog .dnote").first().textContent() == "detail.no_transcript"
                }
                page.locator("#agentdetail .dlog .row").count() shouldBe 0
            }
        }
        When("두 읽기가 아직 안 돌아왔으면") {
            page.evaluate("window.__magi_test_sub('s_slow')")
            page.waitForSelector("#agentdetail:not([hidden]) .sectionhead")
            Then("아무 사실도 적지 않는다 — 안 읽은 화면이 읽기가 끝난 것처럼 서면 안 된다") {
                // 자매 함수 paintPast가 null을 아예 그리지 않는 그 규칙이다. 여기서 「없다」를
                // 적으면 첫 프레임마다 <b>완성된 거짓 문장</b>이 한 번씩 선다.
                page.locator("#agentdetail .dnote").count() shouldBe 0
                page.locator("#agentdetail .dlog .row").count() shouldBe 0
                page.locator("#agentdetail .dk.dhero").count() shouldBe 1
            }
            page.evaluate("window.__magi_test_sub_release()")
            Then("돌아오면 그때 사실이 선다") {
                page.waitForCondition { page.locator("#agentdetail .dlog .row").count() == 1 }
                page.locator("#agentdetail .sectionhead span").first().textContent() shouldBe "judge"
                page.locator("#agentdetail .dnote").first().textContent() shouldBe
                    "detail.running \u00b7 qwen3-coder-next"
            }
            page.evaluate("window.__magi_test_sub(null)")
            page.waitForSelector("#log:not([hidden])")
        }
        When("새 일을 줄 능력이 없는 사람이 보면") {
            page.evaluate("window.__magi_may = ['answer']; window.__magi_test_fleet('working', 's_now')")
            Then("상자는 남되 잠기고, 왜 잠겼는지를 그 자리가 적는다") {
                page.waitForCondition {
                    page.locator("#dock #t").getAttribute("label") == "may.not_prompt"
                }
                page.locator("#dock form:not([hidden])").count() shouldBe 1
                page.locator("#dock #t[disabled]").count() shouldBe 1
                page.locator("#dock #send[disabled]").count() shouldBe 1
            }
        }
        When("둘 다 없는 사람이 보면") {
            page.evaluate("window.__magi_may = []; window.__magi_test_fleet('idle', 's_now')")
            Then("상자가 통째로 물러난다 — 누를 데가 하나도 없는 상자는 자리만 차지한다") {
                page.waitForCondition { page.locator("#dock form[hidden]").count() == 1 }
            }
            // 원래대로 — 상자를 <b>다시 세우는</b> 것은 층위의 몫이라(reach는 물리기만 한다)
            // 능력을 지우는 것만으로는 돌아오지 않는다. 뒤 스펙에 선 상자를 넘긴다.
            page.evaluate("delete window.__magi_may; window.__magi_test_past(null)")
            Then("능력이 돌아오면 상자도 돌아온다") {
                page.waitForCondition { page.locator("#dock form:not([hidden])").count() == 1 }
                page.locator("#dock #t[disabled]").count() shouldBe 0
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
            Then("바뀐 파일의 행은 무리를 말로 말한다 — 상태 글자는 git을 아는 사람에게만 말한다") {
                page.locator("#files .pane-git .gitline .gitkind").first().textContent() shouldBe "git.staged"
            }
            Then("한 파일에 하는 일은 메뉴 하나로 — 행마다 버튼을 늘어놓으면 이름이 먼저 잘린다") {
                page.locator("#files .pane-git .gitacts md-icon-button").count() shouldBe 2
                page.locator("#files .pane-git .gitacts md-menu").count() shouldBe 2
                // 잘리는 판 안에 두면 메뉴가 판의 경계에서 잘린다 — 페이지의 상자들 밖으로 나간다.
                (page.locator("#files .pane-git .gitacts md-menu").first()
                    .getAttribute("positioning") in listOf("popover", "fixed")) shouldBe true
            }
            Then("첫 걸음은 뿌리 하나만 읽는다 — 열지도 않은 가지를 걷지 않는다") {
                page.evaluate("window.__magi_test_dirs") shouldBe "."
            }
        }
        When("바뀐 파일의 메뉴에서 무엇이 달라졌는지 물으면") {
            // git 판은 접힌 채로 선다(운영 규칙) — 펼치고 나서야 그 안의 것을 누를 수 있다.
            page.locator("#files .pane-git .panehead").click()
            page.waitForCondition { page.locator("#files .pane-git.shut").count() == 0 }
            // 쉬는 동안 이 행동들은 숨어 있다(운영 CSS) — 손끝을 얹어야 나온다.
            page.locator("#files .pane-git .gitline").first().hover()
            page.locator("#files .pane-git .gitacts md-icon-button").first().click()
            page.locator("#files .pane-git .gitacts md-menu md-menu-item").first().click()
            Then("차이가 제 카드로 열린다 — 본문과 다른 카드다(둘을 함께 열어 두는 일이 잦다)") {
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_diff") as? String) != null
                }
                page.evaluate("window.__magi_test_diff") shouldBe "src/main.go|staged"
                page.waitForSelector("#fileview .diffbody .dl.add")
                page.locator("#fileview .diffbody .dl.cut").count() shouldBe 1
                page.locator("#fileview .filebody.diffscroll .filegutter").count() shouldBe 0
            }
            page.evaluate("window.__magi_cards[window.__magi_cards.length - 1].close()")
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
            Then("타이핑이 멎으면 이어쓰기를 묻고, Tab이 그것을 가져간다") {
                page.locator("#fileview .fileedit .fileeditarea").click()
                page.locator("#fileview .fileedit .fileeditarea").fill("package main\nfunc x() {")
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_complete") as? String)?.contains("func x() {") == true
                }
                // 유령은 버퍼가 아니라 거울에 산다 — 가져가기 전에는 사람이 쓴 글이 아니다.
                page.waitForSelector("#fileview .editghost .editcomplete")
                page.evaluate("document.querySelector('#fileview .fileeditarea').value") shouldBe
                    "package main\nfunc x() {"
                page.locator("#fileview .fileedit .fileeditarea").press("Tab")
                page.waitForCondition {
                    (page.evaluate("document.querySelector('#fileview .fileeditarea').value") as? String)
                        ?.endsWith("MORE") == true
                }
                page.locator("#fileview .editghost .editcomplete").count() shouldBe 0
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
        // 이 편집기의 도움 둘 — 멈출 때마다 <b>스스로</b> 도는가는 이 브라우저의 취향이고(설정
        // 화면이 적는 낱말을 편집기가 읽는다), 사람이 한 번 청하는 것은 취향과 무관하게 열려 있다.
        When("고치기를 다시 누르면") {
            page.evaluate("window.__magi_test_look = null; window.localStorage.removeItem('lookover')")
            page.locator("#fileview .filebar .fileacts md-text-button").first().click()
            page.waitForSelector("#fileview .fileedit .fileeditarea")
            Then("청하는 컨트롤 둘이 제 상태를 이름으로 말한다 — 색만으로 말하지 않는다") {
                val ask = page.locator("#fileview .filebar .fileacts .editask")
                ask.count() shouldBe 2
                // 스스로도 도는 쪽만 armed다. 룩오버는 기본이 꺼짐이라 이름이 갈린다.
                ask.nth(0).getAttribute("class") shouldNotContain "on"
                ask.nth(1).getAttribute("class") shouldContain "on"
                // md-icon-button은 호스트의 aria-*를 제 것으로 삼아 data-aria-*로 옮긴다(실측:
                // aria-label → data-aria-label). 이름은 그림자 안 버튼이 이어받으므로 읽는
                // 기계에는 그대로 닿는다 — 그 사실을 역할·이름으로 따로 잰다. 운영도 같은
                // 컴포넌트라 이 계약은 두 콘솔이 공유한다(page.js:9312).
                ask.nth(0).getAttribute("data-tip") shouldBe "edit.look_now"
                ask.nth(1).getAttribute("data-tip") shouldBe "edit.ask_armed"
                ask.nth(0).getAttribute("data-aria-busy") shouldBe "false"
                page.getByRole(AriaRole.BUTTON, Page.GetByRoleOptions().setName("edit.look_now"))
                    .count() shouldBe 1
            }
            Then("룩오버가 꺼져 있으면 멈춰도 백엔드를 쓰지 않는다 — 그 비용은 고를 일이다") {
                page.locator("#fileview .fileedit .fileeditarea").fill("package main\nfunc y() {")
                page.waitForCondition {
                    (page.evaluate("window.__magi_test_complete") as? String)?.contains("func y() {") == true
                }
                // 스스로 도는 룩오버는 멈춤 2초 뒤다. 그보다 넉넉히 기다린 뒤에 없음을 재야 한다.
                page.waitForTimeout(2600.0)
                page.evaluate("window.__magi_test_look") shouldBe null
            }
            Then("그래도 사람이 청하면 한 번 본다 — 캐럿 둘레만 싣고") {
                page.evaluate("window.__magi_test_look_hold = true")
                page.locator("#fileview .filebar .fileacts .editask").nth(0).click()
                page.waitForCondition { page.evaluate("window.__magi_test_look") != null }
                (page.evaluate("window.__magi_test_look") as String) shouldContain "src/main.go|"
            }
            Then("도는 표는 세어서 켠다 — 먼저 온 답이 아직 뜬 물음의 표까지 끄지 않게") {
                page.locator("#fileview .filebar .fileacts .editask").nth(0).click()
                page.waitForCondition { page.evaluate("window.__magi_test_look_asked") == 2 }
                val look = page.locator("#fileview .filebar .fileacts .editask").nth(0)
                look.getAttribute("data-aria-busy") shouldBe "true"
                look.getAttribute("data-tip") shouldBe "action.working"
                page.evaluate("window.__magi_test_look_release()")
                withClue("깃발이었다면 여기서 표가 꺼진다 — 아직 하나가 떠 있는데도") {
                    look.getAttribute("data-aria-busy") shouldBe "true"
                }
                page.evaluate("window.__magi_test_look_release()")
                page.waitForCondition { look.getAttribute("data-aria-busy") == "false" }
            }
            Then("답은 그 줄 곁에 서고, 형식 밖의 말은 상자에 선다") {
                page.waitForSelector("#fileview .editghost .linenote")
                page.locator("#fileview .editghost .linenote").count() shouldBe 1
                withClue("운영과 같은 표식(‹)이 코드와 남의 말을 가른다 — page.js:8980") {
                    page.locator("#fileview .editghost .linenote").textContent() shouldBe
                        "  \u2039 says nothing"
                }
                page.locator("#fileview .fileedit .looksaid").textContent() shouldBe
                    "and a line outside the format"
                withClue("형식 밖의 말은 결함이라 경고색을 지킨다 — .plain은 할 말이 없다는 쪽이다") {
                    page.locator("#fileview .fileedit .looksaid.plain").count() shouldBe 0
                }
            }
            Then("같은 검토가 귀로도 간다 — 그것을 그리는 거울은 aria-hidden이다") {
                val sr = page.locator("#fileview .fileedit .sr-only[role=status]")
                sr.count() shouldBe 1
                sr.getAttribute("aria-live") shouldBe "polite"
                sr.textContent() shouldBe "1: says nothing \u00B7 and a line outside the format"
                withClue("필드가 그 줄을 가리켜야 언제든 다시 읽힌다") {
                    page.evaluate(
                        "document.querySelector('#fileview .fileeditarea').getAttribute('aria-describedby')" +
                            " === document.querySelector('#fileview .fileedit .sr-only[role=status]').id"
                    ) shouldBe true
                }
            }
            Then("키 두 짝이 그 둘을 부른다 — 버튼이 제 이름에 이미 광고하던 키다") {
                page.evaluate("window.__magi_test_look_hold = false;" +
                    " window.__magi_test_look = null; window.__magi_test_complete = null")
                val area = page.locator("#fileview .fileedit .fileeditarea")
                area.press("Control+Shift+Enter")
                page.waitForCondition { page.evaluate("window.__magi_test_look") != null }
                page.evaluate("window.__magi_test_complete") shouldBe null
                area.press("Control+Enter")
                page.waitForCondition { page.evaluate("window.__magi_test_complete") != null }
            }
        }
        When("룩오버를 켜고 편집기를 다시 세우면") {
            // 취향은 편집기가 <b>설 때</b> 읽는다 — 설정은 다이얼로그가 아니라 화면이라 이 둘은
            // 같은 순간에 서지 않는다(운영이 살아서 고쳐야 했던 이유가 여기엔 없다).
            page.evaluate("window.localStorage.setItem('lookover','on'); window.__magi_test_look = null")
            page.locator("#fileview .filebar .fileacts md-text-button").last().click()
            page.waitForSelector("#fileview .filebody .filecode")
            page.locator("#fileview .filebar .fileacts md-text-button").first().click()
            page.waitForSelector("#fileview .fileedit .fileeditarea")
            Then("멈추면 스스로 한 번 보고, 그 컨트롤은 armed로 선다") {
                page.locator("#fileview .filebar .fileacts .editask").nth(0)
                    .getAttribute("class") shouldContain "on"
                page.locator("#fileview .fileedit .fileeditarea").fill("package main\nfunc z() {")
                page.waitForCondition { page.evaluate("window.__magi_test_look") != null }
            }
            Then("취향은 이 브라우저에 남는다 — 편집기는 읽기만 한다") {
                page.evaluate("window.localStorage.getItem('lookover')") shouldBe "on"
                page.locator("#fileview .filebar .fileacts md-text-button").last().click()
                page.waitForSelector("#fileview .filebody .filecode")
                page.evaluate("window.localStorage.removeItem('lookover')")
            }
        }
        When("찾기를 누르면") {
            page.locator("#files .filefind md-text-button").first().click()
            Then("무엇을 어디서 찾을지 묻는 상자가 뜬다 — 좁은 기둥에 상자를 늘 펼쳐 두지 않는다") {
                page.waitForSelector("md-dialog.askline md-outlined-text-field")
                page.locator("md-dialog.askline .askwhere .wherechip").count() shouldBe 2
            }
            Then("머리글은 이름을 갖고(#askK), 나가는 길 ✕는 머리글이 아니라 내용 슬롯에 선다") {
                // #askK는 좁은 화면 css의 계약이다(✕가 앉을 앞여백). ✕를 머리글에 두면
                // md-dialog가 거기서 제 이름을 가져가 상자가 "닫기 (제목)"으로 제 이름을 댄다.
                page.locator("md-dialog.askline [slot=headline]#askK").count() shouldBe 1
                page.locator("md-dialog.askline > .dlgclose").getAttribute("slot") shouldBe "content"
                page.locator("md-dialog.askline [slot=headline] md-icon-button").count() shouldBe 0
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
        When("셸이 ⌘K에 실을 것을 물으면") {
            // 이 페이지엔 셸이 없다 — 스펙이 셸 노릇을 한다(문은 window의 그 이름 하나다).
            page.evaluate("(() => { window.__magi_pal = null;" +
                " window.__magi_palette_ask('ma', es => { window.__magi_pal = es }); true })()")
            Then("이 워크스페이스의 파일 이름이 온다 — 갈래말·이름, 그리고 곁에 경로") {
                page.waitForCondition { page.evaluate("!!window.__magi_pal") == true }
                page.evaluate("window.__magi_pal.map(e => e.kind + '|' + e.name + '|' + e.hint).join(',')") shouldBe
                    "pal.kind_file|main.go|src/main.go,pal.kind_file|util.go|src/util.go"
                page.evaluate("window.__magi_test_find") shouldBe "name:ma"
            }
            Then("왼쪽 기둥의 찾기는 그대로다 — 물은 사람이 다르다") {
                // 이 판의 query/hits를 썼다면 ⌘K에 두 글자 친 것이 트리를 결과 목록으로 바꿔 놓는다.
                page.locator("#files .pane-files .treerow.dir").count() shouldBe 1
                page.locator("#files .hits .treerow.hit").count() shouldBe 0
            }
            Then("한 글자에는 아예 묻지 않는다 — 상자를 열고 한 글자가 남의 저장소를 걷는 값이 되지 않게") {
                page.evaluate("(() => { window.__magi_test_find = 'none'; window.__magi_pal = null;" +
                    " window.__magi_palette_ask('m', es => { window.__magi_pal = es }); true })()")
                page.waitForCondition { page.evaluate("!!window.__magi_pal") == true }
                val none = page.evaluate("window.__magi_pal.length") as Number
                none.toInt() shouldBe 0
                page.evaluate("window.__magi_test_find") shouldBe "none"
            }
            Then("많이 걸려도 여덟까지다 — 파일 이름 열둘이 갈 곳들을 상자 밖으로 밀지 않게") {
                page.evaluate("(() => { window.__magi_pal = null;" +
                    " window.__magi_palette_ask('lots', es => { window.__magi_pal = es }); true })()")
                page.waitForCondition { page.evaluate("!!window.__magi_pal") == true }
                val many = page.evaluate("window.__magi_pal.length") as Number
                many.toInt() shouldBe 8
            }
            Then("고르면 그 파일로 간다 — 트리의 줄을 누른 것과 같은 부름이다") {
                // 이미 열어 둔 파일이면 다시 읽지 않고 그 탭으로 간다(WorkspaceStore.openFile) —
                // 그래서 재는 것은 "디스크를 다시 물었나"가 아니라 "무엇을 보이라 했나"다
                // (CardSharing.showing: 어느 카드가 보이는지는 부모가 고른다).
                val was = page.evaluate("window.__magi_cards_showing")
                withClue("고르기 전부터 그 파일을 보고 있었다면 이 장면은 아무것도 재지 않는다") {
                    was shouldBe "src/main.go"
                }
                page.evaluate("(() => { window.__magi_palette_ask('ma', es => es[1].run()); true })()")
                page.waitForCondition { page.evaluate("window.__magi_cards_showing") == "src/util.go" }
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
                page.locator("md-dialog.askconfirm [slot=content]").textContent() shouldBe "files.delete_body"
                page.locator("md-dialog.askconfirm md-text-button.armed").click()
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
        When("질문이 서 있는데 답할 능력만 없으면") {
            page.evaluate("window.__magi_may = ['prompt']; window.__magi_test_fleet('working', 's_now')")
            Then("지금 몫인 그 능력으로 거절한다 — 새 일은 줘도 되지만 대신 답하지는 못한다") {
                page.waitForCondition {
                    page.locator("#dock #t").getAttribute("label") == "may.not_answer"
                }
                page.locator("#dock form:not([hidden])").count() shouldBe 1
                page.locator("#dock #t[disabled]").count() shouldBe 1
                page.locator("#dock #send[disabled]").count() shouldBe 1
            }
            page.evaluate("delete window.__magi_may; window.__magi_test_fleet('idle', 's_now')")
            Then("능력이 돌아오면 답하는 자리로 돌아온다") {
                page.waitForCondition {
                    page.locator("#dock #t").getAttribute("label") == "label.answer"
                }
                page.locator("#dock #send[disabled]").count() shouldBe 0
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
        When("기둥이 하나뿐이라고 부모가 말하면(폰)") {
            page.evaluate("window.__magi_one_pane = true")
            page.evaluate("window.dispatchEvent(new Event('resize'))")
            page.locator("#files .paneagain").click()   // 다시 그리게 한다 — 배치 사실이 바뀌었다
            Then("작업공간은 한 번에 하나를 보인다 — 트리와, git으로 가는 줄") {
                page.waitForSelector("#files[data-shows=files] .panelrow")
                // 마흔 개 이름 아래 깔린 판은 아무도 스크롤해 내려가지 않는다(운영이 이 화면에서 배운 것).
                page.locator("#files .panelrow .panelword").textContent() shouldBe "git.section"
            }
            When("그 줄을 누르면") {
                page.locator("#files .panelrow").click()
                Then("git이 그 자리에 서고, 돌아가는 줄이 생긴다") {
                    page.waitForSelector("#files[data-shows=git] .panelback")
                    page.locator("#files .pane-git").count() shouldBe 1
                }
                Then("돌아가면 트리다") {
                    page.locator("#files .panelback md-text-button").click()
                    page.waitForSelector("#files[data-shows=files] .pane-files")
                }
            }
            page.evaluate("window.__magi_one_pane = false")
            page.locator("#files .paneagain").click()
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤이 없고, 전사와 컴포저가 그대로 쓸 만하다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                page.locator("#log .row").first().isVisible() shouldBe true
                // 상자는 서 있다. 폭은 재지 않는다 — 이 페이지에는 언어 팩이 없어 버튼의 낱말이
                // 키 문자열("action.interrupt")이고, 390px에서 그 길이는 실제 낱말의 두 배다.
                // 팩이 있는 화면에서의 폭은 두 콘솔을 나란히 재서 확인한다(scratchpad/uitest).
                page.locator("#dock .composer #t").count() shouldBe 1
                page.locator("#dock .composer #send").count() shouldBe 1
            }
        }
        When("전사가 자라면 — 같은 화면에 두 번째 프레임이 오면") {
            // 전사는 한 번에 오지 않는다. 한 프레임만 그려 보면 그리는 규칙은 다 맞아 보이고,
            // <b>두 번째</b> 프레임에서만 갈리는 것이 셋이다: 안 달라진 행을 그대로 두는가,
            // 없어진 꼬리를 걷는가, 그리고 가운데가 달라진 프레임을 알아보는가.
            page.evaluate("""window.__magi_test_transcript('[{"who":"user","text":"a"},{"who":"tool","tool":"bash","args":"{}","pending":true},{"who":"assistant","text":"c"}]')""")
            page.waitForCondition { page.locator("#log .row").count() == 3 }
            // 지금 서 있는 노드에 표를 찍는다 — 같은 노드인지는 밖에서 이렇게만 보인다.
            page.evaluate("document.querySelectorAll('#log .row').forEach((n,i)=>{n.dataset.mark='m'+i})")
            page.evaluate("""window.__magi_test_transcript('[{"who":"user","text":"a"},{"who":"tool","tool":"bash","args":"{}","ok":true},{"who":"assistant","text":"c"}]')""")
            Then("가운데 툴이 끝난 프레임이 그려진다 — 길이도 같고 마지막 말도 같은 프레임이다") {
                // 길이와 마지막 행만 견주면 이 프레임은 통째로 버려지고, 그 툴 행은 뒤에
                // 다른 행이 붙어 있는 한 <b>영영</b> 도는 중으로 서 있는다.
                page.waitForSelector("#log .row.tool.toolok")
                page.locator("#log .row.pending").count() shouldBe 0
            }
            Then("달라진 그 행만 새로 서고, 같은 말을 하던 행은 노드째 남는다") {
                // 통째로 다시 그리면 펼쳐 둔 툴 결과가 접히고 고르던 글자가 풀린다.
                page.locator("#log .row").nth(0).getAttribute("data-mark") shouldBe "m0"
                page.locator("#log .row").nth(2).getAttribute("data-mark") shouldBe "m2"
                withClue("달라진 행은 새 노드여야 한다 — 표가 남아 있으면 옛 말이 서 있는 것이다") {
                    page.locator("#log .row").nth(1).getAttribute("data-mark") shouldBe null
                }
            }
            page.evaluate("""window.__magi_test_transcript('[{"who":"user","text":"a"}]')""")
            Then("전사가 짧아지면 남은 꼬리는 걷힌다 — 없는 행이 화면에 남지 않게") {
                page.waitForCondition { page.locator("#log .row").count() == 1 }
                page.locator("#log .row").nth(0).getAttribute("data-mark") shouldBe "m0"
            }
        }
        When("컴패니언이 답하기를 멈추면") {
            // 명단에 있다는 것과 답한다는 것은 다른 사실이다(AgentStates.answering).
            page.evaluate("window.__magi_test_fleet('idle', 's_now', false)")
            Then("대화가 있던 자리에 그 사정이 선다 — 빈 곳으로 두지 않는다") {
                page.waitForSelector("#log .empty")
                page.locator("#log .row").count() shouldBe 0
                page.locator("#log .empty").textContent() shouldContain "state.companion_gone"
            }
            Then("컴포저는 그대로다 — 다시 띄우면 이어서 말할 자리다") {
                page.locator("#dock form:not([hidden])").count() shouldBe 1
            }
            page.evaluate("""window.__magi_test_transcript('[{"who":"user","text":"a"},{"who":"assistant","text":"said while gone"}]')""")
            Then("멈춘 자리에 새 말이 끼어들지 않는다 — 사정 밑에 대화가 자라면 둘 다 거짓이 된다") {
                page.locator("#log .empty").count() shouldBe 1
                page.locator("#log .row").count() shouldBe 0
            }
        }
        When("컴패니언이 돌아오면") {
            page.evaluate("window.__magi_test_fleet('idle', 's_now', true)")
            Then("사정은 걷히고, 멈춘 동안 도착한 말까지 선다") {
                // 사정은 <b>제 까닭보다 오래 살면 안 된다</b>. 행은 같은 말을 두 번 흘리지
                // 않으므로, 돌아온 자리에서 다시 그리지 않으면 살아 있는 대화 위에 "멈췄다"가
                // 서 있는다 — 돌아온 컴패니언이 새 말을 할 때까지, 어쩌면 영영.
                page.waitForCondition { page.locator("#log .row").count() == 2 }
                page.locator("#log .empty").count() shouldBe 0
                page.locator("#log .row").nth(1).locator(".txt").textContent() shouldBe "said while gone"
            }
        }
    }
})
