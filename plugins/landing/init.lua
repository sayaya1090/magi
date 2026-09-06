-- landing — **말만 하고 끝나는 턴**을 잡는다.
--
-- 실물에서 잰 것이다(2026-09-04, PowerPoint 컴패니언). 한 판은 정찰만 하고 "5장부터 얹겠습니다"
-- 한 줄로 끝났다 — 슬라이드 0장. 다른 판은 일곱 장을 다 짓고도 **닫는 말 없이** 끝나, 명시적으로
-- 요구한 검사표 보고가 통째로 없었다. 두 실패는 같은 모양이다: **턴의 마지막 말이 그 턴이 실제로
-- 무엇을 했는지와 무관하다.**
--
-- 코어는 안 건드린다. 코어의 끝-신고 게이트는 카운슬을 요구하고, 이 판에서 카운슬은 꺼져 있다.
--
-- ## 왜 프록시가 아닌가
--
-- `magi.serve` + `set_base_url` 로 LLM 을 가로채면 끝내는 응답을 직접 볼 수 있다. 안 쓴다 —
-- 그 서버는 본문을 통째로 쓰고 **스트리밍을 못 한다**. 창의 생각 상자와 진행 막대가 죽는다.
-- 결함 하나를 잡으려고 매 턴의 화면을 죽이는 것은 남는 장사가 아니다.
--
-- ## 그래서 무엇을 하는가 — 그리고 무엇을 못 하는가
--
-- **못 하는 것부터.** 플러그인은 턴이 끝나는 것을 **막을 수 없다**. `turn_finished` 는 비차단
-- 관찰 이벤트고, 값을 돌려줘도 아무도 안 읽는다. 그러니 이 플러그인은 「절대 안 일어나게」가
-- 아니라 **「일어나면 값을 치르게」**다. 그 차이를 여기 적어 둔다 — 안 그러면 다음에 읽는 사람이
-- 이걸 보증으로 읽는다.
--
-- 하는 것 셋:
--
-- 1. **`land` 툴** — 끝낼 때 지나는 문. 한 일을 **손잡이와 함께** 신고해야 받는다. 계획 문장만
--    실으면 거절하고, 거절은 툴 결과이므로 **루프가 계속 돈다.** 이것이 이 플러그인에서 유일하게
--    턴을 실제로 이어 붙이는 자리다.
-- 2. **컨텍스트** — 매 턴 계약 한 줄. 그리고 훈계가 아니라 **이 세션에서 잰 수**를 같이 싣는다.
-- 3. **기록·알림** — 안 지나고 끝난 턴을 세고 사람에게 알린다. 조용히 지나가는 것이 제일 나쁘다.

local STORE_MISSES = "landing.misses"

-- 지나간 신고의 수. **세션별로 못 센다** — 툴 실행에는 세션이 안 실려 온다(`magi.session_id`
-- 는 spawn 결과에만 있고 전역 함수가 아니다). 그래서 세션 키로 짜면 툴은 늘 한 이름에 적고
-- 이벤트는 진짜 세션 이름으로 읽어, **카운터가 영구 거짓**이 된다. 시험이 그 모양을 잡았다.
--
-- 그래서 셈으로 짠다: 신고가 하나 들어오면 늘고, 턴이 끝날 때 하나 있으면 쓰고 준다. 한 데몬에
-- 대화가 여럿 붙어 있으면 **잘못된 대화가 그 신고를 쓸 수 있다** — 그때는 놓친 턴을 덜 세게
-- 되지, 없는 것을 지어내지는 않는다. 울지 않는 쪽으로 틀리는 것이 매 턴 우는 것보다 낫다.
local credits = 0

local function n(key)
  return tonumber(magi.store_get(key) or "0") or 0
end

-- **읽은 것은 한 일이 아니다.**
--
-- 첫 판본은 손잡이(숫자)만 봤다. 그런데 읽기에도 손잡이가 있다 — 「list_slides 로 총 1장임을
-- 확인했습니다(id 256#2243864090)」는 숫자도 id 도 있고, 그래서 문을 그냥 지났다. 실물에서
-- 봤다(2026-09-04): 덱을 지으라는 부탁에 정찰만 하고 그 문장으로 착지했다.
--
-- 읽기 동사는 `verified` 의 몫이다. `did` 는 **바뀐 것**을 적는 자리다.
local LOOKED = {
  "확인했습니다", "읽었습니다", "조회했", "살펴", "파악했", "점검했",
  "list_slides", "read_slide", "read_notes", "read_tags", "read_theme_colors",
  "describe_style", "list_layouts", "read_animation", "read_suggestions",
  "checked", "read the", "looked at", "verified that",
}

local function looksLikeReading(s)
  local low = string.lower(s or "")
  for _, w in ipairs(LOOKED) do
    if string.find(low, string.lower(w), 1, true) then return true end
  end
  return false
end

-- **계획인가 한 일인가.** 미래형과 다짐은 한 일이 아니다. 이 목록은 실물에서 그 턴들이 실제로
-- 쓴 말에서 왔다("얹겠습니다", "시작합니다", "따르고", "확인했습니다").
local PLANNY = {
  "하겠습니다", "겠습니다", "예정", "계획입니다", "시작합니다", "진행하겠",
  "will ", "going to", "plan to", "next I", "이제 ",
}

-- 제안·안내 문장은 계획이 아니다. 「무엇을 도와드릴까요? 말씀해 주시면 진행하겠습니다」는 인사지 계획인데,
-- 「겠습니다」 하나로 걸려 인사 턴이 되불려 왔다(실물 2026-09-06: 넛지 → 「바꾼 것 없음」 → land 두 번 거절).
-- 그런 문장을 걷어 낸 뒤에 잰다 — 문장 단위로, 제안 어휘가 든 문장만.
local OFFERS = {
  "도와드리", "도와 드리", "말씀해", "말씀하시", "알려 주시", "알려주시", "알려주세요", "알려 주세요", "필요하시", "원하시",
  "요청하시", "요청해 주시",
  "시키시", "부탁하시", "let me know", "happy to help", "tell me", "how can i help", "what would you like",
}
local function withoutOffers(s)
  local kept = {}
  -- 구분자는 ASCII 만. 문자 집합에 「。」을 넣으면 그 바이트(E3 80 82)가 한글 글자 안의 바이트와 겹쳐 글자
  -- 중간에서 잘린다 — Lua 패턴은 바이트 단위다(실측: 「도와」가 「도�」로 잘려 제안 어휘를 못 봤다).
  local ascii = string.gsub(s or "", "。", ".")
  for sentence in string.gmatch(ascii, "[^%.%!%?\n]+") do
    local low = string.lower(sentence)
    local offer = false
    for _, w in ipairs(OFFERS) do
      if string.find(low, w, 1, true) then offer = true break end
    end
    if not offer then kept[#kept + 1] = sentence end
  end
  return table.concat(kept, ". ")
end
local function looksLikePlan(s)
  local low = string.lower(withoutOffers(s))
  for _, w in ipairs(PLANNY) do
    if string.find(low, w, 1, true) then return true end
  end
  return false
end
-- 「바꾼 것 없음」 — 정직한 무(無)는 신고다. 손잡이를 요구하면 아무것도 안 바꾼 턴은 영영 못 끝난다(실물
-- 2026-09-06: 넛지가 「바꾼 것이 없으면 그렇게 적으라」 했는데 문이 손잡이 없다고 두 번 거절했다).
local NOTHING = { "바꾼 것 없", "바꾼것 없", "바꾼 것이 없", "변경 없", "변경한 것 없", "변경 사항 없", "변경사항 없",
  "수정 없", "수정한 것 없", "고친 것 없", "바꾸지 않았", "변경하지 않았", "수정하지 않았",
  "no change", "nothing changed", "did not change", "changed nothing", "no modification" }
local function saysNothingChanged(s)
  local low = string.lower(s or "")
  for _, w in ipairs(NOTHING) do
    if string.find(low, w, 1, true) then return true end
  end
  return false
end

-- 손잡이가 있는가. **다시 집을 수 있는 이름**이라야 신고다 — 슬라이드 id·번호·파일 경로처럼.
-- 없으면 그 줄은 소감이지 증거가 아니다.
local function hasHandle(s)
  s = s or ""
  return string.find(s, "%d") ~= nil
end

-- 턴의 툴 기록에서 「제목이 접힌다」⚠ 가 마지막 말로 남은 장 번호들. add_slides 답의 slides[].notes 와
-- add_slide/set_text 답의 note(s) 를 읽고, 같은 장에 대한 뒤의 제목 set_text 답이 ⚠ 없이 오면 지운다.
local function titleWarnings(steps)
  local pending = {}
  local function noteSays(notes)
    if type(notes) == "string" then return notes:find("제목이", 1, true) ~= nil end
    if type(notes) ~= "table" then return false end
    for _, n in ipairs(notes) do
      if type(n) == "string" and n:find("제목이", 1, true) then return true end
    end
    return false
  end
  for _, st in ipairs(steps) do
    if not st.failed and type(st.output) == "string" then
      local okj, out = pcall(magi.json_decode, st.output)
      if okj and type(out) == "table" then
        if st.name == "add_slides" and type(out.slides) == "table" then
          for _, row in ipairs(out.slides) do
            if type(row.slide) == "number" then pending[row.slide] = noteSays(row.notes) or nil end
          end
        elseif st.name == "add_slide" and type(out.slide) == "number" then
          pending[out.slide] = noteSays(out.notes) or nil
        elseif st.name == "set_text" then
          local a = st.args
          local role = type(a) == "table" and tostring(a.placeholder or ""):lower() or ""
          local n = type(a) == "table" and tonumber(a.slide) or nil
          if n and (role == "title" or pending[n]) then pending[n] = noteSays(out.note) or nil end
        end
      end
    end
  end
  local list = {}
  for n in pairs(pending) do list[#list + 1] = n end
  table.sort(list)
  for i, n in ipairs(list) do list[i] = tostring(n) end
  return list
end

-- **본 것도 한 일의 일부다.** 신고문은 모델이 쓰지만 이 셈은 턴의 툴 기록에서 한다(`magi.turn_steps`).
-- 실물 2026-09-04: 스킬이 「장마다 한 번 render_slide」를 시켰는데 렌더는 0번이었고 land 는 몰랐다.
-- 실물 2026-09-05: 일곱 장에 「제목이 2줄」⚠ 가 실렸는데 하나도 안 줄이고 「제목 한 줄」이라 신고했다.
-- 거절 사유 문자열을 돌려주거나, 통과면 nil. `land` 와 선언 게이트가 같은 자를 쓴다.
local function seenGate()
  local pages, renders = 0, 0
  local ok, steps = pcall(magi.turn_steps)
  if ok and type(steps) == "table" then
    for _, st in ipairs(steps) do
      if not st.failed then
        if st.name == "add_slides" then
          local a = st.args
          pages = pages + ((type(a) == "table" and type(a.slides) == "table") and #a.slides or 1)
        elseif st.name == "add_slide" then
          pages = pages + 1
        elseif st.name == "render_slide" then
          renders = renders + 1
        end
      end
    end
  end
  if pages > 0 and renders < pages then
    return ("아직 끝이 아닙니다 — 이 턴에 장을 %d장 만들었는데 눈으로 본 것(render_slide)은 %d번입니다. "
      .. "장 하나가 끝날 때마다 render_slide{max_width: 640} 로 한 번 보고 어긋난 것을 고친 뒤 끝내세요. "
      .. "아직 안 본 장부터."):format(pages, renders)
  end
  local wrapped = titleWarnings(ok and steps or {})
  if #wrapped > 0 then
    return ("아직 끝이 아닙니다 — 제목이 접힌다는 ⚠ 가 남은 장: %s. 도구가 잰 것이라 render 로 본 것과 "
      .. "상관없이 남습니다. set_text{slide, placeholder: \"title\"} 로 제목을 한 줄로 줄이면 그 답에 ⚠ 가 "
      .. "안 실리고, 그때 지워집니다."):format(table.concat(wrapped, ", "))
  end
  return nil
end

-- **문은 하나다.** 카운슬이 완료 선언을 심사하는 판이면 `land` 는 두 번째 문이 된다 — 모델은 land 로
-- 끝났다고 알고 멈추고, 코어는 council{complete:true} 를 기다리다 되묻는다(실물 2026-09-05, 매 턴 한 번).
-- 그래서 카운슬이 있으면 land 를 내지 않고 같은 자를 선언 게이트로 건다: 선언이 카운슬에 가기 전에 돈다.
local councilOwnsTheDoor = type(magi.council_enabled) == "function" and magi.council_enabled()

if councilOwnsTheDoor then
  magi.register_declaration_gate{ check = seenGate }
end

if not councilOwnsTheDoor then
magi.register_tool{
  name = "land",
  description = table.concat({
    "Declare this turn finished. A turn that only SAYS what it will do is not finished, and this",
    "is the door that tells the two apart. Call it as the last thing you do. The tool's name is exactly",
    "`land` — no mcp__ prefix. did is an ARRAY OF STRINGS.",
    "did: what you CHANGED, one entry per thing, EACH WITH A HANDLE the reader can go",
    "look at — a slide number or id, a sheet name and range, a file path, a shape id. \"정리했습니다\" is not an entry;",
    "\"슬라이드 7(id 269#2126229183) 표 4×3 을 넣고 대체 텍스트를 달았습니다\" is.",
    "READING IS NOT DOING: a line about list_slides, read_slide or \"확인했습니다\" belongs in",
    "verified, not here. A turn that only looked at the deck has not finished a job that asked",
    "for slides — and this door will say so.",
    "verified: how you checked, in the same concrete terms — which tool you re-read with and what",
    "value came back. If you did not check, say so; an honest gap beats a claim.",
    "left: anything you did NOT do that the ask covered. Empty string if nothing.",
    "An empty or plan-shaped declaration is REFUSED and you keep going — that refusal is not an",
    "error, it is the turn telling you it is not over. If you changed NOTHING this turn, say exactly",
    "that as the single entry of did (\"바꾼 것 없음\") — an honest nothing is accepted; a made-up handle is not.",
    "SEEN IS PART OF DONE: this door reads the turn's own tool log. A turn that added slides",
    "(add_slides / add_slide) and looked at fewer of them than it made (render_slide) is refused —",
    "a page nobody looked at is not finished, whatever the report says.",
  }, " "),
  schema = [[{"type":"object","properties":{
    "did":{"type":"array","items":{"type":"string"}},
    "verified":{"type":"string"},
    "left":{"type":"string"}},"required":["did"]}]],
  execute = function(args)
    -- **모양이 조금 달라도 뜻이 같으면 받는다.** 실물(2026-09-06, 파워포인트): did 를 문자열 하나로 준 호출이
    -- 「비었습니다」로 두 번 거절됐고, 항목을 {change, slide} 객체로 준 호출은 여기서 Lua 오류로 죽었다 — 다섯 번째에야
    -- 착지했다. 문자열은 한 줄로, 객체는 그 안의 글로 읽는다. 거절은 손잡이·계획·읽기에만 남긴다.
    local did = args.did or {}
    if type(did) == "string" then did = { did } end
    if type(did) == "table" then
      local flat = {}
      for _, one in ipairs(did) do
        if type(one) == "table" then
          local parts = {}
          for _, k in ipairs({ "change", "did", "what", "text", "description" }) do
            if type(one[k]) == "string" then parts[#parts + 1] = one[k] end
          end
          for _, k in ipairs({ "slide", "slide_id", "id", "sheet", "address", "path" }) do
            if one[k] ~= nil then parts[#parts + 1] = k .. "=" .. tostring(one[k]) end
          end
          flat[#flat + 1] = #parts > 0 and table.concat(parts, " ") or magi.json_encode(one)
        elseif one ~= nil then
          flat[#flat + 1] = tostring(one)
        end
      end
      did = flat
    end
    if type(did) ~= "table" or #did == 0 then
      return "아직 끝이 아닙니다 — did 가 비었습니다(문자열 배열로 주세요). 이 턴에 실제로 바꾼 것을 손잡이(슬라이드 번호·id·시트와 범위·경로)와 "
        .. "함께 적으세요. 바꾼 것이 정말 없으면 did 에 「바꾼 것 없음」 한 줄로 적으세요.", true
    end
    local nothing = #did == 1 and saysNothingChanged(tostring(did[1]))
    local bad = {}
    for i, one in ipairs(did) do
      local s = tostring(one)
      if nothing then
        -- 정직한 무는 손잡이가 없다 — 그대로 받는다
      elseif not hasHandle(s) then
        bad[#bad + 1] = ("%d번째 줄에 손잡이가 없습니다(«%s»)"):format(i, s)
      elseif looksLikePlan(s) then
        bad[#bad + 1] = ("%d번째 줄이 계획으로 읽힙니다(«%s»)"):format(i, s)
      elseif looksLikeReading(s) then
        bad[#bad + 1] = ("%d번째 줄은 읽은 것입니다(«%s»)"):format(i, s)
      end
    end
    if #bad > 0 then
      return "아직 끝이 아닙니다 — " .. table.concat(bad, " · ")
        .. ". 한 일은 다시 집을 수 있는 이름으로 적습니다. 안 한 것은 did 가 아니라 left 에 적으세요.", true
    end
    local seen = seenGate()
    if seen then return seen, true end
    credits = credits + 1
    local left = args.left or ""
    -- **착지가 곧 끝이다.** 코어의 finish 문(magi.finish)이 있으면 이 단계에서 턴을 끝낸다 — 없던 판은 모델의 다음
    -- 단계가 호출 없이 와야 끝났고, 모델이 land 를 일곱 번 되부른 것을 실물에서 봤다(엑셀 2026-09-07). 옛 호스트엔
    -- 이 문이 없으니 있을 때만 부른다.
    if type(magi.finish) == "function" then pcall(magi.finish) end
    if nothing then
      return "착지했습니다 — 바꾼 것 없음으로 신고했습니다" .. (left ~= "" and (" · 남긴 것: " .. left) or "")
        .. ". 이 턴은 여기서 끝나도 됩니다."
    end
    return ("착지했습니다 — 한 일 %d건%s. 이 턴은 여기서 끝나도 됩니다."):format(
      #did, (left ~= "" and (" · 남긴 것: " .. left) or ""))
  end,
}

end -- not councilOwnsTheDoor

if not councilOwnsTheDoor then
-- ── land 없이 끝난 턴을 **되부른다** ─────────────────────────────────────────
--
-- 알림과 다음 턴의 컨텍스트만으로는 그 턴이 그대로 끝난다 — 사람은 「끝났다」는 말을 받았는데
-- 신고가 없고, 만든 장을 아무도 안 봤을 수 있다(사용자 2026-09-06: 「land 가 오지 않으면 모델에게
-- 넛지가 가야지」). 코어에는 플러그인이 모델에게 말을 넣는 문이 없다. 그래서 데몬의 relay 문
-- (`magi --relay <소켓>`, 데몬 규약을 stdin 으로 받는다)으로 **사용자 메시지**를 넣는다 — 「land 로
-- 끝내라」. 그 메시지는 NUDGE_MARK 로 시작하고, 창은 그 표식을 보고 사람의 말풍선으로 안 그린다
-- (addin: Transcript.js). 소켓은 헬퍼가 `[plugins.landing] socket` 에 심어 둔다(helper/council.go) —
-- 플러그인은 제 데몬의 소켓을 모른다.
--
-- 한 대화에 **두 번까지**. 넛지를 받은 턴이 또 land 없이 끝나면 한 번 더, 그 다음은 사람 몫이다 —
-- 카운슬의 declareAskCap 과 같은 생각이고, 끝없이 되부르는 것은 판이 안 끝나는 것으로 보인다.
-- 인사말에는 안 되부른다. 「하이」에 「무엇을 도와드릴까요」로 답한 턴은 신고할 일이 없다 — 되부르면 그 판은
-- 인사마다 두 턴을 돈다. 되부르는 것은 **일을 했다고 하거나 하겠다고 한** 턴뿐이다: 계획으로 읽히거나
-- (looksLikePlan), 바꿨다는 말이 있거나(아래 CLAIMY). 둘 다 아니면 알림·집계만 하고 넘어간다.
local CLAIMY = { "만들었", "넣었", "바꿨", "고쳤", "추가했", "수정했", "삭제했", "지웠", "정리했", "적용했", "완료", "끝냈",
  "반영했", "옮겼", "채웠", "작성했", "생성했", "배치했", "맞췄", "줄였", "키웠" }
local function claimsWork(s)
  if looksLikePlan(s) then return true end
  for _, w in ipairs(CLAIMY) do
    if string.find(s or "", w, 1, true) then return true end
  end
  return false
end
local NUDGE_MARK = "⟦landing⟧"
local NUDGE_CAP = 2
local nudged = {}   -- session → 이 대화에서 보낸 넛지 수
local function nudge(sid, text)
  local socket = magi.store_get("socket")
  if type(socket) ~= "string" or socket == "" then return false, "socket 미설정" end
  if not sid or sid == "" then return false, "session 없음" end
  nudged[sid] = (nudged[sid] or 0) + 1
  if nudged[sid] > NUDGE_CAP then return false, ("cap %d"):format(NUDGE_CAP) end
  local req = magi.json_encode({ method = "submit", session = sid, text = NUDGE_MARK .. " " .. text })
  local r = magi.exec("magi", { "--relay", socket }, { stdin = req .. "\n", timeout = "15s" })
  if not r then return false, "magi --relay 가 안 돌았다" end
  local resp = magi.json_decode(r.stdout or "")
  if type(resp) == "table" and resp.ok then return true end
  return false, ("relay 답: %s / %s"):format(tostring(r.stdout):sub(1, 120), tostring(r.stderr):sub(1, 120))
end

magi.on("turn_finished", function(ev)
  if credits > 0 then
    credits = credits - 1
    return
  end
  local tail = string.sub(ev.text or "", -80)
  -- **일을 했다고도 하겠다고도 안 한 턴은 신고할 것이 없다.** 인사·되묻기·설명은 세지도 알리지도 않는다 —
  -- 세면 「누적 34회」가 인사마다 자라고, 알리면 작업창에 인사마다 플러그인 줄이 선다(실물 2026-09-06).
  if not claimsWork(ev.text or "") then
    magi.log("landing: unlanded turn, no claim and no plan — not counted · tail=" .. tail)
    return
  end
  local misses = n(STORE_MISSES) + 1
  magi.store_set(STORE_MISSES, tostring(misses))
  local plan = looksLikePlan(ev.text or "")
  -- notify 는 (세션, 글) 둘이다. 글 하나만 넘기던 앞 판본은 여기서 죽었고(bad argument #2), 그 뒤의 줄은
  -- 한 번도 안 돌았다 — 실물 데몬 로그에서 봤다(2026-09-06 01:49). 시험은 알림 채널이 없어 조용히 넘어갔다;
  -- 이제 시험이 채널을 붙여 잰다(probe_landing_test).
  if ev.session and ev.session ~= "" then
    magi.notify(ev.session, ("landing: 이 턴은 `land` 없이 끝났습니다 — 한 일 신고가 없습니다%s (누적 %d회)")
      :format(plan and ", 그리고 마지막 말이 계획입니다" or "", misses))
  end
  magi.log("landing: unlanded turn · tail=" .. tail)
  local ok, why = nudge(ev.session, "이 턴은 `land` 없이 끝났습니다(도구 이름은 그냥 land — mcp__ 접두사 없음, did 는 문자열 배열). 이 턴에 실제로 바꾼 것을 손잡이"
    .. "(슬라이드 번호·id·시트와 범위·경로)와 함께 `land{did, verified, left}` 로 신고하고 끝내세요. 만든 장이 있는데 "
    .. "아직 안 봤으면 render_slide(엑셀은 render_range)로 본 뒤에. 바꾼 것이 정말 없으면 did 에 「바꾼 것 없음」 한 줄로 적으세요."
    .. (plan and " 마지막 말이 계획이었습니다 — 계획은 신고가 아닙니다." or ""))
  if ok then
    magi.log("landing: nudged session " .. tostring(ev.session) .. " to land (" .. nudged[ev.session] .. "/" .. NUDGE_CAP .. ")")
  else
    magi.log("landing: no nudge — " .. tostring(why))
  end
end)
end -- not councilOwnsTheDoor

magi.register_context_provider{
  name = "landing-contract",
  provide = function()
    local misses = n(STORE_MISSES)
    if councilOwnsTheDoor then
      return { text = "턴을 끝내는 문은 `council{complete: true}` 하나입니다. 그 선언은 카운슬에 가기 전에 "
        .. "이 턴의 툴 기록으로 먼저 재집니다 — 만든 장 수만큼 render_slide 로 봤는가, 도구가 단 "
        .. "「제목이 접힌다」⚠ 가 남았는가. 남았으면 선언이 거절되고 사유가 옵니다." }
    end
    local lines = {
      "이 판에는 `land` 툴이 있습니다. 턴을 끝낼 때 마지막으로 부르고, 이 턴에 **실제로 바꾼 것**을 "
        .. "손잡이(슬라이드 번호·id·경로)와 함께 신고하세요. 계획만 적으면 거절되고 턴은 계속됩니다.",
    }
    -- **훈계가 아니라 잰 수를 싣는다.** 숫자가 0이면 아무 말도 더 하지 않는다.
    if misses > 0 then
      lines[#lines + 1] = ("이 컴패니언에서 신고 없이 끝난 턴이 지금까지 %d회입니다.")
        :format(misses)
    end
    return { text = table.concat(lines, " ") }
  end,
}

magi.register_command{
  name = "landing",
  description = "신고 없이 끝난 턴의 누적 수를 보거나(인자 없음) 0으로 되돌립니다(reset)",
  execute = function(args)
    if (args or ""):match("reset") then
      magi.store_set(STORE_MISSES, "0")
      return "landing: 0 으로 되돌렸습니다."
    end
    return ("landing: 신고 없이 끝난 턴 %d회."):format(n(STORE_MISSES))
  end,
}
