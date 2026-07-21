-- engram — 자기개선 관찰자 플러그인 (관찰 이벤트 기반 교훈/스킬 학습).
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

-- 사이드카 분석기 시스템 프롬프트 (결정 규칙·성공 확정 정의·환각 금지)
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
- lesson에는 사용자 지시문의 재진술("~하면 된다"식 요구사항 되풀이)을 넣지 마라 — 검증된 인과(왜 그 결과가 났는가)와 재사용 가능한 결론만.
- 입력 끝에 [기존 교훈] 목록이 주어지면: 실질적으로 같은 교훈이 이미 있을 때 lesson=null (표현만 바뀐 재기록 금지 — 특히 회상된 지식을 확인만 한 턴).

형식:
- lesson: {"task":"어떤 작업","approach":"어떻게 시도","outcome":"success|fail|partial","lesson":"왜 그 결과가 났고 다음에 뭘 해야/피해야 하나","category":"디버깅|설정|구현|리팩터링|분석|기타"}
- skill:  {"name":"영문 소문자+언더스코어","trigger":"이 스킬이 필요한 상황","technique":"구체적 해결 기법","description":"트리거 문장","verify":"(선택) 적용 후 성공을 확인하는 커맨드나 체크","avoid":"(선택) 이 스킬을 쓰면 안 되는 상황·알려진 함정(관련 ❌실패 교훈이 있으면 여기에 병합)"}
  · description은 무엇을+언제를 한 문장에 담은 3인칭 트리거 문장으로, 키워드·에러명을 앞쪽에 배치한다.
  · trigger/description에 이번 작업 고유의 파일명·경로·프로젝트명을 넣지 마라 — 일반 조건으로 써라(예: "json_output.py 실행 시" ✗ → "python json.dumps 출력에서 한글이 \uXXXX로 이스케이프될 때" ✓). 단 에러 메시지 원문·라이브러리명·옵션명은 그대로 포함하라(트리거 매칭의 핵심).
  · technique에는 실제 실행돼 검증된 커맨드/코드 조각을 패러프레이즈 없이 그대로 담아라 — 요약하면 정확한 식별자(옵션명·플래그)가 유실된다.
  · 병합 우선(진화): 입력 끝의 [기존 스킬] 본문이 그 스킬의 현재 최신 상태다(사람이 직접 고쳤을 수 있다). 같은 주제를 다시 다룬다면 그 name을 재사용하고, 기존 본문을 baseline으로 삼아 이번에 새로 확인된 것만 더하거나 다듬어라 — 기존 본문에 이미 담긴 내용(특히 사람이 추가·수정한 지침)을 삭제하거나 되돌리지 마라. 무관한 새 기법일 때만 새 name.
  · 사용한 스킬 복제 금지(중요): [기존 스킬] 중 **[이번 턴 사용됨]**으로 표시된 것은 이번 작업에서 실제로 로드·적용된 스킬이다. 이번 성공이 그 스킬을 **써서** 난 것이라면, 그것과 사실상 같은 기법을 이름만 바꿔 새 스킬로 만들지 마라 — 그 스킬은 이미 있고 이번에 다시 먹혔음이 확인됐을 뿐이며, 복제는 유사 스킬 난립의 원인이다. 그 스킬에 이번에 새로 다듬을 게 있으면 그 name을 그대로 재사용하고, 딱히 없으면 skill=null. 진짜로 그 스킬과 무관한 별개 기법일 때만 새 스킬을 만들어라.

민감정보 제거(필수, 기록은 git으로 팀 공유됨): 절대경로→상대화, API키/토큰→종류만, IP/도메인/포트→역할, 이메일/실명→역할, 비밀번호/환경변수 값→변수명.]]

-- 결과 게이트: 사용자 발화 키워드 휴리스틱으로 성패를 추측하는 대신, magi 호스트가
-- turn_finished 페이로드에 실어주는 **구조적 판정**을 쓴다 — 호스트는 턴의 성패를
-- 실측으로 안다(카운슬 합의/UNVERIFIED 착지/가드 강제정지/에러). 사용자 발화에서
-- 성패를 추측할 필요가 없다.
--   verified   → 카운슬이 증거를 보고 done에 합의한 완료 (성공 확정의 최상급 근거)
--   unverified → 착지는 했지만 카운슬이 끝내 승인 안 함 (실패/부분 후보)
--   guard      → loop/stall 가드 강제정지 (실패 확정)
--   error      → 에러로 종료 (실패 확정)
--   ungated    → 실작업(툴 사용) 턴인데 카운슬 게이트가 아예 안 돌았다(카운슬 비활성/워크플로/하위깊이)
--                → 완료가 검증되지 않음. 사용자 확인 없이 성공으로 기록하지 마라
--   done       → 카운슬 판정 없는 평이한 종료(잡담 등) → 분석 안 함
local ANALYZE_OUTCOMES = { verified = true, unverified = true, guard = true, error = true, ungated = true }

local function outcome_hint(outcome, reason)
  local desc = {
    verified = "카운슬(3인 합의 게이트)이 실행 증거를 보고 완료를 승인했다 — 성공 확정",
    unverified = "턴은 끝났지만 카운슬이 끝내 승인하지 않았다(UNVERIFIED) — 실패 또는 부분",
    guard = "무진전 가드가 강제 정지시켰다 — 실패 확정",
    error = "에러로 종료됐다 — 실패 확정",
    ungated = "실작업 턴인데 검증 게이트가 돌지 않았다 — 완료 미검증. 사용자가 확인하지 않았다면 성공으로 기록하지 마라",
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
-- SKILL_BODY_CAP bounds one skill's body in the digest. Skills are authored SHORT
-- (save_skill keeps them terse; long verify procedures go to a side script), so the
-- FULL current SKILL.md fits — the sidecar must see the file's actual current text
-- (including a HUMAN edit) as the baseline to evolve from, not a stale one-line index.
local SKILL_BODY_CAP = 2000

-- used_now is a set {slug=true} of skills LOADED THIS TURN (from turn_finished ev.skills), so
-- the digest can mark them [이번 턴 사용됨]. That closes the duplicate-skill loop the user hit:
-- a turn that SUCCEEDED BY USING skill X is confirmation X works, not grounds to extract an
-- X-prime under a new name — the sidecar must see which skills the success actually leaned on.
local function skills_digest(used_now)
  local idx = magi.store_get("skill_index")
  if not idx or idx == "" then return "" end
  -- 사용 실적을 병합 판단 근거로 함께 노출(로드/성공/실패).
  local usage = {}
  for line in string.gmatch(magi.store_get("skill_usage") or "", "[^\n]+") do
    local n, l, o, b = string.match(line, "^([^\t]*)\t(%d+)\t(%d+)\t(%d+)$")
    if n then usage[n] = " (로드 " .. l .. "·성공 " .. o .. "·실패 " .. b .. ")" end
  end
  local blocks = {}
  for line in string.gmatch(idx, "[^\n]+") do
    local name, desc = string.match(line, "^([^\t]*)\t(.*)$")
    if name then
      -- The CURRENT on-disk body is the baseline: a human edit to SKILL.md becomes the
      -- starting point of the next extraction, so refinement builds on it rather than
      -- reverting it. Fall back to the one-line index if the file is unreadable.
      local body = magi.read_file(SKILLS_DIR .. "/" .. name .. "/SKILL.md")
      local mark = ""
      if used_now and used_now[name] then mark = " **[이번 턴 사용됨]**" end
      local header = "### " .. name .. (usage[name] or "") .. mark
      if body and body ~= "" then
        if #body > SKILL_BODY_CAP then body = string.sub(body, 1, SKILL_BODY_CAP) .. "\n…(생략)" end
        blocks[#blocks + 1] = header .. "\n" .. body
      else
        blocks[#blocks + 1] = header .. ": " .. desc
      end
    end
  end
  if #blocks == 0 then return "" end
  return "[기존 스킬] (아래 본문이 현재 최신 상태다 — 이를 baseline으로 개선/병합하되, 이미 담긴 내용을 되돌리지 마라)\n\n"
    .. table.concat(blocks, "\n\n")
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

-- 원장 행 파싱: "|" 구분 컬럼(공백 트림). Lua 패턴에 lazy 빈 캡처 함정이 있어
-- 비지 않은 세그먼트만 뽑는다: | 일시 | 사용자 | 분류 | 작업 | 접근 | 결과 | 교훈 |
local function ledger_rows()
  local raw = magi.read_file(SUMMARY)
  if not raw or raw == "" then return {} end
  local rows = {}
  for line in string.gmatch(raw, "[^\n]+") do
    local t = string.gsub(line, "^%s+", "")
    if string.sub(t, 1, 1) == "|" and not string.find(t, "일시 | 사용자", 1, true)
      and not string.match(t, "^|%s*:?%-%-") then
      local cols = {}
      for c in string.gmatch(t, "[^|]+") do
        cols[#cols + 1] = string.gsub(c, "^%s*(.-)%s*$", "%1")
      end
      if #cols >= 7 then rows[#rows + 1] = cols end
    end
  end
  return rows
end

-- 기존 교훈 다이제스트(최근 N행의 작업/접근/교훈 요약) — 사이드카가 "이미 기록된
-- 교훈"을 알고 재기록을 거부하게 한다(회상→재기록 에코 루프 차단).
local function lessons_digest()
  local rows = ledger_rows()
  if #rows == 0 then return "" end
  local out = {}
  for i = math.max(1, #rows - 9), #rows do
    local c = rows[i]
    out[#out + 1] = "- " .. c[4] .. " / " .. c[5] .. " → " .. string.sub(c[7], 1, 120)
  end
  return "[기존 교훈]\n" .. table.concat(out, "\n")
end

-- 결정론적 유사-중복 백스톱: LLM의 "동일하면 null" 규칙은 소프트해서 뚫린다(실측).
-- 새 교훈의 토큰 집합이 기존 어느 행의 교훈과 과반 이상 겹치면 중복으로 스킵한다.
local function tokenize(s)
  local set, n = {}, 0
  for w in string.gmatch(string.lower(tostring(s or "")), "[%w가-힣_%-=]+") do
    if #w >= 2 and not set[w] then set[w] = true n = n + 1 end
  end
  return set, n
end

local function lesson_is_duplicate(lesson_text)
  local a, na = tokenize(lesson_text)
  if na == 0 then return false end
  for _, c in ipairs(ledger_rows()) do
    local b, nb = tokenize(c[7])
    if nb > 0 then
      local inter = 0
      for w in pairs(a) do
        if b[w] then inter = inter + 1 end
      end
      if inter / math.min(na, nb) >= 0.6 then return true end
    end
  end
  return false
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

-- YAML 큰따옴표 스칼라 이스케이프: 백슬래시를 먼저(이중 이스케이프 방지), 그 다음
-- 따옴표. "\uXXXX" 같은 원문이 진짜 YAML 파서(다른 호스트)에서 유니코드 이스케이프로
-- 오해석되지 않게 한다.
local function yaml_quote(s)
  s = string.gsub(tostring(s or ""), "\\", "\\\\")
  s = string.gsub(s, '"', '\\"')
  s = string.gsub(s, "\r?\n", " ")
  return '"' .. s .. '"'
end

local function save_skill(skill)
  local slug = slugify(skill.name)
  local desc = tostring(skill.description or skill.technique or "")
  local body = {
    "---",
    "name: " .. slug,
    "description: " .. yaml_quote(desc),
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
  local verify = tostring(skill.verify or "")
  if verify ~= "" then
    body[#body + 1] = "## 검증"
    if #verify > 200 then
      -- 긴 검증 절차는 부속 스크립트로 분리 — magi가 번들 리소스 매니페스트로
      -- 스킬 디렉토리 파일 목록을 본문에 노출하므로 상대 참조가 해석된다.
      magi.write_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh", verify)
      body[#body + 1] = "scripts/verify.sh 를 실행해 확인한다."
    else
      body[#body + 1] = verify
    end
    body[#body + 1] = ""
  end
  local avoid = tostring(skill.avoid or "")
  if avoid ~= "" then
    body[#body + 1] = "## 주의 (쓰면 안 되는 경우 / 알려진 함정)"
    body[#body + 1] = avoid
    body[#body + 1] = ""
  end
  body[#body + 1] = "<!-- engram: 작업 이력에서 자동 추출된 스킬 -->"
  magi.write_file(SKILLS_DIR .. "/" .. slug .. "/SKILL.md", table.concat(body, "\n"))
  index_skill(slug, desc)
  return slug
end

-- 캡처: 결과가 확정된 턴 → 사이드카 분석 → 교훈 기록 + 검증된 스킬 저장.
-- 이 핸들러는 호스트의 관찰 워커에서 돌므로 느린 사이드카가 턴을 막지 않는다.
local function analyze_and_record(sid, hint, user, used_csv)
  local r = recent[sid]
  if not r or #r < 2 then return end -- 직전 시도가 될 맥락 부족
  local input = render_window(r)
  if hint and hint ~= "" then input = input .. "\n\n" .. hint end
  local ld = lessons_digest()
  if ld ~= "" then input = input .. "\n\n" .. ld end
  -- Skills loaded THIS turn → mark them in the digest so the sidecar won't clone a skill the
  -- success actually came from using (the duplicate-skill loop).
  local used = nil
  if used_csv and used_csv ~= "" then
    used = {}
    for name in string.gmatch(used_csv, "[^,]+") do used[name] = true end
  end
  local digest = skills_digest(used)
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
        if lesson_is_duplicate(lesson.lesson) then
          magi.log("engram: 유사 교훈 이미 기록됨 — 스킵: " .. tostring(lesson.task))
        else
          local ok = append_lesson{
            timestamp = magi.time(),
            username = magi.store_get("username") or user or "user",
            category = tostring(lesson.category or "기타"),
            task = tostring(lesson.task), approach = tostring(lesson.approach or ""),
            outcome = outcome, lesson = tostring(lesson.lesson),
          }
          if ok then
            magi.log("engram: 교훈 기록 — " .. tostring(lesson.task))
            -- D13 공유 경험 스토어에도 제안(리뷰 큐 → 팀 tier). 스토어가 없거나
            -- 실패해도 무시 — 원장이 1차 저장소다.
            local _, perr = magi.propose_experience{
              memories = { {
                text = "[" .. tostring(lesson.category or "기타") .. "] " .. tostring(lesson.task)
                  .. " — " .. tostring(lesson.approach or "") .. " → " .. outcome .. ": " .. tostring(lesson.lesson),
                tags = { "engram", tostring(lesson.category or "기타") },
              } },
            }
            if perr then magi.log("engram: D13 제안 실패(무시): " .. tostring(perr)) end
          end
        end
      end
    end
  end

  -- 검증된 성공의 스킬만 저장 (사이드카 프롬프트의 결정 규칙이 1차 게이트)
  local skill = result.skill
  if skill and skill.name and skill.technique then
    local slug = save_skill(skill)
    magi.log("engram: 스킬 저장 — " .. slug)
    magi.store_set("last_skill", slug)
    magi.notify(sid, "engram: 검증된 성공에서 스킬 '" .. slug
      .. "' 을(를) 자동 저장했습니다. 잘못 저장됐으면 'N' 또는 '스킬 취소'라고 입력하면 되돌립니다.")
    local _, perr = magi.propose_experience{
      skills = { {
        name = slug,
        description = tostring(skill.description or ""),
        body = tostring(skill.technique or ""),
      } },
    }
    if perr then magi.log("engram: D13 스킬 제안 실패(무시): " .. tostring(perr)) end
  end
end

-- 짧은 부정 응답(자동 저장된 스킬 취소) 판정.
local DENIALS = {
  "^%s*[nN][oO]?%s*[.!]?%s*$", "^%s*아니요?%s*[.!]?%s*$", "^%s*아냐%s*[.!]?%s*$",
  "^%s*취소%s*[.!]?%s*$", "^%s*스킬%s*취소%s*[.!]?%s*$", "^%s*하지%s*마%s*[.!]?%s*$",
  "^%s*[pP][aA][sS][sS]%s*[.!]?%s*$", "^%s*[sS][kK][iI][pP]%s*[.!]?%s*$", "^%s*스킵%s*[.!]?%s*$",
}
local function is_denial(text)
  for _, pat in ipairs(DENIALS) do
    if string.match(tostring(text or ""), pat) then return true end
  end
  return false
end

-- 마지막 자동 저장 스킬의 취소(undo): 저장 직후 "다음 사용자 메시지"까지가 취소 창.
local function undo_last_skill(sid)
  local slug = magi.store_get("last_skill")
  if not slug or slug == "" then return false end
  magi.remove_file(SKILLS_DIR .. "/" .. slug)
  -- 인덱스에서도 제거
  local idx = magi.store_get("skill_index") or ""
  local out = {}
  for line in string.gmatch(idx, "[^\n]+") do
    if string.match(line, "^([^\t]*)\t") ~= slug then out[#out + 1] = line end
  end
  magi.store_set("skill_index", table.concat(out, "\n"))
  magi.store_set("last_skill", "")
  magi.notify(sid, "engram: 스킬 '" .. slug .. "' 저장을 취소(삭제)했습니다.")
  magi.log("engram: 스킬 취소 — " .. slug)
  return true
end

magi.on("user_message", function(ev)
  -- 취소 창: 직전에 자동 저장된 스킬이 있으면, 이 메시지가 짧은 부정이면 되돌리고
  -- 아니면 창을 닫는다(one-shot — 다음-턴 취소 시맨틱).
  local last = magi.store_get("last_skill")
  if last and last ~= "" then
    if is_denial(ev.text) then
      undo_last_skill(ev.session)
    else
      magi.store_set("last_skill", "")
    end
  end
  push_recent(ev.session, "user", ev.text)
end)

-- 스킬 사용 실적 원장(플러그인 스토어): slug\t로드수\t성공수\t실패수.
-- 호스트가 turn_finished 페이로드에 실어주는 "이번 턴에 로드된 스킬" ×
-- 구조적 outcome으로 결정론적으로 계측한다 — 큐레이션(병합/정리)의 실측 근거.
-- Last-used dates (YYYY-MM-DD per skill slug) drive date-based pruning. Stored as a
-- newline-joined "slug\tdate" map alongside skill_usage.
local function load_lastused()
  local m = {}
  for line in string.gmatch(magi.store_get("skill_lastused") or "", "[^\n]+") do
    local n, d = string.match(line, "^([^\t]*)\t(.*)$")
    if n then m[n] = d end
  end
  return m
end

local function save_lastused(m)
  local out = {}
  for n, d in pairs(m) do out[#out + 1] = n .. "\t" .. d end
  magi.store_set("skill_lastused", table.concat(out, "\n"))
end

-- days_between returns the whole-day difference between two YYYY-MM-DD strings
-- (a - b) using a proleptic-Gregorian day count. Returns nil on a malformed date.
local function to_days(ymd)
  local y, m, d = string.match(ymd or "", "^(%d+)-(%d+)-(%d+)$")
  if not y then return nil end
  y, m, d = tonumber(y), tonumber(m), tonumber(d)
  -- days since a fixed epoch (Howard Hinnant's civil algorithm)
  y = (m <= 2) and (y - 1) or y
  local era = math.floor(((y >= 0) and y or (y - 399)) / 400)
  local yoe = y - era * 400
  local doy = math.floor((153 * ((m > 2) and (m - 3) or (m + 9)) + 2) / 5) + d - 1
  local doe = yoe * 365 + math.floor(yoe / 4) - math.floor(yoe / 100) + doy
  return era * 146097 + doe - 719468
end

-- PRUNE_DAYS default; overridable per-user with /engram-prune-days (stored).
local PRUNE_DAYS_DEFAULT = 7

local function prune_days()
  local v = tonumber(magi.store_get("prune_days") or "")
  if v and v > 0 then return math.floor(v) end
  return PRUNE_DAYS_DEFAULT
end

-- prune_stale archives engram-authored skills unused for >= prune_days days: it MOVES
-- .claude/skills/<slug>/SKILL.md to .claude/skills/.archive/<slug>/SKILL.md (read →
-- write → remove — recoverable, git-tracked) and drops the slug from the index/usage/
-- lastused. HUMAN-edited skills are protected: a skill whose SKILL.md no longer carries
-- the engram auto-generated marker is never archived. Pure count/date logic — no LLM.
local ENGRAM_MARKER = "engram: 작업 이력에서 자동 추출된 스킬"

local function prune_stale()
  local today = to_days(string.sub(magi.time(), 1, 10))
  if not today then return end
  local cutoff = prune_days()
  local lastused = load_lastused()
  local idx = magi.store_get("skill_index") or ""
  local archived = {}
  for line in string.gmatch(idx, "[^\n]+") do
    local slug = string.match(line, "^([^\t]*)\t")
    if slug then
      local path = SKILLS_DIR .. "/" .. slug .. "/SKILL.md"
      local body = magi.read_file(path)
      if body and string.find(body, ENGRAM_MARKER, 1, true) then -- engram-owned only
        local ld = to_days(lastused[slug])
        -- A skill with no recorded last-used date is treated as stale by AGE only if it
        -- has never been loaded; but without a date we can't measure age, so skip it —
        -- it earns a date the first time it's loaded. Only archive dated-and-stale ones.
        if ld and (today - ld) >= cutoff then
          magi.write_file(SKILLS_DIR .. "/.archive/" .. slug .. "/SKILL.md", body)
          magi.remove_file(path)
          archived[slug] = true
        end
      end
    end
  end
  if next(archived) == nil then return end
  -- Drop archived slugs from the index, usage, and lastused maps.
  local function filter_lines(key, slugof)
    local kept = {}
    for line in string.gmatch(magi.store_get(key) or "", "[^\n]+") do
      local s = slugof(line)
      if s and not archived[s] then kept[#kept + 1] = line end
    end
    magi.store_set(key, table.concat(kept, "\n"))
  end
  filter_lines("skill_index", function(l) return string.match(l, "^([^\t]*)\t") end)
  filter_lines("skill_usage", function(l) return string.match(l, "^([^\t]*)\t") end)
  filter_lines("skill_lastused", function(l) return string.match(l, "^([^\t]*)\t") end)
  local names = {}
  for s in pairs(archived) do names[#names + 1] = s end
  magi.log("engram: " .. #names .. "개 미사용 스킬 아카이브(>" .. cutoff .. "일): " .. table.concat(names, ", "))
end

local function update_usage(skills_csv, outcome)
  if not skills_csv or skills_csv == "" then return end
  local map, order = {}, {}
  for line in string.gmatch(magi.store_get("skill_usage") or "", "[^\n]+") do
    local n, l, o, b = string.match(line, "^([^\t]*)\t(%d+)\t(%d+)\t(%d+)$")
    if n then map[n] = { tonumber(l), tonumber(o), tonumber(b) } order[#order + 1] = n end
  end
  local today = string.sub(magi.time(), 1, 10) -- YYYY-MM-DD (UTC), for last-used pruning
  local lastused = load_lastused()
  for name in string.gmatch(skills_csv, "[^,]+") do
    local m = map[name]
    if not m then m = { 0, 0, 0 } map[name] = m order[#order + 1] = name end
    m[1] = m[1] + 1
    if outcome == "verified" then m[2] = m[2] + 1
    elseif outcome == "unverified" or outcome == "guard" or outcome == "error" then m[3] = m[3] + 1 end
    lastused[name] = today -- loaded this turn → refresh its last-used date
  end
  save_lastused(lastused)
  local out = {}
  for _, n in ipairs(order) do
    local m = map[n]
    out[#out + 1] = n .. "\t" .. m[1] .. "\t" .. m[2] .. "\t" .. m[3]
  end
  magi.store_set("skill_usage", table.concat(out, "\n"))
end

magi.on("turn_finished", function(ev)
  push_recent(ev.session, "assistant", ev.text)
  local outcome = ev.outcome or "done"
  update_usage(ev.skills, outcome)
  if ANALYZE_OUTCOMES[outcome] then
    local user = ev.user
    if user == "" then user = nil end
    analyze_and_record(ev.session, outcome_hint(outcome, ev.reason), user, ev.skills)
  end
  prune_stale() -- archive engram-owned skills unused past the cutoff (count/date only)
end)

-- /engram-prune-days [N] — show or set the unused-skill archive cutoff (days). Human-
-- edited skills are never archived regardless of the cutoff.
magi.register_command{
  name = "engram-prune-days",
  description = "미사용 스킬 아카이브 기준 일수 조회/설정 (예: /engram-prune-days 14)",
  execute = function(args)
    local n = tonumber((args or ""):match("%d+"))
    if n and n > 0 then
      magi.store_set("prune_days", tostring(math.floor(n)))
      return "engram: 미사용 스킬 아카이브 기준을 " .. math.floor(n) .. "일로 설정했습니다."
    end
    return "engram: 현재 아카이브 기준 " .. prune_days() .. "일. 변경하려면 `/engram-prune-days <일수>`."
  end,
}

-- 회상: 최근 교훈을 컨텍스트로 주입(원장이 없으면 침묵). 행/문자 상한으로 바운드.
magi.register_context_provider{
  name = "engram-lessons",
  provide = function(q)
    local raw = magi.read_file(SUMMARY)
    if not raw or raw == "" then return {} end
    -- 관련성 게이트: 현재 질의와 토큰이 겹치는 교훈 행만 주입한다. 무관한 행(예:
    -- 세션 첫 턴의 교훈)이 매 스텝 실리면 모델 추론이 그 옛 요청을 계속 되뇐다
    -- (실측 버그 리포트). 질의가 짧아 겹침 판단이 무의미하면 최근 행으로 폴백.
    local qset, qn = tokenize(q.prompt or "")
    local rows = {}
    for line in string.gmatch(raw, "[^\n]+") do
      local t = string.gsub(line, "^%s+", "")
      if string.sub(t, 1, 1) == "|" and not string.find(t, "일시 | 사용자", 1, true)
        and not string.match(t, "^|%s*:?%-%-") then
        if qn >= 3 then
          -- 행 토큰이 질의 문자열의 부분문자열로 나타나는지로 겹침을 센다:
          -- 한국어 조사("포트가" vs "포트") 때문에 토큰 동등 비교는 오탐 필터링한다.
          local qtext = string.lower(q.prompt or "")
          local rset = tokenize(t)
          local inter = 0
          for w in pairs(rset) do
            if string.find(qtext, w, 1, true) then inter = inter + 1 end
          end
          if inter >= 2 then rows[#rows + 1] = t end
        else
          rows[#rows + 1] = t
        end
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
      "[ENGRAM — 이 저장소에 축적된 과거 작업 교훈입니다.]",
      "- 현재 작업과 상황이 실제로 일치할 때만 참고하세요. 무관하면 답변·사고 과정에서 언급 자체를 하지 마세요(이 기록을 요약하거나 되뇌지 말 것).",
      "- 일치하는 교훈을 근거로 쓸 때는 누가·언제·어떤 결과(❌/✅)였는지를 함께 밝히세요.",
      "- 동일 상황에서 ❌실패로 기록된 접근은 피하세요.",
      "",
      LEDGER_HEADER, LEDGER_DIVIDER, body,
    }, "\n")
    return { { text = block, source = "engram" } }
  end,
}

magi.log("engram: 플러그인 로드 완료 (관찰 이벤트 + 사이드카 분석 + 회상)")
