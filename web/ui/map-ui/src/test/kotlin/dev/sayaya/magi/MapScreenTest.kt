package dev.sayaya.magi

import dev.sayaya.gwt.test.GwtHtml
import dev.sayaya.gwt.test.GwtTestSpec
import io.kotest.matchers.ints.shouldBeGreaterThanOrEqual
import io.kotest.matchers.shouldBe
import io.kotest.matchers.string.shouldContain

/** 맵 관통 — 두 경계의 상자, 팀 머리, 노드 어휘, 그리고 측정된 와이어. */
@GwtHtml("maptest.html")
internal class MapScreenTest : GwtTestSpec({
    Given("여기(you@mac, core 둘)와 침묵한 buildbox(들인 것), 오간 것 둘") {
        When("화면이 그려지면") {
            Then("머신 상자 둘 — 내 것이 먼저, 침묵은 상자 위의 나쁜 소식으로") {
                page.waitForSelector("#map .machine")
                page.locator("#map .machine").count() shouldBe 2
                page.locator("#map .machine .machinename").first().textContent() shouldBe "mac"
                page.locator("#map .machine .placeseen.down").count() shouldBe 1
            }
            Then("계정 상자가 신뢰의 말을 달고, 팀은 머리로 선다") {
                page.locator("#map .place.own .placetrust").textContent() shouldBe "map.trust_own"
                page.locator("#map .teamlabel").first().textContent() shouldBe "core"
            }
            Then("노드는 같은 다섯 상태의 같은 말 — 딴 머신 것은 링크가 아니다") {
                page.locator("#map .node").count() shouldBe 3
                page.locator("#map a.node").count() shouldBe 2
                page.locator("#map div.node.faroff").count() shouldBe 1
                page.locator("#map .node .nodehub").count() shouldBe 1
            }
            Then("와이어 둘 — 도는 중 하나, 닿을 수 없음 하나. 범례가 셋을 말한다") {
                page.waitForCondition { page.locator("#map .wires path").count() >= 2 }
                page.locator("#map .wires path.flight").count() shouldBe 1
                page.locator("#map .wires path.down").count() shouldBe 1
                page.locator("#map .maplegend .mapkey").count() shouldBe 3
            }
            Then("표로 돌아가는 길이 머리에 산다") {
                page.locator("#map .sectionhead .astable").count() shouldBe 1
            }
        }
        When("서 있는 노드의 상태만 달라지면") {
            page.waitForSelector("#map .node[data-sock='/a']")
            // 판이 다시 섰는지를 묻는 유일한 방법: 그 순간 서 있던 바로 그 요소에 표를 하나
            // 꽂아 두고, 새 프레임 뒤에도 그 표가 붙어 있는지 본다. 다시 세우면 표는 버려진다.
            page.evaluate(
                """var n = document.querySelector('#map .node[data-sock="/a"]');
                   n.__magi_same = 'kept'; n.focus();
                   window.__magi_test_map_tick('/a', 'idle');"""
            )
            Then("그 노드는 그대로 서 있고 — 포커스도, 그 자리도") {
                page.waitForCondition {
                    page.evaluate("""document.querySelector('#map .node[data-sock="/a"]').className""") ==
                        "node state idle"
                }
                page.evaluate("""document.querySelector('#map .node[data-sock="/a"]').__magi_same""") shouldBe "kept"
                page.evaluate("""document.activeElement.getAttribute('data-sock')""") shouldBe "/a"
            }
            Then("입은 것만 갈아입는다 — 말도 그림도") {
                page.locator("#map .node[data-sock='/a'] .nodestate").textContent() shouldBe "idle"
                page.evaluate("""document.querySelector('#map .node[data-sock="/a"]').getAttribute('data-mark')""") shouldBe
                    "#i-ss-moon"
            }
            Then("옆 노드는 건드리지 않는다") {
                page.locator("#map .node").count() shouldBe 3
                page.locator("#map .node[data-sock='/b'] .nodestate").textContent() shouldBe "idle"
                page.locator("#map div.node.faroff .nodestate").textContent() shouldBe "remote"
            }
        }
        When("같은 그림을 입는 상태로 또 달라지면(stopped→abandoned)") {
            page.evaluate("window.__magi_test_map_tick('/a', 'stopped')")
            page.waitForCondition {
                page.evaluate("""document.querySelector('#map .node[data-sock="/a"] .nodestate').textContent""") == "stopped"
            }
            page.evaluate(
                """document.querySelector('#map .node[data-sock="/a"] .nodemark').__magi_same = 'kept';
                   window.__magi_test_map_tick('/a', 'abandoned');"""
            )
            Then("말은 갈리지만 그림은 그 자리에 그대로 — 다섯 상태가 그림 다섯은 아니다") {
                page.waitForCondition {
                    page.evaluate("""document.querySelector('#map .node[data-sock="/a"] .nodestate').textContent""") ==
                        "abandoned"
                }
                page.evaluate("""document.querySelector('#map .node[data-sock="/a"] .nodemark').__magi_same""") shouldBe "kept"
            }
        }
        // 이 화면이 나이를 두 번 적는다: 노드 곁의 낱말 하나, 그리고 상자 위의 <b>문장 안에</b>
        // 든 하나("{ago}부터 소식이 없다"). 둘 다 프레임이 실어 온 초를 낱말로 바꿔 두기만
        // 하면, 그 프레임이 다시 오지 않는 동안 — 즉 침묵이 길어지는 바로 그 동안 — 얼어붙는다.
        When("아무 프레임도 오지 않은 채 시간이 흐르면") {
            // 팩 없는 하네스는 낱말 자리에 키를 앉힌다. 진짜 문(창의 팩 + 판 번호)으로 들여놓되,
            // 감싸는 문장은 나이가 <b>안에</b> 있다는 것만 드러나게 짧게 둔다.
            page.evaluate(
                "window.__magi_labels = {'time.ago': '{d}', 'map.unseen': '[{ago}]'};" +
                    "window.__magi_labels_v = 1;"
            )
            Then("딴 머신 노드의 나이가 자란다") {
                val age = page.locator("#map .node.faroff .nodeage")
                page.waitForCondition { Regex("^\\d+[smhd]$").matches(age.textContent()) }
                val first = Regex("\\d+").find(age.textContent())!!.value.toInt()
                page.waitForCondition {
                    Regex("\\d+").find(age.textContent())!!.value.toInt() >= first + 2
                }
            }
            Then("침묵한 상자 위의 문장도 자란다 — 「3분 전」은 4분째에 거짓말이므로") {
                val seen = page.locator("#map .machine .placeseen.down")
                page.waitForCondition { Regex("^\\[\\d+[smhd]]$").matches(seen.textContent()) }
                val first = Regex("\\d+").find(seen.textContent())!!.value.toInt()
                page.waitForCondition {
                    Regex("\\d+").find(seen.textContent())!!.value.toInt() >= first + 2
                }
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("가로 스크롤이 없고 상자들이 그대로 읽힌다") {
                page.waitForCondition {
                    (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean)
                }
                page.locator("#map .machine").first().isVisible() shouldBe true
            }
        }
    }
})
