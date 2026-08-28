package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/**
 * 지식 화면 관통 — 가짜 포트의 고정 목록 셋으로 그린 DOM을 검사한다.
 * 언어 팩은 못 읽으므로(키 폴백) 구조와 키를 잰다.
 */
@GwtHtml("skillstest.html")
internal class KnowledgeScreenTest : GwtTestSpec({
    Given("규칙 둘·기억 하나, 위키 둘(하나는 낡음), 서버 둘") {
        When("화면이 그려지면") {
            Then("세 판이 프레임의 직계로 서고(운영 구조), 경험은 규칙과 기억으로 갈린다") {
                page.waitForSelector("#skills .sk")
                page.locator("#frame > #skills").count() shouldBe 1
                page.locator("#frame > #wiki").count() shouldBe 1
                page.locator("#frame > #mcp").count() shouldBe 1
                // 행은 판의 직계다 — 목록 래퍼가 끼면 운영 CSS와 어긋난다(rect 대조로 실측).
                page.locator("#skills > .sk").count() shouldBe 3
                page.locator("#skills h3.sectionhead").count() shouldBe 2
                page.locator("#skills .sk.fact").count() shouldBe 1
            }
            Then("행의 메타가 정착도(seen)와 기간, 출처를 말한다") {
                page.locator("#skills .sk").first().locator(".meta").textContent() shouldContain "seen 4×"
                page.locator("#skills .sk").first().locator(".meta").textContent() shouldContain "2026-08-01 → 2026-08-25"
            }
            Then("위키의 낡은 페이지는 ⚠ 묘비로, 편집자·날짜가 메타에 선다") {
                page.locator("#wiki .sk").count() shouldBe 2
                page.locator("#wiki .sk.fact .what").textContent() shouldContain "⚠"
                page.locator("#wiki .sk").first().locator(".meta").textContent() shouldContain "2026-08-20"
            }
            Then("서버 행은 무엇이 실제로 도는지(.how)와 어디 적혀 있는지(.where)를 말한다") {
                page.locator("#mcp .srv").count() shouldBe 2
                page.locator("#mcp .srv .how").first().textContent() shouldContain "https://api.example.com"
                page.locator("#mcp .srv .how").last().textContent() shouldBe "rg-mcp --root ."
                page.locator("#mcp .srv .where").last().textContent() shouldContain "needs RG_TOKEN"
            }
            Then("서버 머리에 추가 액션이 살고(목록을 지나 스크롤하지 않는다), 적어두기 아래 임베딩 줄이 선다") {
                page.locator("#mcp .sectionhead .mcpopen").count() shouldBe 1
                page.locator(".skwrite .skmodel").count() shouldBe 1
            }
        }
        When("규칙의 본문을 읽으면") {
            page.locator("#skills .sk .fold").first().click()
            Then("본문이 펼쳐지고 출처 줄은 제 자리(메타)에만 있다") {
                page.locator("#skills .sk .body:not([hidden])").count() shouldBe 1
                page.locator("#skills .sk .body").first().textContent() shouldContain "byte-identical"
                page.locator("#skills .sk .body").first().textContent().contains("source:") shouldBe false
            }
        }
        When("찾기에 cache를 치면") {
            page.locator("#skills .skfind md-outlined-text-field textarea, #skills .skfind md-outlined-text-field input").first().fill("cache")
            Then("드문 단어 랭킹이 좁히고, 수가 상자 아래 선다") {
                page.waitForCondition { page.locator("#skills .sk").count() == 1 }
                // 팩 없는 페이지의 폴백은 키다 — 단수 키가 골라졌다는 것이 "1건"의 구조 계약.
                page.locator("#skills .filesnote").textContent() shouldBe "find.result"
                page.locator("#skills .sk .what").textContent() shouldContain "cache"
            }
            page.locator("#skills .skfind md-outlined-text-field textarea, #skills .skfind md-outlined-text-field input").first().fill("")
            page.waitForCondition { page.locator("#skills .sk").count() == 3 }
        }
        When("아무것도 걸리지 않는 말을 치면") {
            // 셸의 다른 귓구멍 — 이쪽은 눈에 보이지 않고 들리기만 하는 줄(#say)이다.
            // 마지막 값이 함수면 플레이라이트가 그것을 부른다 — 귓구멍이 빈 인자로 한 번 울려
            // 배열 머리에 null이 앉는다(실측). 값 하나로 닫아 그 부름을 막는다.
            page.evaluate("window.__magi_test_said = []; window.__magi_say = t => { window.__magi_test_said.push(t) }; true")
            page.locator("#skills .skfind md-outlined-text-field textarea, #skills .skfind md-outlined-text-field input").first().fill("zzzzz-nothing-here")
            Then("0건이야말로 소리로 간다 — 목록이 줄어드는 것은 안 보이는 사람에게 아무 소리도 아니다") {
                page.waitForCondition { page.locator("#skills .sk").count() == 0 }
                page.waitForCondition { page.evaluate("window.__magi_test_said.length") as Number != 0 }
                page.evaluate("window.__magi_test_said.at(-1)") shouldBe "find.results"
            }
            Then("다른 말을 쳐서 또 0건이어도 다시 말한다 — 수가 같다고 물음이 같은 것은 아니다") {
                page.locator("#skills .skfind md-outlined-text-field textarea, #skills .skfind md-outlined-text-field input").first().fill("qqqqq-also-nothing")
                page.waitForCondition { (page.evaluate("window.__magi_test_said.length") as Number).toInt() == 2 }
            }
            Then("비우면 몇 건인지는 더 말하지 않는다 — 쉬는 판이 할 말은 그것이 아니다") {
                page.locator("#skills .skfind md-outlined-text-field textarea, #skills .skfind md-outlined-text-field input").first().fill("")
                page.waitForCondition { page.locator("#skills .sk").count() == 3 }
                val counts = "window.__magi_test_said.filter(t => t.indexOf('find.') === 0).length"
                (page.evaluate(counts) as Number).toInt() shouldBe 2
            }
            Then("대신 화면 전체의 요약이 나간다 — 상자를 비운 그 순간 화면은 다시 전체가 된다") {
                page.waitForCondition {
                    page.evaluate("window.__magi_test_said.at(-1)") ==
                            "count.rules \u00b7 count.remembered_one \u00b7 count.crossing \u00b7 count.pages \u00b7 count.servers"
                }
            }
        }
        When("잊기를 두 번 눌러 확인하면") {
            page.locator("#skills .sk .drop").first().click()
            Then("먼저 확인으로 무장한다") {
                page.locator("#skills .sk .drop.armed").count() shouldBe 1
            }
            page.locator("#skills .sk .drop.armed").click()
            Then("그 규칙이 잊히고 목록이 다시 읽힌다") {
                page.waitForCondition { page.evaluate("window.__magi_test_forgot") != null }
                page.evaluate("window.__magi_test_forgot") shouldBe "rule-cache@global"
                page.locator("#skills .sk").count() shouldBe 2
            }
            // 이 화면은 보지 않는 사람에게 세 판이 아니라 한 문장이다 — 운영 sayShared의 자리.
            Then("요약이 다시 나간다 — 규칙이 하나 남아 단수 키로, 위키·서버 수는 뒤에 그대로 붙는다") {
                page.waitForCondition {
                    page.evaluate("window.__magi_test_said.at(-1)") ==
                            "count.rule \u00b7 count.remembered_one \u00b7 count.crossing \u00b7 count.pages \u00b7 count.servers"
                }
            }
        }
        When("한 줄 적어 두면") {
            page.locator("#skills .skwrite md-outlined-text-field textarea").fill("always run gofmt")
            page.locator("#skills #skSave").click()
            Then("global로 기록되고 상자는 빈다") {
                page.waitForCondition { page.evaluate("window.__magi_test_remembered") != null }
                page.evaluate("window.__magi_test_remembered") shouldBe "always run gofmt@global"
            }
            Then("목록이 그대로면 요약은 다시 나가지 않는다 — 걸음마다 같은 문장이면 그 줄은 못 쓰게 된다") {
                val same = "window.__magi_test_said.filter(t => t.indexOf('count.rule \u00b7') === 0).length"
                (page.evaluate(same) as Number).toInt() shouldBe 1
            }
        }
        When("서버 추가를 눌러 채우고 저장하면") {
            page.locator("#mcp .sectionhead .mcpopen").click()
            Then("다이얼로그가 열리고 HTTP 쪽 필드만 보인다") {
                page.waitForSelector("#mcpDialog[open]")
                page.locator("#mcpForm md-outlined-text-field[name=url]:not([hidden])").count() shouldBe 1
                page.locator("#mcpForm md-outlined-text-field[name=command][hidden]").count() shouldBe 1
            }
            page.locator("#mcpForm md-outlined-text-field[name=url] input, #mcpForm md-outlined-text-field[name=url] textarea").first().fill("https://mcp.example.dev/")
            page.locator("#mcpForm md-outlined-text-field[name=name] input, #mcpForm md-outlined-text-field[name=name] textarea").first().fill("example")
            page.locator("#mcpDialog [slot=actions] md-text-button[value=add]").click()
            Then("저장이 기록되고 다이얼로그가 닫힌다") {
                page.waitForCondition { page.evaluate("window.__magi_test_saved") != null }
                page.evaluate("window.__magi_test_saved") shouldBe "example@global"
                // 닫힘은 컴포넌트 애니메이션 뒤에 온다 — 기다려 잰다.
                page.waitForCondition { page.locator("#mcpDialog[open]").count() == 0 }
            }
        }
        // 이 상자는 지워지지 않고 <b>다시 쓰인다</b> — 열 때마다 ✕가 하나씩 늘면 안 된다.
        When("같은 상자를 다시 열어 ✕를 재면") {
            page.locator("#mcp .sectionhead .mcpopen").click()
            page.waitForSelector("#mcpDialog[open]")
            Then("✕는 여전히 하나고, 내용 슬롯에 있다") {
                page.locator("#mcpDialog > .dlgclose").count() shouldBe 1
                page.locator("#mcpDialog > .dlgclose").getAttribute("slot") shouldBe "content"
                page.locator("#mcpDialog [slot=headline] md-icon-button").count() shouldBe 0
            }
            Then("누르면 아무것도 저장하지 않고 걷힌다 — 취소가 하는 그 일이다") {
                // 넓은 창에서 이 표는 display:none이라(그 자리는 셸 스펙이 폰 폭에서 잰다) 여기서
                // 재는 것은 픽셀이 아니라 <b>손잡이가 무엇에 물렸는가</b>다 — 그래서 이벤트로 친다.
                page.evaluate("window.__magi_test_saved = null")
                page.locator("#mcpDialog .dlgclose").dispatchEvent("click")
                page.waitForCondition { page.locator("#mcpDialog[open]").count() == 0 }
                page.evaluate("window.__magi_test_saved") shouldBe null
            }
        }
        // ── 거절이 가는 자리 (운영 page.js:5556-5580) ──────────────────────────
        When("서버가 이름 대는 칸을 짚어 거절하면") {
            page.evaluate("window.__magi_test_refuse = 'a server needs a name, and the name can only contain letters, numbers, hyphens and underscores'")
            page.locator("#mcp .sectionhead .mcpopen").click()
            page.waitForSelector("#mcpDialog[open]")
            page.locator("#mcpForm md-outlined-text-field[name=url] input, #mcpForm md-outlined-text-field[name=url] textarea").first().fill("https://mcp.example.dev/")
            page.locator("#mcpForm md-outlined-text-field[name=name] input, #mcpForm md-outlined-text-field[name=name] textarea").first().fill("bad name")
            page.locator("#mcpDialog [slot=actions] md-text-button[value=add]").click()
            Then("그 칸의 라벨이 사유를 인다 — 상자는 열린 채다") {
                page.waitForSelector("#mcpForm md-outlined-text-field[name=name][error]")
                page.locator("#mcpForm md-outlined-text-field[name=name]").getAttribute("error-text") shouldContain "can only contain letters"
                page.locator("#mcpForm md-outlined-text-field[name=url][error]").count() shouldBe 0
                page.locator("#mcpDialog[open]").count() shouldBe 1
            }
            Then("다시 열면 지난번의 빨간 줄은 없다 — 아직 아무것도 보내지 않았다") {
                page.locator("#mcpDialog [slot=actions] md-text-button[value=cancel]").click()
                page.waitForCondition { page.locator("#mcpDialog[open]").count() == 0 }
                page.locator("#mcp .sectionhead .mcpopen").click()
                page.waitForSelector("#mcpDialog[open]")
                page.locator("#mcpForm md-outlined-text-field[error]").count() shouldBe 0
                page.locator("#mcpDialog [slot=actions] md-text-button[value=cancel]").click()
                page.waitForCondition { page.locator("#mcpDialog[open]").count() == 0 }
            }
        }
        When("stdio 서버를 고치다가 주소·명령 둘 다 부르는 거절을 받으면") {
            page.evaluate("window.__magi_test_refuse = 'a server is either a url (HTTP) or a command (stdio) — this is neither'")
            page.locator("#mcp .srv").last().locator(".srvedit").click()
            page.waitForSelector("#mcpDialog[open]")
            // waitForSelector는 기본이 "보일 때까지"다 — 접힌 칸은 영영 보이지 않아 그대로
            // 쓰면 타임아웃으로 죽는다(실측). 여기서 기다리는 것은 가시성이 아니라 접힘이다.
            page.waitForCondition { page.locator("#mcpForm md-outlined-text-field[name=url][hidden]").count() == 1 }
            page.locator("#mcpDialog [slot=actions] md-text-button[value=add]").click()
            Then("빨간 줄은 접힌 주소 칸이 아니라 서 있는 명령 칸에 선다") {
                // 이름 순서로만 고르면 url이 먼저 걸리는데 그 칸은 지금 0×0이다(운영 페이지에서
                // 운영의 두 갈래를 그대로 돌려 실측: picked=url, w=0 h=0 display:none).
                page.waitForSelector("#mcpForm md-outlined-text-field[name=command][error]")
                page.locator("#mcpForm md-outlined-text-field[name=url][error]").count() shouldBe 0
                page.locator("#mcpForm md-outlined-text-field[name=command]").getAttribute("error-text") shouldContain "either a url"
            }
        }
        When("어느 칸의 것도 아닌 이유로 거절하면(여럿이 닿는 콘솔의 403이 그렇다)") {
            // 셸의 귓구멍을 세운다 — 이 페이지에는 마스트헤드가 없고, 화면 모듈은 제 판 밖에
            // 적지 않는다. 재는 것은 픽셀이 아니라 <b>화면이 셸에 청했는가</b>다.
            page.evaluate("window.__magi_test_note = null; window.__magi_says = t => { window.__magi_test_note = t }; true")
            page.evaluate("window.__magi_test_refuse = 'changing which MCP servers this machine runs is off on this console: it has an auth.toml, so more than one person reaches it. Do it from a terminal on that machine.'")
            page.locator("#mcpDialog [slot=actions] md-text-button[value=add]").click()
            Then("사유가 셸의 상태줄로 간다 — 아무 칸도 붉지 않고, 상자는 열린 채다") {
                page.waitForCondition { page.evaluate("window.__magi_test_note") != null }
                (page.evaluate("window.__magi_test_note") as String) shouldContain "off on this console"
                // 잘려도 읽을 길이 있어야 해서 80자에서 끊는다(운영과 같은 길이).
                (page.evaluate("window.__magi_test_note.length") as Number).toInt() shouldBe 80
                page.locator("#mcpForm md-outlined-text-field[error]").count() shouldBe 0
                page.locator("#mcpDialog[open]").count() shouldBe 1
            }
            Then("치우고 나온다") {
                page.evaluate("window.__magi_test_refuse = null")
                page.locator("#mcpDialog [slot=actions] md-text-button[value=cancel]").click()
                page.waitForCondition { page.locator("#mcpDialog[open]").count() == 0 }
            }
        }
        When("서버 제거를 확인까지 누르면") {
            page.locator("#mcp .srv .drop").first().click()
            page.locator("#mcp .srv .drop.armed").click()
            Then("이름과 자리가 기록되고 행이 빠진다") {
                page.waitForCondition { page.evaluate("window.__magi_test_removed") != null }
                page.evaluate("window.__magi_test_removed") shouldBe "github@global"
                page.locator("#mcp .srv").count() shouldBe 1
            }
            Then("요약의 서버 수도 함께 줄어 단수가 된다 — 세 판의 수가 한 문장에 모인다") {
                page.waitForCondition {
                    page.evaluate("window.__magi_test_said.at(-1)") ==
                            "count.rule \u00b7 count.remembered_one \u00b7 count.crossing \u00b7 count.pages \u00b7 count.server"
                }
            }
        }
        When("잊기가 거절당하면") {
            page.evaluate("window.__magi_test_press_refuses = 'that rule is pinned by the workspace'")
            page.locator("#skills .sk .drop").first().click()
            page.locator("#skills .sk .drop.armed").click()
            Then("서버가 한 말이 경험 판 위에 서고, 지워지지 않은 줄은 그대로다") {
                page.waitForSelector("#skills .refused")
                page.locator("#skills .refused").textContent() shouldBe "that rule is pinned by the workspace"
                page.locator("#skills .sk").count() shouldBe 2
                page.locator("#skills .refused").getAttribute("role") shouldBe "alert"
            }
            Then("다른 판은 조용하다 — 사유는 그 판의 것이다") {
                // 이 화면은 판이 셋이라, 사유를 한 자리에 모으면 서버를 지우려다 들은 말이
                // 규칙 목록 위에 서게 된다.
                page.locator("#mcp .refused").count() shouldBe 0
            }
        }
        When("이번엔 서버 제거가 거절당하면") {
            page.locator("#mcp .srv .drop").first().click()
            page.locator("#mcp .srv .drop.armed").click()
            Then("사유는 서버 판 위에 서고, 그 줄도 그대로다") {
                page.waitForSelector("#mcp .refused")
                page.locator("#mcp .refused").textContent() shouldBe "that rule is pinned by the workspace"
                page.locator("#mcp .srv").count() shouldBe 1
            }
        }
        When("경험 판에서 다음에 누른 것이 받아들여지면") {
            page.evaluate("delete window.__magi_test_press_refuses")
            page.locator("#skills .sk .drop").first().click()
            page.locator("#skills .sk .drop.armed").click()
            Then("그 판의 말만 사라진다 — 서버 판은 아직 청한 대로가 아니다") {
                page.waitForCondition { page.locator("#skills .refused").count() == 0 }
                page.locator("#skills .sk").count() shouldBe 1
                page.locator("#mcp .refused").count() shouldBe 1
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("고르개가 서고 셋 중 하나만 보인다 — 폰에서 세 목적을 한 열에 세우지 않는다") {
                page.waitForSelector("#sharedTabs:not([hidden])")
                page.locator("#sharedTabs md-secondary-tab").count() shouldBe 3
                page.locator("#skills:not([hidden])").count() shouldBe 1
                page.locator("#wiki[hidden]").count() shouldBe 1
                page.locator("#mcp[hidden]").count() shouldBe 1
                // 탭 하나가 아니라 한 벌이라고 말한다 — 화살표에 답하는 쪽이 그렇게 말한다.
                (page.locator("#sharedTabs").getAttribute("role")) shouldBe "tablist"
            }
            Then("고르개를 누르면 그 판으로 바뀐다") {
                page.locator("#sharedTabs md-secondary-tab").nth(1).click()
                page.waitForSelector("#wiki:not([hidden])")
                page.locator("#skills[hidden]").count() shouldBe 1
                page.locator("#sharedTabs md-secondary-tab").nth(2).click()
                page.waitForSelector("#mcp:not([hidden])")
            }

            Then("가로 스크롤이 없고, 고른 판이 그대로 읽힌다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                // 지금 고른 것은 서버 판이다(앞 장면의 마지막 누름) — 보이는 것은 그 하나다.
                page.locator("#mcp .srv").first().isVisible() shouldBe true
                page.locator("#skills .sk").first().isVisible() shouldBe false
            }
            Then("다시 넓히면 고르개가 걷히고 셋이 한 열에 선다") {
                page.setViewportSize(1400, 900)
                page.waitForCondition { page.locator("#sharedTabs[hidden]").count() == 1 }
                page.locator("#skills:not([hidden])").count() shouldBe 1
                page.locator("#wiki:not([hidden])").count() shouldBe 1
                page.locator("#mcp:not([hidden])").count() shouldBe 1
            }
        }
        // 끊긴 회선은 <b>맨 끝</b>에 잰다: 이 화면에는 다시 읽는 문이 쓰기의 답 하나뿐이라,
        // 목록을 못 읽은 뒤에는 누를 행조차 없어 스스로 돌아오지 못한다(새로고침 말고는).
        // 앞 장면들이 그 상태를 물려받지 않도록 자리를 여기로 뒀다.
        When("회선이 끊긴 채로 눌러 거절당하면") {
            // 우리가 지어낸 말(`error.unreachable`)이 서는 경우가 곧 <b>목록도 못 읽는</b>
            // 경우다: 쓰기가 못 닿았으면 뒤따르는 읽기도 못 닿는다. 그래서 이 둘은 겹쳐서
            // 재야 한다 — 겹치지 않게 재면 「말할 것이 우리 것뿐인 때에만 말하지 않는」
            // 화면을 초록으로 통과시킨다.
            page.evaluate("window.__magi_test_unreachable = true")
            page.evaluate("window.__magi_test_press_refuses = 'error.unreachable'")
            page.locator("#skills .sk .drop").first().click()
            page.locator("#skills .sk .drop.armed").click()
            Then("목록을 못 읽어도 사유는 선다 — 그때가 이 줄이 가장 필요한 때다") {
                page.waitForCondition { page.locator("#skills .refused").count() == 1 }
                page.locator("#skills .refused").textContent() shouldBe "error.unreachable"
                // 목록은 못 읽었으니 행이 없다 — 사유는 목록의 자식이 아니라 판의 것이다.
                page.locator("#skills .sk").count() shouldBe 0
                page.evaluate("delete window.__magi_test_unreachable")
                page.evaluate("delete window.__magi_test_press_refuses")
            }
        }
    }
})
