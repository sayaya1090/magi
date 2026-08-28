package dev.sayaya.magi.demo;

import elemental2.dom.RequestInit;
import elemental2.dom.Response;
import elemental2.promise.Promise;

/**
 * 컴패니언 곁의 것들 — 계획·컨텍스트·도구·루프·일감·예약, 그리고 그 컴패니언이 닿는 모델들.
 *
 * 전부 <b>일어난 일의 기록이 아니라 물어서 나온 답</b>이라 전사가 아니라 여기 있다(운영이
 * 이것들을 판으로 세운 그 판단). 픽스처가 잘 풀린 경우만 담지 않는 것도 같은 이유다: 나쁘게
 * 끝난 배경 명령 하나가 있어야 그 카드가 무엇을 위해 있는지 보인다.
 */
final class Companion {
    private Companion() {}

    static Promise<Response> answer(String path, RequestInit init) {
        // 한 번의 읽기로 오는 것들 — 지난 세션과 자식의 전사, 심의의 증거, 낳은 자식들.
        // 쓰기 분기 <b>앞에</b> 둔다: 이것들은 몸 없는 물음이라 저 아래에서는 닿지 않는다.
        // 한 세션을 물으면 <b>그 전사</b>다 — 데모의 전사는 하나이므로 스트림이 흘리는 것과
        // 같은 것을 돌려준다(운영도 그 하나를 쓴다). 두 벌을 두면 지난 일과 지금이 서로 다른
        // 대화를 이야기한다.
        if ("/transcript".equals(path)) return Mock.json(Fleet.transcript());
        if ("/council".equals(path)) {
            return Mock.json("{\"task\":\"Make the console read the same as the terminal\","
                    + "\"report\":\"Ported the workspace card and its editor.\","
                    + "\"actions\":\"read · edit · build\",\"changes\":\"web/ui/…\"}");
        }
        // 낳은 자식 하나면 족하다 — 이 화면이 무엇을 보이는 화면인지는 하나로도 보인다.
        if ("/subagents".equals(path)) {
            return Mock.json("[{\"id\":\"s_demo_child\",\"role\":\"scout\","
                    + "\"task\":\"find every component that draws an empty state\","
                    + "\"model\":\"qwen3-coder-next\",\"ago\":240,\"running\":false}]");
        }
        // 갱신은 몸 없는 POST다 — 쓰는 부름인데 wrote(init)가 거짓이라 저 아래 분기로는 닿지
        // 않는다. 데몬이 답한 <b>말</b>을 그대로 그리는 것이 이 컨트롤의 계약이므로, 데모도 말을
        // 한 줄 답한다(그림자 상태는 두지 않는다 — 데모의 명단은 그대로 v0.22.0이다).
        if ("/update".equals(path)) return Mock.json("updated v0.22.0 \u2192 v0.23.0, restarting");
        // 컴포저의 이어쓰기 — 이 길을 목이 몰라 데모가 제 회선으로 나갔고, 파일만 내주는 자리라
        // POST는 501이었다(실측). 아래 삼킴 분기에 얹지 않는 이유는 빈 답이 지우라는 뜻이어서다:
        // 받아만 두면 501이 조용한 빈칸으로 바뀔 뿐 힌트는 여전히 안 뜬다. 쓰다 만 말에 <b>이어
        // 붙는</b> 글이므로 앞 빈칸까지 답의 일부다. 짝은 편집기의 유령 글자(/complete, Workspace).
        // 컴포저의 이어쓰기 — 이 길을 목이 몰라 데모가 제 회선으로 나갔고, 파일만 내주는 자리라
        // POST는 501이었다(실측). 아래 삼킴 분기에 얹지 않는 이유는 빈 답이 지우라는 뜻이어서다:
        // 받아만 두면 501이 조용한 빈칸으로 바뀔 뿐 힌트는 여전히 안 뜬다. 쓰다 만 말에 <b>이어
        // 붙는</b> 글이므로 앞 빈칸까지 답의 일부다. 짝은 편집기의 유령 글자(/complete, Workspace).
        if ("/suggest".equals(path)) return Mock.json(" and show me the failing ones");
        if (Mock.wrote(init)) {
            // 바꾸는 부름들(모델·결재·백엔드·보고서 양식·접기·보내기)은 받아만 둔다:
            // 데모의 값은 다음 물음에서 그대로 돌아온다 — 거짓 성공을 그리지 않는다.
            // 지난 세션의 전사 — 한 번의 읽기다(스트림이 아니라).
        if ("/transcript".equals(path)) return Mock.json("[{\"who\":\"user\",\"text\":\"why did the retries storm?\"},"
                + "{\"who\":\"assistant\",\"text\":\"the backoff had no ceiling — capped at 30s\"}]");
        // 심의의 증거 — 라운드마다 그 자식이 무엇을 보고 무엇을 했는지.
        if ("/council".equals(path)) return Mock.json(
                "{\"task\":\"Make the console read the same as the terminal\","
              + "\"report\":\"Ported the workspace card and its editor.\","
              + "\"actions\":\"read · edit · build\",\"changes\":\"web/ui/…\"}");
        switch (path) {
                case "/model": case "/permission": case "/providers": case "/report-format":
                case "/compact": case "/submit": case "/answer": case "/interrupt":
                // 옮기기도 받아만 둔다 — 빈 답이 "옮겼다"이다. 데모의 명단은 그대로라서 화면은
                // 여전히 그 세션을 지난 것으로 보는데, 그림자 상태를 두면 데모가 명단과 어긋난
                // 두 번째 사실이 된다(갱신 버튼에서 한 그 판단).
                case "/resume":
                    return Mock.json("");
                default: break;
            }
        }
        switch (path) {
        // 구 콘솔의 데모와 같은 다섯 — 끝난 것 둘, 하는 중 하나, 아직 둘. 진행이 막대와 개수로
        // 함께 말해지는 것이 이 판의 요점이라, 세 상태 중 어느 것도 비어 있으면 안 된다.
            case "/plan": return Mock.json("[{\"content\":\"read what the empty states do now\",\"status\":\"completed\"},"
                        + "{\"content\":\"write the spec\",\"status\":\"completed\"},"
                        + "{\"content\":\"name the tokens it uses\",\"status\":\"in_progress\"},"
                        + "{\"content\":\"get it reviewed by buttons\",\"status\":\"pending\"},"
                        + "{\"content\":\"fold it into the component docs\",\"status\":\"pending\"}]");
        // 창이 얼마나 찼는지만이 아니라 <b>접혀 나간 것</b>도: 두 번 접었고 39,000 토큰을 덜어
        // 냈으며 직전에 48,000이 9,000이 되었고, 그때 접힌 주제 셋은 되부를 수 있다. 그 줄이 없으면
        // 사라진 맥락은 화면 어디에도 적히지 않는다(구 콘솔 데모가 이 값을 담은 이유).
            case "/context": return Mock.json("{\"model\":\"qwen3-coder-next\",\"window\":128000,\"used\":104300,\"estimated\":false,"
                        + "\"messages\":61,\"compactions\":2,\"shed\":39000,"
                        + "\"lastBefore\":48000,\"lastAfter\":9000,"
                        + "\"topics\":[\"internal/adapter/fleet/fleet.go\",\"cmd/magi-web/page.go\",\"discussion\"]}");
            case "/model": return Mock.json("[\"gpt-oss:120b-cloud\",\"qwen3-coder-next\",\"claude-sonnet-5\"]");
            case "/tools": return Mock.json("[\"read\",\"edit\",\"multiedit\",\"bash\",\"glob\",\"grep\",\"todo\",\"hand_off\",\"wait_for\"]");
            case "/loop": return Mock.json("{\"map\":\"1 plan\\n2 read · edit\\n3 build → ok\",\"origin\":\"\",\"diff\":\"\"}");
        // 구 콘솔의 데모와 같은 둘: 지금 도는 자식 하나와, 나쁘게 끝난 배경 명령 하나 —
        // 성공만 있는 픽스처는 이 카드가 무엇을 위해 있는지 보여 주지 못한다.
            case "/jobs": return Mock.json("{\"children\":[{\"id\":\"s_demo_child\",\"tool\":\"scout\","
                        + "\"task\":\"find every component that draws an empty state\",\"running\":true,\"steps\":4}],"
                        + "\"background\":[{\"id\":\"bg_demo\",\"command\":\"npm run build\",\"running\":false,\"exit\":1,"
                        + "\"tail\":\"compiling\\u2026\\n3 warnings\\nerror: Token --surface-dim is not defined\"}]}");
        // 셋: 도는 것, 꺼 둔 것, 그리고 <b>영영 안 도는 것</b> — 마지막이 이 목록이 존재하는
        // 이유다(켜져 있고 평범해 보이는데 다시는 아무도 그 얘기를 하지 않는다).
            case "/cron": return Mock.json("[{\"name\":\"nightly-audit\",\"schedule\":\"0 3 * * *\",\"enabled\":true,"
                        + "\"prompt\":\"walk yesterday's commits and report anything that looks like a regression\","
                        + "\"file\":\"/Users/you/work/design-system/.magi/config.toml\"},"
                        + "{\"name\":\"weekly-report\",\"schedule\":\"0 9 * * 1\",\"enabled\":false,"
                        + "\"prompt\":\"summarise what changed in the design system this week\","
                        + "\"file\":\"/Users/you/.config/magi/config.toml\",\"global\":true},"
                        + "{\"name\":\"leap-day\",\"schedule\":\"0 0 30 2 *\",\"enabled\":true,"
                        + "\"problem\":\"this schedule never comes round\","
                        + "\"prompt\":\"the one nobody noticed had stopped\","
                        + "\"file\":\"/Users/you/work/design-system/.magi/config.toml\"}]");
            case "/report-format": return Mock.json("{\"from\":\"console\",\"sections\":[{\"key\":\"what\",\"prompt\":\"What changed\"}," +
                "{\"key\":\"why\",\"prompt\":\"Why it was needed\"}]}");
            default: return null;
        }
    }
}
