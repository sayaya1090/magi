package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/**
 * 환경설정 — 다이얼로그가 아니라 화면이고, 저장 버튼이 없다(바뀌는 순간 저장한다).
 * 무엇이 어디에 사는지가 이 화면의 계약이다: 브라우저의 것과 데몬이 읽는 것.
 */
@GwtHtml("prefstest.html")
internal class SettingsScreenTest : GwtTestSpec({
    Given("환경설정 화면") {
        When("그려지면") {
            Then("어느 파일을 고치는지 맨 위에 적는다 — 다음에 묻는 질문이라서") {
                page.waitForSelector("#settings #settingsScope")
                page.locator("#settings #settingsScopeK").textContent() shouldBe "settings.scope_global"
                page.locator("#settings #settingsScopeFile").textContent() shouldContain "settings.scope_file"
            }
            Then("무리마다 머리가 서고, 저장 버튼은 없다") {
                // 화면·알림·모델 보조·완성 설정·모델 프로파일·이 콘솔 — 운영의 그 여섯.
                page.locator("#settings .prefgroup").count() shouldBe 6
                page.locator("#settings #grpAppearance").textContent() shouldBe "pref.grp.appearance"
                page.locator("#settings md-filled-button").count() shouldBe 0
            }
        }
        When("알림 줄을 보면") {
            Then("켤 수 없는 자리에서는 왜인지 말한다 — 흐린 스위치만으로는 알 수 없다") {
                page.waitForSelector("#settings #notifySwitch")
                // 헤드리스 브라우저는 http로 뜨므로 보안 컨텍스트가 아니다(그것이 첫 이유다).
                val why = page.locator("#settings #notifyWhy").textContent()
                (why == "notify.insecure" || why == "notify.unsupported" ||
                    why == "notify.denied" || why == "notify.how") shouldBe true
                // 이유가 있으면 스위치는 눌리지 않는다.
                if (why != "notify.how") {
                    page.locator("#settings #notifySwitch[disabled]").count() shouldBe 1
                }
            }
        }
        When("테마를 누르면") {
            Then("셋을 돈다 — 기계를 따르는 자리로 돌아올 수 있어야 해서") {
                page.locator("#settings #themeToggle").click()
                page.waitForCondition {
                    page.evaluate("document.documentElement.getAttribute('color-theme')") == "light"
                }
                page.locator("#settings #themeToggle").click()
                page.waitForCondition {
                    page.evaluate("document.documentElement.getAttribute('color-theme')") == "dark"
                }
                page.locator("#settings #themeToggle").click()
                // 기계를 따름은 "적지 않는 것"이다 — 속성이 있으면 매체 질의가 답할 수 없다.
                page.waitForCondition {
                    page.evaluate("document.documentElement.hasAttribute('color-theme')") == false
                }
                page.evaluate("window.localStorage.getItem('theme')") shouldBe "system"
            }
        }
        When("모델 보조 스위치를 끄면") {
            page.locator("#settings md-switch[data-pref=lookover]").click()
            Then("이 브라우저에 남는다 — 다른 기계의 취향까지 바꾸지 않는다") {
                page.waitForCondition {
                    page.evaluate("window.localStorage.getItem('lookover')") == "off"
                }
                // 그리고 데몬에는 아무것도 가지 않았다.
                (page.evaluate("window.__magi_test_saved || ''")) shouldBe ""
            }
        }
        When("데몬이 읽는 칸을 바꾸면") {
            page.locator("#settings md-switch[data-field=crossSession]").click()
            Then("그 config 파일로 간다 — 전역이면 소켓 없이 tier로") {
                page.waitForCondition { page.evaluate("window.__magi_test_saved") != null }
                page.evaluate("window.__magi_test_saved") shouldBe "global|crossSession=on"
            }
            Then("프로파일은 고를 수 있는 것만 — 없음(끔)도 하나의 답이다") {
                page.locator("#settings md-outlined-select[data-field=codeProfile] md-select-option")
                    .count() shouldBe 3
                (page.evaluate("document.querySelector('#settings md-outlined-select[data-field=codeProfile]').value"))
                    .shouldBe("fast-local")
            }
        }
        When("모델 프로파일을 보면") {
            Then("무엇을 고르는 것인지 목록으로 말한다 — 위의 완성 설정이 고르는 그 백엔드다") {
                page.waitForSelector("#settings #profList .profrow")
                page.locator("#settings #profList .profrow .profnm").first().textContent() shouldBe "fast-local"
                page.locator("#settings .profadd #profName").count() shouldBe 1
            }
            Then("이름 없이 저장하지 않는다 — 이름이 그 프로파일의 주소다") {
                page.locator("#settings #profSave").click()
                page.locator("#settings #profWhy").textContent() shouldBe "prof.need_name"
            }
            Then("적어 저장하면 그대로 간다") {
                page.locator("#settings #profName input, #settings #profName textarea").first().fill("cloud-mini")
                page.locator("#settings #profModel input, #settings #profModel textarea").first().fill("gpt-oss:20b-cloud")
                page.locator("#settings #profSave").click()
                page.waitForCondition { page.evaluate("window.__magi_test_profile") != null }
                page.evaluate("window.__magi_test_profile") shouldBe "save|cloud-mini|gpt-oss:20b-cloud"
            }
        }
        When("컴패니언을 보는 중이면(주소의 ?d=)") {
            page.evaluate("history.replaceState(null,'','?v=settings&d=%2Ftmp%2Fa1.sock')")
            page.reload()
            Then("그 컴패니언의 것이라고 말한다 — 같은 컨트롤이 다른 파일을 고친다") {
                page.waitForSelector("#settings #settingsScopeK")
                page.locator("#settings #settingsScopeK").textContent() shouldBe "settings.scope_project"
            }
            Then("저장도 그 컴패니언으로 간다") {
                page.locator("#settings md-switch[data-field=ambient]").click()
                page.waitForCondition { page.evaluate("window.__magi_test_saved") != null }
                page.evaluate("window.__magi_test_saved") shouldBe "/tmp/a1.sock|ambient=off"
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
