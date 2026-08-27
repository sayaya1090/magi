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
        }
        When("한 줄 적어 두면") {
            page.locator("#skills .skwrite md-outlined-text-field textarea").fill("always run gofmt")
            page.locator("#skills #skSave").click()
            Then("global로 기록되고 상자는 빈다") {
                page.waitForCondition { page.evaluate("window.__magi_test_remembered") != null }
                page.evaluate("window.__magi_test_remembered") shouldBe "always run gofmt@global"
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
        When("서버 제거를 확인까지 누르면") {
            page.locator("#mcp .srv .drop").first().click()
            page.locator("#mcp .srv .drop.armed").click()
            Then("이름과 자리가 기록되고 행이 빠진다") {
                page.waitForCondition { page.evaluate("window.__magi_test_removed") != null }
                page.evaluate("window.__magi_test_removed") shouldBe "github@global"
                page.locator("#mcp .srv").count() shouldBe 1
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤이 없고 세 판이 그대로 읽힌다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                page.locator("#skills .sk").first().isVisible() shouldBe true
                page.locator("#mcp .srv").first().isVisible() shouldBe true
            }
        }
    }
})
