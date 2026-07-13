-- engram — 자기개선 관찰자 플러그인 (ds-cortex 코어 로직의 Lua 포트).
-- 캡처: user_message에 결과 신호가 보이면 magi.analyze 사이드카(도구 없음)로
-- 교훈/검증된 스킬을 추출해 SESSION_SUMMARY.md / .claude/skills 에 기록.
-- 회상: register_context_provider 로 최근 교훈을 컨텍스트에 주입.
-- (관찰 이벤트는 호스트가 턴 경로 밖의 워커에서 돌리므로 사이드카가 턴을 막지 않는다.
-- 사이드카가 도는 동안 이 플러그인의 컨텍스트 프로바이더는 그 스텝을 스킵할 수 있다 —
-- 호스트의 바운드 락 획득이 의도한 우아한 퇴화다. 파일 쓰기는 호스트의 단일 관찰
-- 워커가 직렬화하므로 별도 파일락이 필요 없다.)

local SUMMARY = "SESSION_SUMMARY.md"
local SKILLS_DIR = ".claude/skills"
local LEDGER_TITLE = "# 작업 이력 및 교훈 기록 (팀 공유)"
local LEDGER_HEADER = "| 일시 | 사용자 | 분류 | 작업 | 시도한 접근 | 결과 | 교훈 |"
local LEDGER_DIVIDER = "| :--- | :--- | :--- | :--- | :--- | :--- | :--- |"
local MAX_RECENT = 8       -- 사이드카 입력 윈도우
local MAX_LESSON_ROWS = 50 -- 회상 주입 상한(행)
local MAX_LESSON_CHARS = 6000

-- 세션별 최근 대화 윈도우 / 세션 내 중복 교훈 가드
local recent = {}   -- sid -> { {role=, text=}, ... }
local recorded = {} -- sid -> { [lessonKey]=true }

-- ds-cortex core에서 이식한 SIDECAR_SYSTEM_PROMPT (결정 규칙·성공 확정 정의·환각 금지 동일)
local SIDECAR_PROMPT = [[당신은 개발 대화 로그를 분석하는 전용 분석기다. 코딩이나 도구 호출은 절대 하지 말 것.
입력으로 최근 대화 일부가 주어진다. 아래를 판단해 JSON "하나"로만 출력하라. 설명·마크다운·코드펜스 금지.

출력 형태:
{"lesson": <교훈 객체 또는 null>, "skill": <스킬 객체 또는 null>}

[결정 규칙 — 반드시 이대로 분류]
1) "성공이 확정"되고 재사용 가능한 구체적 기법이면 → lesson(outcome:"success")과 skill을 둘 다 채운다.
2) 실패/부분이 확정되면 → lesson(outcome:"fail" 또는 "partial")만 채우고 skill=null. (실패 접근은 스킬로 만들지 말 것)
3) 성패가 아직 확정 안 됐거나, 일반 상식·사소함이라 기록 가치가 없으면 → {"lesson":null,"skill":null}.

[성공 확정의 정의 — 중요]
- "성공 확정"은 사용자가 됐다고 확인했거나(예: '됐다/해결됨/통과') 대화에서 실제로 검증된 경우만 해당한다.
- 에이전트가 방금 해법을 제안·적용한 것만으로는 success가 아니다(아직 미확정). 이 경우 skill=null, lesson은 outcome 미확정이면 null.
- 즉 검증되지 않은 제안을 스킬로 만들지 마라. 스킬은 "이미 먹힌 게 확인된" 기법만.

[근거 규칙 — 환각·과적합 금지]
- 대화에서 실제로 일어나고 확인된 사실에만 근거하라. 시도하지 않은 단계, 확인되지 않은 원인·해결책을 지어내지 마라.
- 확정 안 된 건 outcome에 반영하거나 아예 기록하지 마라.
- 특정 프로젝트·환경에만 해당하면 그 조건을 lesson에 명시하고 일반화하지 마라.

형식:
- lesson: {"task":"어떤 작업","approach":"어떻게 시도","outcome":"success|fail|partial","lesson":"왜 그 결과가 났고 다음에 뭘 해야/피해야 하나","category":"디버깅|설정|구현|리팩터링|분석|기타"}
- skill:  {"name":"영문 소문자+언더스코어","trigger":"이 스킬이 필요한 상황","technique":"구체적 해결 기법","description":"트리거 문장"}
  · description은 무엇을+언제를 한 문장에 담은 3인칭 트리거 문장으로, 키워드·에러명·파일명을 앞쪽에 배치한다.
  · 병합 우선: 입력 끝에 [기존 스킬] 목록이 주어지면, 겹치거나 정제 가능한 경우 그 name을 재사용해 병합본을 출력하라. 무관한 새 기법일 때만 새 name.

민감정보 제거(필수, 기록은 git으로 팀 공유됨): 절대경로→상대화, API키/토큰→종류만, IP/도메인/포트→역할, 이메일/실명→역할, 비밀번호/환경변수 값→변수명.]]

-- 결과 게이트: OpenCode 어댑터의 키워드 휴리스틱(outcomeGate) 대신, magi 호스트가
-- turn_finished 페이로드에 실어주는 **구조적 판정**을 쓴다 — 호스트는 턴의 성패를
-- 실측으로 안다(카운슬 합의/UNVERIFIED 착지/가드 강제정지/에러). 사용자 발화에서
-- 성패를 추측할 필요가 없다.
--   verified   → 카운슬이 증거를 보고 done에 합의한 완료 (성공 확정의 최상급 근거)
--   unverified → 착지는 했지만 카운슬이 끝내 승인 안 함 (실패/부분 후보)
--   guard      → loop/stall 가드 강제정지 (실패 확정)
--   error      → 에러로 종료 (실패 확정)
--   done       → 카운슬 판정 없는 평이한 종료(잡담 등) → 분석 안 함
local ANALYZE_OUTCOMES = { verified = true, unverified = true, guard = true, error = true }

local function outcome_hint(outcome, reason)
  local desc = {
    verified = "카운슬(3인 합의 게이트)이 실행 증거를 보고 완료를 승인했다 — 성공 확정",
    unverified = "턴은 끝났지만 카운슬이 끝내 승인하지 않았다(UNVERIFIED) — 실패 또는 부분",
    guard = "무진전 가드가 강제 정지시켰다 — 실패 확정",
    error = "에러로 종료됐다 — 실패 확정",
  }
  local h = "[호스트 판정] outcome=" .. outcome .. " — " .. (desc[outcome] or outcome)
  if reason and reason ~= "" then h = h .. " (사유: " .. reason .. ")" end
  return h .. "\n이 판정은 호스트의 실측이다. lesson.outcome은 이 판정과 모순되게 쓰지 마라."
end

local function sanitize_cell(v)
  v = tostring(v or "")
  v = string.gsub(v, "|", "\\|")
  v = string.gsub(v, "\r?\n", " ")
  return (string.gsub(v, "^%s*(.-)%s*$", "%1"))
end

local function outcome_label(o)
  if o == "success" then return "✅ 성공" end
  if o == "fail" then return "❌ 실패" end
  if o == "partial" then return "⚠️ 부분" end
  return o or "-"
end

local function slugify(name)
  local s = string.lower(tostring(name or ""))
  s = string.gsub(s, "[^a-z0-9_%-]+", "_")
  s = string.gsub(s, "^[_%-]+", "")
  s = string.gsub(s, "[_%-]+$", "")
  if s == "" then s = "skill" end
  return string.sub(s, 1, 64)
end

local function lesson_key(task, approach)
  local k = string.lower((task or "") .. "|" .. (approach or ""))
  return (string.gsub(k, "%s+", " "))
end

-- LLM 출력에서 JSON 오브젝트 추출(코드펜스/잡담 허용: 첫 { ~ 마지막 })
local function parse_cortex_json(raw)
  if not raw then return nil end
  local s = string.find(raw, "{", 1, true)
  local e = nil
  for i = #raw, 1, -1 do
    if string.sub(raw, i, i) == "}" then e = i break end
  end
  if not s or not e or e <= s then return nil end
  local obj = magi.json_decode(string.sub(raw, s, e))
  return obj
end

local function push_recent(sid, role, text)
  if not text or text == "" then return end
  local r = recent[sid]
  if not r then r = {} recent[sid] = r end
  r[#r + 1] = { role = role, text = string.sub(text, 1, 4000) }
  while #r > MAX_RECENT do table.remove(r, 1) end
end

local function render_window(r)
  local parts = {}
  for _, m in ipairs(r) do
    parts[#parts + 1] = "[" .. m.role .. "]\n" .. m.text
  end
  return table.concat(parts, "\n\n---\n\n")
end

-- 스킬 인덱스(병합 판단용 다이제스트)는 플러그인 스토어에 name<TAB>description 행으로 유지
-- (샌드박스에 디렉토리 나열이 없어 파일 시스템 스캔 대신 자체 인덱스를 쓴다)
local function skills_digest()
  local idx = magi.store_get("skill_index")
  if not idx or idx == "" then return "" end
  local lines = {}
  for line in string.gmatch(idx, "[^\n]+") do
    local name, desc = string.match(line, "^([^\t]*)\t(.*)$")
    if name then lines[#lines + 1] = "- " .. name .. ": " .. desc end
  end
  if #lines == 0 then return "" end
  return "[기존 스킬]\n" .. table.concat(lines, "\n")
end

local function index_skill(slug, desc)
  local idx = magi.store_get("skill_index") or ""
  local out = {}
  for line in string.gmatch(idx, "[^\n]+") do
    local name = string.match(line, "^([^\t]*)\t")
    if name ~= slug then out[#out + 1] = line end
  end
  out[#out + 1] = slug .. "\t" .. string.gsub(desc or "", "\n", " ")
  magi.store_set("skill_index", table.concat(out, "\n"))
end

local function append_lesson(entry)
  local existing = magi.read_file(SUMMARY) or ""
  local row = {
    sanitize_cell(entry.timestamp), sanitize_cell(entry.username),
    sanitize_cell(entry.category), sanitize_cell(entry.task),
    sanitize_cell(entry.approach), sanitize_cell(outcome_label(entry.outcome)),
    sanitize_cell(entry.lesson),
  }
  -- 디스크 중복 가드: 작업+접근+결과 시그니처가 이미 있으면 스킵
  local signature = row[4] .. " | " .. row[5] .. " | " .. row[6]
  if string.find(existing, signature, 1, true) then return false end
  local content = string.gsub(existing, "%s+$", "")
  if not string.find(content, LEDGER_HEADER, 1, true) then
    content = LEDGER_TITLE .. "\n\n" .. LEDGER_HEADER .. "\n" .. LEDGER_DIVIDER
  end
  content = content .. "\n| " .. table.concat(row, " | ") .. " |\n"
  magi.write_file(SUMMARY, content)
  return true
end

local function save_skill(skill)
  local slug = slugify(skill.name)
  local desc = tostring(skill.description or skill.technique or "")
  local body = {
    "---",
    "name: " .. slug,
    'description: "' .. string.gsub(string.gsub(desc, '"', '\\"'), "\r?\n", " ") .. '"',
    "---",
    "",
    "# " .. tostring(skill.name),
    "",
  }
  local when = tostring(skill.trigger or "")
  if when ~= "" then
    body[#body + 1] = "이 스킬은 **" .. when .. "** 상황에 적용합니다."
    body[#body + 1] = ""
  end
  body[#body + 1] = "## 적용 방법"
  body[#body + 1] = tostring(skill.technique or "")
  body[#body + 1] = ""
  body[#body + 1] = "<!-- engram: 작업 이력에서 자동 추출된 스킬 -->"
  magi.write_file(SKILLS_DIR .. "/" .. slug .. "/SKILL.md", table.concat(body, "\n"))
  index_skill(slug, desc)
  return slug
end

-- 캡처: 결과가 확정된 턴 → 사이드카 분석 → 교훈 기록 + 검증된 스킬 저장.
-- 이 핸들러는 호스트의 관찰 워커에서 돌므로 느린 사이드카가 턴을 막지 않는다.
local function analyze_and_record(sid, hint)
  local r = recent[sid]
  if not r or #r < 2 then return end -- 직전 시도가 될 맥락 부족
  local input = render_window(r)
  if hint and hint ~= "" then input = input .. "\n\n" .. hint end
  local digest = skills_digest()
  if digest ~= "" then input = input .. "\n\n" .. digest end
  local model = magi.store_get("sidecar_model") -- [plugins.engram] sidecar_model 오버라이드
  local raw, err = magi.analyze{ system = SIDECAR_PROMPT, text = input, model = model }
  if not raw then
    magi.log("engram: 사이드카 실패(무시): " .. tostring(err))
    return
  end
  local result = parse_cortex_json(raw)
  if not result then
    magi.log("engram: 사이드카 응답 파싱 실패(무시)")
    return
  end
  if not result.lesson and not result.skill then
    magi.log("engram: 기록할 교훈/스킬 없음(사소하거나 미확정)")
    return
  end

  local lesson = result.lesson
  if lesson and lesson.task and lesson.lesson then
    local outcome = tostring(lesson.outcome or "none")
    if outcome ~= "none" then
      local key = lesson_key(lesson.task, lesson.approach)
      local seen = recorded[sid]
      if not seen then seen = {} recorded[sid] = seen end
      if not seen[key] then
        seen[key] = true
        local ok = append_lesson{
          timestamp = magi.time(), username = "user",
          category = tostring(lesson.category or "기타"),
          task = tostring(lesson.task), approach = tostring(lesson.approach or ""),
          outcome = outcome, lesson = tostring(lesson.lesson),
        }
        if ok then magi.log("engram: 교훈 기록 — " .. tostring(lesson.task)) end
      end
    end
  end

  -- 검증된 성공의 스킬만 저장 (사이드카 프롬프트의 결정 규칙이 1차 게이트)
  local skill = result.skill
  if skill and skill.name and skill.technique then
    local slug = save_skill(skill)
    magi.log("engram: 스킬 저장 — " .. slug)
  end
end

magi.on("user_message", function(ev)
  push_recent(ev.session, "user", ev.text)
end)

magi.on("turn_finished", function(ev)
  push_recent(ev.session, "assistant", ev.text)
  local outcome = ev.outcome or "done"
  if ANALYZE_OUTCOMES[outcome] then
    analyze_and_record(ev.session, outcome_hint(outcome, ev.reason))
  end
end)

-- 회상: 최근 교훈을 컨텍스트로 주입(원장이 없으면 침묵). 행/문자 상한으로 바운드.
magi.register_context_provider{
  name = "engram-lessons",
  provide = function(q)
    local raw = magi.read_file(SUMMARY)
    if not raw or raw == "" then return {} end
    local rows = {}
    for line in string.gmatch(raw, "[^\n]+") do
      local t = string.gsub(line, "^%s+", "")
      if string.sub(t, 1, 1) == "|" and not string.find(t, "일시 | 사용자", 1, true)
        and not string.match(t, "^|%s*:?%-%-") then
        rows[#rows + 1] = t
      end
    end
    if #rows == 0 then return {} end
    local from = math.max(1, #rows - MAX_LESSON_ROWS + 1)
    local body = table.concat(rows, "\n", from)
    if #body > MAX_LESSON_CHARS then
      body = string.sub(body, #body - MAX_LESSON_CHARS + 1)
      local nl = string.find(body, "\n", 1, true)
      if nl then body = string.sub(body, nl + 1) end
    end
    local block = table.concat({
      "[DS-CORTEX — 이 저장소에 축적된 과거 작업 교훈입니다.]",
      "- 현재 작업과 상황이 실제로 일치할 때만 참고하세요. 무관하면 무시하세요.",
      "- 일치하는 교훈을 근거로 쓸 때는 누가·언제·어떤 결과(❌/✅)였는지를 함께 밝히세요.",
      "- 동일 상황에서 ❌실패로 기록된 접근은 피하세요.",
      "",
      LEDGER_HEADER, LEDGER_DIVIDER, body,
    }, "\n")
    return { { text = block, source = "engram" } }
  end,
}

magi.log("engram: 플러그인 로드 완료 (관찰 이벤트 + 사이드카 분석 + 회상)")
