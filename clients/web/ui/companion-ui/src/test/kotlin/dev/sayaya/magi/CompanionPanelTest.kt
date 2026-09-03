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
                page.waitForSelector("#agentview #sidecol #detail")
                page.locator("#agentview #filecol").count() shouldBe 1
                page.locator("#agentview #sidecol #side").count() shouldBe 1
                // 사실판은 <b>우측 기둥</b>에 선다. 가운데에 있던 동안 그 판은 auto-fit 격자라
                // 남는 폭을 전부 열로 썼고, 카드가 하나도 없는 화면 — 대부분의 화면 — 에서는
                // 전사 위에 통째로 누웠다. 이 컴패니언이 무엇인가는 곁눈질하는 것이고, M3 가
                // 그 자리라 부르는 side sheet 가 이 콘솔에서는 이 기둥이다.
                page.locator("#sidecol > #detail").count() shouldBe 1
                page.locator("#stream > #detail").count() shouldBe 0
                // 여는 것은 상단의 그 토글이다 — 기둥은 닫힌 채로 오고, 전사가 창을 다 갖는다.
                page.locator("#sideToggle").count() shouldBe 1
            }
            Then("왼쪽은 닫힌 채로, 오른쪽은 열린 채로 온다") {
                // 왼쪽은 파일 트리 — 처음 온 사람에게 이 화면은 대화이고 트리는 찾아가는 것이다.
                // 오른쪽은 사실판이 옮겨 온 뒤로 "이 컴패니언이 무엇인가"를 답하는 자리가 됐고,
                // 그것은 열자마자 있어야 하는 답이다.
                page.waitForSelector("body[files=shut][side=open]")
                // 닫힘은 폭 0이다: 대화가 창을 다 갖는다(운영 규칙 그대로).
                (page.evaluate("getComputedStyle(document.getElementById('agentview'))" +
                    ".gridTemplateColumns.split(' ')[0]")) shouldBe "0px"
                // 칸이 0인 것과 그 안의 것이 <b>0을 지키는 것</b>은 다른 사실이다. 사실판이 이
                // 기둥으로 옮겨 온 날 그것을 배웠다: 닫힘 규칙이 `#side` 하나만 이름 대고 있어서,
                // minmax(14rem,…) 격자인 사실판이 0짜리 칸에서 제 최소 폭을 주장하며 밖으로
                // 넘쳤고 — 전사 위를 덮어 화면이 통째로 사라진 것처럼 보였다. 칸을 재는 것만으로는
                // 안 잡힌다. 칸 안의 것이 실제로 얼마나 넓은지를 잰다.
                // 칸이 0인 것과 그 안의 것이 <b>0을 지키는 것</b>은 다른 사실이다. 사실판이 이
                // 기둥으로 옮겨 온 날 그것을 배웠다: 닫힘 규칙이 `#side` 하나만 이름 대고 있어서,
                // minmax(14rem,…) 격자인 사실판이 0짜리 칸에서 제 최소 폭을 주장하며 밖으로
                // 넘쳤고 — 전사 위를 덮어 화면이 통째로 사라진 것처럼 보였다. 칸을 재는 것만으로는
                // 안 잡힌다. 여기서는 <b>왼쪽</b> 기둥이 그 검사를 진다(오른쪽은 이제 열려 온다).
                (page.evaluate(
                    "(() => Array.from(document.querySelectorAll('#filecol > *'))" +
                        ".every(e => e.getBoundingClientRect().width <= 1))()"
                ) as Boolean) shouldBe true
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
            // 이 목이 내는 어긋남이 바로 그 결함의 모양이다: 데몬은 gpt-oss:120b 위에 있다고
            // 답하고, 그 백엔드의 목록은 fast-model·deep-model이다. 고르개가 「답한 것만」
            // 내놓으면 켤 값이 없어 이 칸은 <b>빈 칸</b>이 되고, 사람은 제 컴패니언이 무엇으로
            // 돌고 있는지 모른 채 그것을 바꾸라는 말을 듣는다(실측: 클라우드 모델 위의 데몬).
            Then("모델 칸은 돌고 있는 것을 보여 준다 — 그 이름이 백엔드의 목록에 없어도") {
                val pick = "#detail .f[data-k=\"field.provider_model\"] " +
                    "md-outlined-select[data-aria-label=\"field.model\"]"
                val model = page.locator(pick)
                page.waitForCondition { model.count() == 1 }
                page.waitForCondition { (page.evaluate("document.querySelector('$pick').value")) == "gpt-oss:120b" }
                // 답한 것들도 여전히 고를 수 있다 — 지금 것을 세우느라 목록을 덮지 않는다.
                model.locator("md-select-option[value=\"fast-model\"]").count() shouldBe 1
                model.locator("md-select-option[value=\"deep-model\"]").count() shouldBe 1
            }
            // 「마지막 활동」은 이 판에서 <b>가만히 있을 때</b> 보는 값이다 — 그래서 명단
            // 프레임에 매달아 두면 정확히 볼 이유가 있는 동안 얼어붙는다(서버는 쉰 시간만
            // 달라진 프레임을 보내지 않는다). 이 줄은 창의 시계로 늙는다.
            Then("마지막 활동은 프레임 없이도 늙는다") {
                page.evaluate(
                    "window.__magi_labels = {'time.ago': '{d}'}; window.__magi_labels_v = 1;"
                )
                val age = page.locator("#detail .f[data-k=\"field.last_activity\"] .v")
                page.waitForCondition { Regex("^\\d+[smhd]$").matches(age.textContent()) }
                val first = Regex("\\d+").find(age.textContent())!!.value.toInt()
                page.waitForCondition {
                    Regex("\\d+").find(age.textContent())!!.value.toInt() >= first + 2
                }
            }
            Then("손잡이를 누르면 그 기둥이 열린다 — 닫힌 기둥의 속은 보이지 않는 것이 옳다") {
                // 손잡이는 마스트헤드에 선다(셸이 내준 자리) — 여는 것은 이 화면의 기둥이지만
                // 손잡이 자체는 이 창을 어떻게 배치할지에 대한 것이라서.
                page.locator("#masthead #chrome #sideToggle").count() shouldBe 1
                page.locator("#masthead #chrome #filesToggle").count() shouldBe 1
                // 이 기둥은 이제 열려서 온다 — 그러니 닫아 보고 나서 다시 연다. 재는 것은
                // 「닫힌 기둥의 속은 화면에 없다」이고, 그 사실은 기본값이 무엇이든 참이어야 한다.
                page.locator("#sideToggle").click()
                page.waitForSelector("body[side=shut]")
                // 닫힘은 <b>기다려야</b> 참이 된다: visibility 는 움직임이 끝난 뒤에 걷히도록
                // 지연이 걸려 있다(그래야 사라지는 동안 내용이 보인다). 누른 직후를 재면 아직 보인다.
                page.waitForCondition { !page.locator("#side #plan").isVisible() }
                // 그리고 그 안의 것들이 자리도 안 차지한다 — 칸이 0이라 남은 폭은 밖으로 넘친다.
                page.waitForCondition {
                    (page.evaluate(
                        "(() => Array.from(document.querySelectorAll('#sidecol > *'))" +
                            ".every(e => e.getBoundingClientRect().width <= 1))()"
                    ) as Boolean)
                }
                openSide(page)
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
                (page.evaluate("document.getElementById('sideToggle').dataset.tip")) shouldBe "side.show"
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
        When("이 기계 데몬이 플릿의 최신보다 뒤처져 있으면") {
            Then("빌드 칸이 갱신 버튼을 인다 — 뒤처졌다는 사실은 명단이 안다") {
                page.waitForSelector("#detail .f[data-k=\"field.version\"] md-text-button:not([hidden])")
                page.locator("#detail .f[data-k=\"field.version\"] .vnum").textContent() shouldBe "v0.28.0"
                // 버튼의 말은 팩 없는 테스트 페이지에서 키 그대로다(tr 폴백).
                page.locator("#detail .f[data-k=\"field.version\"] md-text-button")
                    .textContent() shouldContain "action.update"
                // 아직 아무 일도 없었으니 할 말도 없다.
                // 사실판은 우측 기둥에 산다 — 그 안의 무엇을 누르려면 기둥부터 연다.
                openSide(page)
                page.locator("#detail .f[data-k=\"field.version\"] .updsay[hidden]").count() shouldBe 1
            }
            Then("눌렀는데 아무 말도 없으면 — 회선이 끊긴 것이다 — 버튼은 그 자리에 남는다") {
                page.locator("#detail .f[data-k=\"field.version\"] md-text-button").click()
                page.waitForCondition { page.evaluate("window.__magi_test_update") == "/tmp/a1.sock" }
                page.waitForSelector("#detail .f[data-k=\"field.version\"] .updsay:not([hidden])")
                page.locator("#detail .f[data-k=\"field.version\"] .updsay")
                    .textContent() shouldBe "update.failed"
                // 다시 눌러 볼 만한 유일한 경우다: 아무도 아무 말도 하지 않았다.
                page.locator("#detail .f[data-k=\"field.version\"] md-text-button:not([hidden])").count() shouldBe 1
            }
            Then("데몬이 답하면 그 말이 그대로 서고, 버튼은 걷힌다") {
                // 무엇을 답할지는 스펙이 정한다 — 데몬이 무엇을 말하든 그대로 서는 것이 계약이라서.
                page.evaluate("window.__magi_test_update_says = 'updated v0.28.0 -> v0.29.0, restarting'")
                page.locator("#detail .f[data-k=\"field.version\"] md-text-button").click()
                page.waitForCondition {
                    page.locator("#detail .f[data-k=\"field.version\"] .updsay").textContent() ==
                        "updated v0.28.0 -> v0.29.0, restarting"
                }
                // 사유가 있는 답에 버튼을 다시 세우면 같은 사유를 한 번 더 받으라는 말이 된다.
                page.locator("#detail .f[data-k=\"field.version\"] md-text-button[hidden]").count() shouldBe 1
            }
        }
        When("이 컴패니언이 퍼미션을 기다리면") {
            page.evaluate("window.__magi_test_ask('permission', null)")
            Then("도크에 질문이 서고, 컴포저 위다 — 답하러 목록으로 되돌아가지 않는다") {
                page.waitForSelector("#dock #prompt:not([hidden])")
                page.locator("#prompt .asking").textContent() shouldContain "may I drop the table?"
                // 몇 개 중 몇 번째인지는 둘 이상일 때만 말한다.
                page.locator("#prompt .asking").textContent() shouldContain "ask.of"
                // 근거가 있으면 함께 — 답하기 전에 읽어야 하는 것이다.
                page.locator("#prompt .grounds .gsec .gv").textContent() shouldContain "the migration needs it"
                // 위아래 차례가 곧 "먼저 읽고 답한다"이다.
                (page.evaluate("(() => { const bay = document.querySelector('#dock .bay');" +
                    " return [...bay.children].map(c => c.id || c.className).join(','); })()"))
                    .shouldBe("prompt,cfill")
            }
            Then("결정 넷이 한 무게로 서고, 저마다 표를 단다") {
                page.locator("#prompt .answer .bgroup md-outlined-button").count() shouldBe 4
                page.locator("#prompt .answer .bgroup md-outlined-button .mk").count() shouldBe 4
            }
            Then("올라온다 — 있던 자리에 툭 나타나지 않는다(riseIn)") {
                (page.evaluate("getComputedStyle(document.getElementById('prompt')).animationName"))
                    .shouldBe("riseIn")
            }
        }
        When("보기가 딸린 질문이면") {
            page.evaluate("window.__magi_test_ask('question', '[\"postgres\",\"sqlite\"]')")
            Then("보기들이 답으로 서고, 그밖에 쓸 길도 있다") {
                page.waitForSelector("#prompt .answer.choices")
                page.locator("#prompt .answer.choices md-outlined-button").count() shouldBe 2
                page.locator("#prompt .answer.choices md-outlined-button").first().textContent() shouldBe "postgres"
                // 목록은 제안이지 전부가 아니다 — 목록 밖의 답도 보낼 수 있어야 한다.
                page.locator("#prompt .answer.choices md-text-button").count() shouldBe 1
            }
        }
        When("맨 질문이면") {
            page.evaluate("window.__magi_test_ask('question', null)")
            Then("상자는 질문만 말한다 — 답은 컴포저가 받는다(글 상자를 둘 세우지 않는다)") {
                page.waitForSelector("#prompt:not([hidden])")
                page.locator("#prompt .answer").count() shouldBe 0
                // 그리고 그 사실을 자식에게 알린다 — 답할 입력을 가진 쪽이 쓰라고.
                (page.evaluate("(window.__magi_ask ? 'bridge' : '') + " +
                    "(window.__magi_ask && window.__magi_ask.call ? '/' + window.__magi_ask.call : '')"))
                    .shouldBe("bridge/call_7")
            }
        }
        When("컴포저가 그 문으로 답을 넘기는데 서버가 거부하면") {
            // 자식이 하는 그대로 문을 두드린다(AskSharing.answer가 이 함수를 부른다).
            page.evaluate(
                """window.__magi_test_refuse = 'that call was already answered';
                   window.__magi_test_landed = null;
                   window.__magi_ask_send('yes, drop it', function (why) { window.__magi_test_landed = why; });"""
            )
            Then("사유가 자식에게 그대로 간다 — 이것만이 쓴 글을 되돌려 놓을 수 있다") {
                page.waitForCondition { page.evaluate("window.__magi_test_landed") != null }
                page.evaluate("window.__magi_test_landed") shouldBe "that call was already answered"
                // 답 자체는 보냈다 — 버려진 것은 사유였지 부름이 아니다.
                page.evaluate("window.__magi_test_last") shouldBe "answer alpha yes, drop it"
            }
        }
        When("같은 문으로 답이 서면") {
            page.evaluate(
                """delete window.__magi_test_refuse;
                   window.__magi_test_landed = null;
                   window.__magi_ask_send('yes, drop it', function (why) { window.__magi_test_landed = why; });"""
            )
            Then("빈 사유가 간다 — 되돌릴 것이 없다는 뜻") {
                page.waitForCondition { page.evaluate("window.__magi_test_landed") != null }
                page.evaluate("window.__magi_test_landed") shouldBe ""
            }
        }
        When("질문이 걷히면") {
            page.evaluate("window.__magi_test_ask(null, null)")
            Then("상자도 걷히고, 알림도 비워진다 — 낡은 부름에 답하는 상자가 남지 않게") {
                // [hidden]은 waitForSelector의 기본(보임)으로는 영영 오지 않는다 — 세어서 기다린다.
                page.waitForCondition { page.locator("#prompt[hidden]").count() == 1 }
                (page.evaluate("window.__magi_ask === null || window.__magi_ask === undefined")) shouldBe true
            }
        }
        When("폰 폭(390px)으로 줄이면") {
            page.setViewportSize(390, 844)
            Then("탭 넷이 서고 한 번에 하나만 보인다 — 기본은 대화다(운영의 그 넷)") {
                page.waitForSelector("#ptabs:not([hidden])")
                page.locator("#ptabs md-primary-tab").count() shouldBe 4
                // 순서: 대화 · 작업공간 · 진행 · 정보. 정보가 마지막인 것이 이 줄의 요점이다 —
                // 둘째 자리에 있던 동안, 대화에서 옆으로 한 번 넘기면 닿는 것이 호스트와 빌드
                // 번호였다. 읽으러 온 것은 대화이고 그 다음은 무엇을 하고 있나와 어느 파일인가다.
                page.locator("#ptabs md-primary-tab").allTextContents()
                    .shouldBe(listOf("panel.talk", "panel.files", "panel.plan", "panel.facts"))
                (page.evaluate("document.body.getAttribute('panel')")) shouldBe "talk"
                page.locator("#stream .cfill:not([hidden])").count() shouldBe 1
                page.locator("#detail[hidden]").count() shouldBe 1
                // 기둥은 서 있고 그 속이 숨는다(운영도 그렇다: 폰에서 #filecol은 높이 0의 빈
                // 기둥으로 남는다) — 기둥째 걷으면 옆 기둥들이 그 자리만큼 흔들린다.
                page.locator("#filecol > .cfill[hidden]").count() shouldBe 1
            }
            Then("정보 탭은 사실판을 보인다") {
                page.locator("#ptab-facts").click()
                page.waitForSelector("#detail:not([hidden])")
                page.locator("#stream .cfill[hidden]").count() shouldBe 1
            }
            Then("작업공간 탭은 왼쪽 기둥을, 진행 탭은 오른쪽 판을 보인다") {
                page.locator("#ptab-files").click()
                page.waitForSelector("#filecol > .cfill:not([hidden])")
                page.locator("#detail[hidden]").count() shouldBe 1
                page.locator("#ptab-plan").click()
                page.waitForSelector("#sidecol #side:not([hidden])")
                page.locator("#filecol > .cfill[hidden]").count() shouldBe 1
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
        When("이 컴패니언이 답하기를 멈추면(명단에 남은 채 live=false)") {
            page.evaluate("window.__magi_test_stopped()")
            Then("사실판은 그대로 선다 — 멈춘 순간이 바로 이것들을 읽고 싶은 때다") {
                // 한때 여기서 판이 통째로 숨었다: 스토어가 "답하지 않는 행"을 "없는 행"으로
                // 돌려주어서, 어디서 무엇을 하던 컴패니언이었는지가 멈추는 순간 사라졌다.
                // 요약 줄이 먼저 바뀐다 — 그 한 줄이 상태와 작업공간을 같이 인다.
                // 팩이 없는 페이지에서 stateWord는 원어를 돌려준다(키가 아니라 "stopped").
                page.waitForCondition {
                    (page.locator("#detail .foldbar .sum").textContent() ?: "").startsWith("stopped")
                }
                page.locator("#detail[hidden]").count() shouldBe 0
                page.locator("#detail .foldbar .sum").textContent() shouldContain "/Users/you/work/app"
                page.locator("#detail .f[data-k=\"field.workspace\"] .v")
                    .textContent() shouldBe "/Users/you/work/app"
                page.locator("#detail .f[data-k=\"field.steps\"] .v").textContent() shouldBe "7"
            }
        }
        When("세션 고르개를 읽으면") {
            Then("줄마다 아이디와 제목을 함께 적는다 — 아이디만이면 해시 목록이다") {
                val opts = page.locator("#detail .f[data-k=\"field.session\"] md-select-option")
                page.waitForCondition { opts.count() == 4 }
                // 첫 줄은 명단이 아직 못 따라잡은 지금 이 세션(픽스처의 history엔 없다).
                // 팩이 없는 페이지에서 tr은 키를 돌려주므로 키 계약으로 잰다.
                opts.nth(0).textContent()?.trim() shouldBe "s_demo1 · session.thisone"
                opts.nth(1).textContent()?.trim() shouldBe "s_now · the open one"
                opts.nth(2).textContent()?.trim() shouldBe "s_old · fix the retry storm"
                // 지난 세션의 제목 없음은 지금 이 세션의 그것과 다른 말이다.
                opts.nth(3).textContent()?.trim() shouldBe "s_bare · session.untitled"
            }
        }

        When("한 시간을 일한 뒤에 묻는 긴 질문이면 — 폰을 눕힌 채로(664×390)") {
            // 운영이 딥 화면(?ask=)을 지은 이유가 이 모양이다: 명령 한 줄과 산문 세 토막.
            // 이 콘솔에는 그 화면이 없고 도크가 인다 — 그러면 도크가 이것을 감당하는지가
            // 딥 화면 없음의 값을 정한다. 눕힌 폰은 높이가 가장 적은 창이고, 운영이 한 번
            // 밟은 자리다(캡이 세로 폰의 미디어쿼리 안에 적혀 있어 가로에선 캡이 없었다).
            page.setViewportSize(664, 390)
            page.evaluate("window.__magi_test_ask_long()")
            page.waitForSelector("#dock #prompt:not([hidden])")
            Then("도크는 창의 절반까지만 — 고정된 띠가 화면을 먹지 않는다") {
                page.waitForCondition {
                    (page.evaluate("document.getElementById('dock').getBoundingClientRect().height" +
                        " <= window.innerHeight / 2 + 1") as Boolean)
                }
                // 그리고 전사가 남는다 — 절반은 여전히 대화의 것이다.
                (page.evaluate("document.getElementById('dock').getBoundingClientRect().top" +
                    " >= window.innerHeight / 2 - 1") as Boolean) shouldBe true
            }
            Then("잘리지 않는다 — 넘치는 만큼 도크가 제 안에서 구른다") {
                (page.evaluate("(() => { const d = document.getElementById('dock');" +
                    " return d.scrollHeight > d.clientHeight" +
                    " && getComputedStyle(d).overflowY !== 'hidden'; })()") as Boolean) shouldBe true
                // 질문은 통째로 있다(가로로 잘라 내지 않는다 — 명령은 끝까지 읽어야 한다).
                ((page.locator("#prompt .asking").textContent()?.length ?: 0) > 1000) shouldBe true
                (page.evaluate("(() => { const q = document.querySelector('#prompt .asking');" +
                    " return q.scrollWidth <= q.clientWidth + 1; })()") as Boolean) shouldBe true
            }
            Then("답은 어디까지 굴러도 그 자리에 있다 — 읽는 것과 하는 것이 갈리지 않게") {
                val seen = "(() => { const d = document.getElementById('dock').getBoundingClientRect();" +
                    " const b = document.querySelector('#prompt .answer .bgroup md-outlined-button');" +
                    " if (!b) return false; const r = b.getBoundingClientRect();" +
                    " return r.height > 0 && r.top >= d.top - 1 && r.bottom <= d.bottom + 1" +
                    " && r.bottom <= window.innerHeight + 1; })()"
                (page.evaluate(seen) as Boolean) shouldBe true
                page.evaluate("(() => { const d = document.getElementById('dock');" +
                    " d.scrollTop = d.scrollHeight; return true; })()")
                page.waitForCondition { page.evaluate(seen) as Boolean }
            }
            Then("가로 스크롤은 없다") {
                (page.evaluate("document.scrollingElement.scrollWidth <= window.innerWidth + 1") as Boolean) shouldBe true
            }
            page.setViewportSize(1400, 900)
        }

        When("가서 보는 것 — 도구") {
            // 이 셋은 전사의 행이 아니라 카드다: 누가 물어서 나온 답이지 일어난 일의 기록이
            // 아니다(운영의 그 판단). 그래서 재는 것도 "전사 자리에 카드가 섰는가"다.
            // 문 셋은 사실판 안에 있고 그 판은 우측 기둥에 산다 — 누르려면 기둥을 먼저 연다.
            // 그것이 이 화면의 계약이다: 처음 온 사람에게 이 화면은 대화이고, 이 컴패니언이
            // 무엇인가는 물어봐야 나온다.
            openSide(page)
            page.locator("#detail .f[data-k=\"field.what_it_has\"] .bgroup button.deeper").count() shouldBe 3
            Then("문 셋은 한 줄에 머문다 — 넘치면 옆 칸 위에 그려진다, 겹치지 않은 채로") {
                // 운영이 실측으로 되밟은 자리다: 아이콘까지 283px인 줄이 238px 트랙에 들어가며
                // nowrap 플렉스가 줄어들지 않고 제 상자 밖에 <b>칠했다</b>. DOM은 겹치지 않으니
                // 상자 교차 검사는 통과한다 — 재야 할 것은 픽셀이라, 칸의 오른쪽 끝을 본다.
                (page.evaluate(
                    "(() => { const r = document.querySelector('#detail .f[data-k=\"field.what_it_has\"]');" +
                        " const g = r.querySelector('.bgroup');" +
                        " return g.getBoundingClientRect().right <= r.getBoundingClientRect().right + 1; })()"
                ) as Boolean) shouldBe true
            }
            page.locator("#detail .f[data-k=\"field.what_it_has\"] .bgroup button.deeper").first().click()
            Then("카드가 서고 줄에 제 탭이 생긴다 — 사실판은 물러난다") {
                page.waitForSelector("#fileview .dinsp#insp\\.tools")
                page.locator("#cardtabs md-secondary-tab[data-card=\"insp.tools\"]").count() shouldBe 1
                // 사실판은 물러나지 않는다 — 이제 다른 기둥이라 자리를 다투지 않는다. 예전에는
                // 둘이 가운데를 나눠 써서 카드가 서면 사실판이 숨어야 했고, 그래서 도구를 열면
                // 이 컴패니언이 무엇인지가 화면에서 사라졌다.
                page.locator("#detail").isVisible() shouldBe true
            }
            Then("데몬이 말한 것만 적는다 — 이 콘솔의 목록이 아니라") {
                page.locator("#fileview .dinsp .dlog .f .k").count() shouldBe 3
                page.locator("#fileview .dinsp .dlog .f .k").first().textContent() shouldBe "read"
            }
        }
        When("도구를 물었는데 데몬이 빈 답을 하면") {
            // 빈 답은 "도구가 없다"가 아니다 — 컴패니언은 늘 무언가를 갖고 있고, 빈 답이
            // 뜻하는 것은 물어볼 수 없을 만큼 낡은 데몬이다. 다른 말을 적으면 화면이 사실을
            // 지어낸다. 이 갈래는 낡은 데몬이 없으면 영영 안 열리므로 여기서만 열린다.
            page.evaluate("window.__magi_test_tools_says = '[]'")
            page.locator("#cardtabs md-secondary-tab[data-card=\"insp.tools\"] .tabclose").click()
            page.locator("#detail .f[data-k=\"field.what_it_has\"] .bgroup button.deeper").first().click()
            Then("목록 대신 그 사정을 적는다 — 빈 목록을 그리지 않는다") {
                page.waitForSelector("#fileview .dinsp .dnote")
                page.locator("#fileview .dinsp .dnote").textContent() shouldBe "insp.tools_unknown"
                page.locator("#fileview .dinsp .dlog").count() shouldBe 0
            }
            page.evaluate("delete window.__magi_test_tools_says")
        }
        When("도구를 물었는데 답이 아예 안 오면") {
            // 빈 답과 같은 사정이 아니다. 낡은 데몬은 `[]`로 도착한다(BFF가 browse 실패를 일부러
            // 빈 목록으로 접는다) — null은 magi-web에 못 닿았거나 본문이 깨진 것이고, 그 셋 중
            // 어느 것도 낡은 데몬이 아니다. 둘을 한 문장으로 접으면 그 문장이 유일하게 설명할 수
            // 없는 경우에 그 문장이 붙는다.
            page.evaluate("window.__magi_test_tools_says = 'null'")
            page.locator("#cardtabs md-secondary-tab[data-card=\"insp.tools\"] .tabclose").click()
            page.locator("#detail .f[data-k=\"field.what_it_has\"] .bgroup button.deeper").first().click()
            Then("닿지 못했다고 적는다 — 낡은 데몬이라고 단정하지 않는다") {
                page.waitForSelector("#fileview .dinsp .dnote")
                page.locator("#fileview .dinsp .dnote").textContent() shouldBe "error.unreachable"
                page.locator("#fileview .dinsp .dlog").count() shouldBe 0
            }
            page.evaluate("delete window.__magi_test_tools_says")
        }
        When("가서 보는 것 — 루프") {
            page.locator("#cardtabs md-secondary-tab[data-card=\"insp.tools\"] .tabclose").click()
            page.locator("#detail .f[data-k=\"field.what_it_has\"] .bgroup button.deeper").nth(1).click()
            Then("지도는 정렬이 곧 내용이다 — 공백을 접으면 걸음 번호가 줄줄이 붙은 문단이 된다") {
                page.waitForSelector("#fileview .dinsp .dpre")
                page.locator("#fileview .dinsp .dpre").textContent() shouldContain "1 plan"
                (page.evaluate("getComputedStyle(document.querySelector('#fileview .dinsp .dpre'))" +
                    ".whiteSpace.startsWith('pre')") as Boolean) shouldBe true
            }
            Then("갈라져 나온 세션이 아니면 원본 절은 서지 않는다 — 아무것도 아닌 것과의 차이는 전사 전체다") {
                page.locator("#fileview .dinsp .dk").count() shouldBe 1
            }
        }
        When("그 세션이 다른 세션에서 갈라져 나온 것이면") {
            page.evaluate(
                "window.__magi_test_loop_says = JSON.stringify(" +
                    "{map: '1 plan\\n2 edit', origin: 's_parent', diff: '- old\\n+ new'})"
            )
            page.locator("#cardtabs md-secondary-tab[data-card=\"insp.loop\"] .tabclose").click()
            page.locator("#detail .f[data-k=\"field.what_it_has\"] .bgroup button.deeper").nth(1).click()
            Then("원본과 그 뒤의 차이가 함께 선다") {
                page.waitForCondition { page.locator("#fileview .dinsp .dk").count() == 3 }
                page.locator("#fileview .dinsp .dv").textContent() shouldBe "s_parent"
                page.locator("#fileview .dinsp .dpre").count() shouldBe 2
                page.locator("#fileview .dinsp .dpre").last().textContent() shouldContain "+ new"
            }
            page.evaluate("delete window.__magi_test_loop_says")
            page.locator("#cardtabs md-secondary-tab[data-card=\"insp.loop\"] .tabclose").click()
        }
        When("가서 보는 것 — 보고 양식") {
            page.locator("#detail .f[data-k=\"field.what_it_has\"] .bgroup button.deeper").nth(2).click()
            Then("어느 층에서 온 양식인지와 그 절들이 선다") {
                page.waitForSelector("#fmtDialog .fmtform .fmtrow")
                page.locator("#fmtDialog .dlgsup.from").textContent() shouldBe "fmt.from_workspace"
                page.locator("#fmtDialog .fmtrow").count() shouldBe 1
            }
            Then("절을 더할 수 있다 — 더하기는 늘 맨 아래에 남는다") {
                // 이 폼이 무너진 자리가 여기였다: 절을 더하기 단추 <b>앞에</b> 끼워 넣는데
                // 그 단추가 아직 폼의 자식이 아니라 첫 절에서 예외가 났고, 그러면 폼도 사유가
                // 설 자리도 저장의 귀도 안 달린 채 끝났다. 그래서 둘 다 잰다 — 절이 서는가,
                // 그리고 더하기가 여전히 맨 아래인가.
                page.locator("#fmtDialog .fmtform > *").last().evaluate("e => e.tagName.toLowerCase()") shouldBe "md-text-button"
                page.locator("#fmtDialog .fmtform md-text-button").click()
                page.waitForCondition { page.locator("#fmtDialog .fmtrow").count() == 2 }
                page.locator("#fmtDialog .fmtform > *").last().evaluate("e => e.tagName.toLowerCase()") shouldBe "md-text-button"
            }
            // 저장을 눌러 창이 닫히는 것은 <b>저장됐다</b>는 만국 공통의 신호다. 이 자리는 뒤에서
            // 진실을 다시 읽는 것이 없다 — 절은 이 창을 열 때만 읽는 파일 안에 있다.
            Then("거절당하면 닫지 않고 그 사유를 세운다") {
                page.evaluate("window.__magi_test_format_refuses = 'two sections named what'")
                page.locator("#fmtDialog md-filled-button").click()
                page.waitForCondition {
                    page.locator("#fmtDialog .fmtwhy").textContent() == "two sections named what"
                }
                page.locator("#fmtDialog").count() shouldBe 1
                page.evaluate("delete window.__magi_test_format_refuses")
            }
            Then("받아들여지면 그때 닫힌다") {
                page.locator("#fmtDialog md-filled-button").click()
                page.waitForCondition { page.locator("#fmtDialog").count() == 0 }
                // 이름 없는 절은 절이 아니다 — 위에서 더한 빈 줄은 저장에 실리지 않는다.
                page.evaluate("window.__magi_test_format") shouldBe "what|What changed"
            }
        }
        When("보고 양식을 못 읽으면") {
            // 못 읽은 것은 「기본값」이 아니다 — from은 셋 중 어디서 왔는가를 말하는 값이고,
            // 그중 「기본값」은 <i>파일이 아예 없다</i>는 뜻이다. 모르는 것을 없다는 적극적
            // 진술로 바꿔 적는 셈이 된다.
            page.evaluate("window.__magi_test_format_says = 'null'")
            page.locator("#detail .f[data-k=\"field.what_it_has\"] .bgroup button.deeper").nth(2).click()
            Then("닿지 못했다고 적는다 — 어느 층에서 왔다고 단정하지 않는다") {
                page.waitForSelector("#fmtDialog .dnote")
                page.locator("#fmtDialog .dnote").textContent() shouldBe "error.unreachable"
                page.locator("#fmtDialog .dlgsup.from").count() shouldBe 0
            }
            Then("저장 단추가 아예 없다 — 못 읽은 폼 위의 저장은 파일을 통째로 덮어쓴다") {
                io.kotest.assertions.withClue("빈 폼은 '절이 없다'로 읽히고, 저장은 패치가 아니라 파일 전체다") {
                    page.locator("#fmtDialog .fmtform").count() shouldBe 0
                }
                page.locator("#fmtDialog md-filled-button").count() shouldBe 0
            }
            page.evaluate("delete window.__magi_test_format_says")
            page.locator("#fmtDialog md-text-button").first().click()
            page.waitForCondition { page.locator("#fmtDialog").count() == 0 }
        }
        When("자식이 <b>이것을 보이라</b>고 청하면") {
            // 부탁과 보고는 다른 칸이다(CardSharing.ask/asked 대 showing). 한 칸이었을 때 부모가
            // 적은 보고가 다음 그리기에 요청으로 되읽혔고, 눌러서 연 카드 대신 사실판이 섰다.
            // 갈라 둔 지금은 그 일이 표현되지 않는다 — 부모는 부탁을 적을 수 없다.
            //
            // 자식은 여기 없으므로(코딩 화면은 다른 모듈이다) 자식이 하는 일을 그대로 한다:
            // 부탁 칸에 적고 부탁의 종을 울린다. 그 종이 <b>따로</b> 있다는 것이 여기서 재는 것이다 —
            // 보고의 종에 얹어 두었을 때 부탁은 자식이 카드를 다시 건네 주는 우연으로만 도착했다.
            page.locator("#detail .f[data-k=\"field.what_it_has\"] .bgroup button.deeper").first().click()
            page.waitForSelector("#fileview .dinsp#insp\\.tools")
            Then("서 있던 것은 방금 연 카드다 — 이 장면이 재는 것은 <b>바뀌는가</b>다") {
                page.evaluate("window.__magi_cards_showing") shouldBe "insp.tools"
                // 사실판은 물러나지 않는다 — 이제 다른 기둥이라 자리를 다투지 않는다. 예전에는
                // 둘이 가운데를 나눠 써서 카드가 서면 사실판이 숨어야 했고, 그래서 도구를 열면
                // 이 컴패니언이 무엇인지가 화면에서 사라졌다.
                page.locator("#detail").isVisible() shouldBe true
            }
            Then("청한 것이 선다 — 카드 줄은 그대로인데") {
                page.evaluate("(() => { window.__magi_cards_ask = 'facts';" +
                    " window.__magi_cards_ask_obs.forEach(f => f()); true })()")
                page.waitForCondition { page.evaluate("window.__magi_cards_showing") == "facts" }
                page.locator("#detail").isVisible() shouldBe true
                // 닫은 것이 아니라 <b>갈아탄</b> 것이다: 그 카드의 탭은 그대로 있다.
                page.locator("#cardtabs md-secondary-tab[data-card=\"insp.tools\"]").count() shouldBe 1
            }
            Then("이뤄진 부탁은 지워진다 — 읽을 때가 아니라 <b>이뤄질 때</b>") {
                // 읽자마자 비우면, 아직 카드가 서지 않은 것을 청한 부탁은 그것이 설 기회를 갖기 전
                // 그리기에서 증발한다(코딩 화면의 팔레트가 그 자리다: 본문이 와야 카드가 선다).
                page.evaluate("window.__magi_cards_ask || ''") shouldBe ""
            }
            page.locator("#cardtabs md-secondary-tab[data-card=\"insp.tools\"] .tabclose").click()
        }
    }
})

/**
 * 우측 기둥을 <b>열려 있게</b> 한다.
 *
 * 누르지 않고 상태를 본다: `#sideToggle` 은 토글이라 두 블록이 각자 누르면 뒤엣것이 닫는다 —
 * 실제로 그랬다(도구 넷이 한꺼번에 빨개졌다). 시험이 원하는 것은 "눌렀다"가 아니라 "열려 있다"다.
 */
private fun openSide(page: com.microsoft.playwright.Page) {
    if (page.locator("body[side=open]").count() == 0) {
        page.locator("#sideToggle").click()
        page.waitForSelector("body[side=open]")
    }
}
