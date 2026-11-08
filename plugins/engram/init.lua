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

-- [plugins.engram] author_skills — 기본 true. "false"/"off"/"0" 이면 스킬을 짓지 않는다(교훈은 남긴다).
local function flag(key, default)
  local v = magi.store_get(key)
  if v == nil then return default end
  if type(v) == "boolean" then return v end  -- `false or "true"` 의 함정: 불리언은 or 로 못 받는다
  local sv = tostring(v):lower()
  return not (sv == "false" or sv == "off" or sv == "0")
end

local function authorSkills() return flag("author_skills", true) end

-- [plugins.engram] lessons — 기본 true. false 면 턴 끝의 사이드카 분석을 아예 안 돈다: 교훈도 스킬도 없다.
-- 사이드카 분석은 플러그인 락을 쥔 채 두 분 안팎을 돌고, 그동안 다음 턴의 컨텍스트 제공자가 그 락을
-- 기다린다 — 실물 2026-09-05(PPT): 턴이 끝난 뒤 큐의 지시가 되살아나기까지 2분이 빈 채였다.
local function recordLessons() return flag("lessons", true) end

-- 셸 스크립트로 보이는가: 한글 음절이 없고(UTF-8 EA B0 80 ~ ED 9E A3), 첫 비어 있지 않은 줄이
-- shebang 이거나 명령처럼 시작한다. 산문 절차를 .sh 로 쓰지 않기 위한 문지기(2026-09-05).
local function looksLikeShell(s)
  -- (Lua 5.1 에는 \x 이스케이프가 없다 — 10진 바이트로 적는다: EA B0 80 ~ ED 9E A3)
  if s:find("[\234-\237][\128-\191][\128-\191]") then return false end
  local first = s:match("^%s*([^\n]+)") or ""
  return first:match("^#!") ~= nil or first:match("^[%w_%./%-]+[%s$]") ~= nil or first:match("^[%w_%./%-]+$") ~= nil
end
local LEDGER_TITLE = "# 작업 이력 및 교훈 기록 (팀 공유)"
local LEDGER_HEADER = "| 일시 | 사용자 | 분류 | 작업 | 시도한 접근 | 결과 | 교훈 |"
local LEDGER_DIVIDER = "| :--- | :--- | :--- | :--- | :--- | :--- | :--- |"
-- 정정·아카이브 섹션: 교체되거나 밀려난 행이 삭제 대신 이 헤딩 아래로 내려간다(툼스톤).
-- 활성 표를 읽는 모든 경로(회상 주입·중복 판정·다이제스트)는 이 헤딩에서 멈춘다 —
-- 정정으로 밀려난 옛 주장이 계속 주입되고 새 기록을 중복으로 막던 것이 원장의 약점이었다.
local ARCHIVE_TITLE = "## 이전 기록 (정정·아카이브 — 회상에 주입되지 않음)"
local MAX_RECENT = 8        -- 사이드카 입력 윈도우
local MAX_LESSON_ROWS = 50  -- 회상 주입 상한(행)
local MAX_LESSON_CHARS = 6000
local MAX_ACTIVE_ROWS = 80  -- 활성 표 상한 — 초과분은 오래된 행부터 아카이브로 (스킬 prune과 대칭)

-- 세션별 최근 대화 윈도우 / 세션 내 중복 교훈 가드
local recent = {}   -- sid -> { {role=, text=}, ... }
local recorded = {} -- sid -> { [lessonKey]=true }
local embed_lease_touch = function() return false end

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
- lesson에는 사용자 지시문의 재진술("~하면 된다"식 요구사항 되풀이)을 넣지 마라. 관측한 변경과 결과를 쓰고, 원인이 확정되지 않았으면 그 한계를 명시하라.
- 입력 끝에 [기존 교훈] 목록이 주어지면: 실질적으로 같은 교훈이 이미 있을 때 lesson=null (표현만 바뀐 재기록 금지 — 특히 회상된 지식을 확인만 한 턴).

형식:
- lesson: {"task":"어떤 작업","approach":"어떻게 시도","outcome":"success|fail|partial","lesson":"왜 그 결과가 났고 다음에 뭘 해야/피해야 하나","category":"디버깅|설정|구현|리팩터링|분석|기타","replaces":<기존 교훈 행 번호 또는 null>}
  · change에는 이번에 바꾼 것을, evidence에는 대화에 실제로 있는 근거를 쓴다. 근거가 없으면 빈 문자열이다.
  · 성공한 턴은 특정 변경의 효과를 입증하지 않는다. 비교 실행이 없는 이 분석의 attribution은 confounded다. 인과가 확정됐다고 쓰지 마라.
  · replaces(정정/병합 — 중요): [기존 교훈] 목록의 행이 (n) 번호를 달고 온다. 이번 교훈이 그중 한 행과 **같은 주제의 정정·구체화·뒤집기**라면 새 행을 추가하지 말고 replaces에 그 번호를 넣어라 — 그 행이 이번 교훈으로 교체되고 옛 행은 아카이브로 내려간다. 특히 결과가 모순되게 뒤집혔으면(전에 ✅였는데 지금 ❌로 판명, 또는 반대) 반드시 replaces를 쓰고 lesson에 왜 뒤집혔는지를 담아라 — 낡은 주장과 새 주장이 나란히 남는 것이 이 원장의 최악의 상태다. 무관한 새 주제일 때만 replaces=null.
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
  v = string.gsub(v, "%z", "")
  v = string.gsub(v, "\1", "") -- ledger_cells의 이스케이프 마스크 바이트 — 셀이 위조하면 안 된다
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
-- 디스크가 진실이다. 이 목록은 오래도록 engram 자신의 장부(skill_index)에서만 나왔고, 그
-- 장부는 engram이 직접 쓴 스킬만 담는다 — 사람이 만든 스킬, 다른 컴패니언이 만들어 공유
-- 스토어로 넘어온 스킬, 장부가 생기기 전의 스킬은 디스크에 있으면서 사이드카 눈에는 없었다.
-- 없으니 "이미 있다"를 판단할 수 없고, 모델은 같은 기법을 새 이름으로 다시 만든다. 유사 스킬
-- 난립의 원인 중 프롬프트로는 절대 못 고치는 절반이 이것이었다.
--
-- 이제 SKILLS_DIR을 직접 나열하고, 장부는 설명 문구의 폴백으로만 남는다(디렉토리는 이름만
-- 주고 한 줄 설명은 안 주므로). 나열이 실패하면 예전처럼 장부로 돌아간다 — 목록이 없는 것보다
-- 불완전한 목록이 낫다.
local function skill_slugs()
  local seen, out = {}, {}
  for _, name in ipairs(magi.list_files(SKILLS_DIR) or {}) do
    local slug = string.match(name, "^(.+)/$") -- 디렉토리만: 스킬 하나 = 폴더 하나
    if slug and slug ~= ".archive" and not seen[slug] then
      seen[slug] = true
      out[#out + 1] = slug
    end
  end
  return out, seen
end

local function skills_digest(used_now)
  local idx = magi.store_get("skill_index") or ""
  -- 장부의 한 줄 설명을 슬러그로 색인해 둔다(본문을 못 읽을 때의 폴백).
  local desc_of = {}
  for line in string.gmatch(idx, "[^\n]+") do
    local n, d = string.match(line, "^([^\t]*)\t(.*)$")
    if n then desc_of[n] = d end
  end
  local slugs, on_disk = skill_slugs()
  -- 디스크에서 못 읽었으면 장부라도 쓴다.
  if #slugs == 0 then
    for n in pairs(desc_of) do
      if not on_disk[n] then slugs[#slugs + 1] = n end
    end
  end
  if #slugs == 0 then return "" end
  -- 사용 실적을 병합 판단 근거로 함께 노출(로드/성공/실패).
  local usage = {}
  for line in string.gmatch(magi.store_get("skill_usage") or "", "[^\n]+") do
    local n, l, o, b = string.match(line, "^([^\t]*)\t(%d+)\t(%d+)\t(%d+)$")
    if n then usage[n] = " (로드 " .. l .. "·성공 " .. o .. "·실패 " .. b .. ")" end
  end
  local blocks = {}
  for _, name in ipairs(slugs) do
    do
      local desc = desc_of[name] or ""
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

-- 원장을 활성부/아카이브부로 가른다. 아카이브 헤딩 아래는 툼스톤 — 어떤 읽기 경로에도
-- 실리지 않고, git과 사람에게만 남는 기록이다.
local function split_ledger(raw)
  raw = raw or ""
  local i = string.find(raw, ARCHIVE_TITLE, 1, true)
  if not i then return raw, "" end
  return string.sub(raw, 1, i - 1), string.sub(raw, i)
end

local function is_data_row(line)
  local t = string.gsub(line, "^%s+", "")
  return string.sub(t, 1, 1) == "|" and not string.find(t, "일시 | 사용자", 1, true)
    and not string.match(t, "^|%s*:?%-%-")
end

-- 원장 행의 판정과 분해는 이 한 곳이다: 다이제스트 번호·정정(replaces) 대상·상한 정리가
-- 전부 같은 행 집합을 봐야 한다(어긋나면 번호가 밀려 엉뚱한 행이 아카이브된다). "≥7컬럼
-- |행"만으로는 부족했다 — 사람이 산문 구역에 둔 7컬럼 표(주간 일정표)가 헤더째 원장으로
-- 읽혀 사이드카에 "기존 교훈"으로 나가고 정정·상한의 표적이 됐다(4라운드 검수, 실측).
-- 원장 행의 제1컬럼은 magi.time()의 RFC3339 일시다: 날짜꼴 첫 컬럼을 요구하면 사람의 표는
-- 형태로 갈라지고, 날짜 없는 행은 원장이 아니라 사람 몫으로 취급돼 어느 경로도 손대지
-- 않는다. 들여쓴 행의 선행 공백이 세그먼트 하나로 세리며 컬럼을 밀던 것도 트림으로 봉한다.
-- (Lua 패턴에 lazy 빈 캡처 함정이 있어 비지 않은 세그먼트만 뽑는다.)
local function ledger_cells(line)
  if not is_data_row(line) then return nil end
  -- 작성기(sanitize_cell)가 셀 안의 |를 \|로 이스케이프한다 — 분해도 같은 규약을 지킨다:
  -- \|에서 쪼개면 파이프를 품은 셀(셸 파이프라인이 흔한 내용이다)이 한 칸씩 밀려,
  -- 다이제스트가 엉뚱한 열을 교훈이라 소개하고 중복 판정이 엉뚱한 셀을 읽었다(5라운드).
  -- 자리표시 바이트로 가렸다가 셀 단위로 복원한다.
  local masked = string.gsub(string.gsub(line, "^%s+", ""), "\\|", "\1")
  local cols = {}
  for c in string.gmatch(masked, "[^|]+") do
    c = string.gsub(c, "^%s*(.-)%s*$", "%1")
    cols[#cols + 1] = string.gsub(c, "\1", "\\|")
  end
  if #cols < 7 then return nil end
  if not string.match(cols[1], "^%d%d%d%d%-%d%d%-%d%d") then return nil end
  return cols
end

-- 원장 파싱: | 일시 | 사용자 | 분류 | 작업 | 접근 | 결과 | 교훈 |
-- 활성부만 읽는다 — 정정으로 밀려난 옛 주장이 새 기록을 중복으로 막으면 안 된다.
local function ledger_rows()
  local active = split_ledger(magi.read_file(SUMMARY))
  if active == "" then return {} end
  local rows = {}
  for line in string.gmatch(active, "[^\n]+") do
    local cols = ledger_cells(line)
    if cols then rows[#rows + 1] = cols end
  end
  return rows
end

-- 기존 교훈 다이제스트(최근 N행의 작업/접근/교훈 요약) — 사이드카가 "이미 기록된
-- 교훈"을 알고 재기록을 거부하거나(같으면 null) 정정을 지목하게 한다(replaces=행 번호).
-- (n)은 활성 표에서의 진짜 행 번호다 — replaces가 이 번호로 교체 대상을 찾는다.
local function lessons_digest()
  local rows = ledger_rows()
  if #rows == 0 then return "" end
  local out = {}
  for i = math.max(1, #rows - 9), #rows do
    local c = rows[i]
    out[#out + 1] = "- (" .. i .. ") " .. c[4] .. " / " .. c[5] .. " [" .. c[6] .. "] → " .. string.sub(c[7], 1, 120)
  end
  return "[기존 교훈] (같은 주제의 정정·뒤집기면 replaces에 그 (n)을 넣어라)\n" .. table.concat(out, "\n")
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

-- Only new managed projections use this identity map. Legacy/independent D13
-- records are not adopted by matching their text. Persist requests BEFORE the
-- call; a retry reuses its operation ID even if the host saved before a crash.
local managed_versions = setmetatable({}, { __mode = "k" })
local function managed_state()
  local raw = magi.store_get("managed_experience_v1:" .. magi.workdir())
  if not raw or raw == "" then
    local state = { links = {}, objects = {}, queue = {} }
    managed_versions[state] = ""
    return state
  end
  local state = magi.json_decode(raw)
  if type(state) ~= "table" or type(state.links) ~= "table"
    or type(state.objects) ~= "table" or type(state.queue) ~= "table" then
    magi.log("engram: invalid managed identity map; refusing to replace it")
    return nil
  end
  managed_versions[state] = raw
  return state
end

local function persist_managed(state)
  local encoded = magi.json_encode(state)
  local ok, err = magi.store_compare_exchange("managed_experience_v1:" .. magi.workdir(), managed_versions[state] or "", encoded)
  if ok then managed_versions[state] = encoded
  else magi.log("engram: managed state not saved: " .. tostring(err)) end
  return ok
end

local function reconcile_managed_conflict(request)
  if type(magi.search_experience) ~= "function" then return false end

  -- Optimistic Retry Loop: Reconcile 도중 제3의 컴패니언이 끼어드는 Moving Target 문제 방어 (최대 3회)
  for attempt = 1, 3 do
    local hits = magi.search_experience{
      query = request.object_id,
      kind = request.kind or "",
      limit = 1,
    }
    if type(hits) ~= "table" or #hits == 0 then return false end
    local remote = hits[1]
    if not remote or remote.object_id ~= request.object_id or not remote.revision or remote.revision == "" then
      return false
    end

    local latest_revision = remote.revision

    -- 근거(evidence) 목록 병합 (중복 제거)
    local ev_map = {}
    local merged_ev = {}
    local function add_ev(list)
      if type(list) ~= "table" then return end
      for _, item in ipairs(list) do
        if type(item) == "table" then
          local key = tostring(item.session or "") .. ":" .. tostring(item.turn or "") .. ":" .. tostring(item.outcome or "")
          if not ev_map[key] then
            ev_map[key] = true
            merged_ev[#merged_ev + 1] = item
          end
        end
      end
    end
    add_ev(remote.evidence)
    add_ev(request.evidence)
    request.evidence = merged_ev

    if request.kind == "memory" then
      local remote_text = tostring(remote.text or (remote.memory and remote.memory.text) or "")
      local local_text = tostring(request.text or (request.memory and request.memory.text) or "")

      -- 1. Fast-Path: 본문 내용이 완전히 동일하면 LLM 호출 없이 revision만 전진시켜 즉시 커밋
      if remote_text == local_text then
        request.expected_revision = latest_revision
        request.operation_id = magi.nonce()
        local receipt, err = magi.apply_experience(request)
        if receipt then return receipt end
        -- 그사이 또 바뀌었으면 다음 루프로 재시도
      elseif remote_text ~= "" and local_text ~= "" then
        -- 2. 텍스트 충돌 발생: 사이드카 LLM(magi.analyze)으로 3자 의미 화해 시도
        -- 디테일 증발 방지(Compression Drift 방어) 가드레일 명시
        local synthesized = nil
        if type(magi.analyze) == "function" then
          local sys = "당신은 두 컴패니언의 기억/교훈 충돌을 해결하는 전용 화해기다. 지침에 따라 고유명사와 구체적 기술 엔티티를 무손실 보존한 단일 교훈 텍스트만 출력할 것."
          local prompt = "동일한 객체(" .. tostring(request.object_id) .. ")에 대해 서로 다른 기억이 충돌했습니다.\n\n" ..
            "[기존 저장된 기억 (Remote)]:\n" .. remote_text .. "\n\n" ..
            "[새로 기록하려는 기억 (Local)]:\n" .. local_text .. "\n\n" ..
            "지침:\n" ..
            "1. 무손실 보존(Lossless Entities): 포트 번호, CLI 옵션/명령어, 파일 경로, 에러 메시지 등 구체적 고유 명사와 기술 식별자는 단 하나도 누락하거나 일반화하지 마십시오.\n" ..
            "2. 삭제 의도 존중(Explicit Deletion): 한쪽에만 있던 내용이 다른 쪽에서 명시적으로 정정되거나 폐기된 것이라면 최신 검증된 문맥을 따르십시오.\n" ..
            "3. 모순 해결 기준(Conflict Resolution): 두 내용이 직접 충돌하면 구체적인 실행 결과나 검증 근거(Evidence)가 포함된 쪽을 채택하십시오.\n" ..
            "4. 두 기억의 사실과 유효한 관찰을 모두 포함하는 1~2문장의 단일 교훈으로 작성하십시오.\n" ..
            "5. 서론, 부가 설명, 마크다운 코드블록 없이 오직 합성된 텍스트 본문만 출력하십시오."
          local res, aerr = magi.analyze{system = sys, text = prompt}
          if res and type(res) == "string" and string.match(res, "%S") then
            local cleaned = string.gsub(res, "^```%w*\n?", "")
            cleaned = string.gsub(cleaned, "\n?```$", "")
            synthesized = string.match(cleaned, "^%s*(.-)%s*$")
          end
        end

        if synthesized and synthesized ~= "" then
          request.text = synthesized
          if type(request.memory) == "table" then request.memory.text = synthesized end
          request.status = "hypothesis"
          magi.log("engram: managed conflict resolved via semantic 3-way analysis for " .. request.object_id)
        else
          -- 3. Safe Fallback(LWW): 원격 텍스트를 일단 유지하되, 밀려난 로컬 텍스트를 유실하지 않도록
          -- status를 'conflicted'로 마킹하고 설명에 남겨 다음 유휴 드림(Dream) 단계에서 재화해하도록 보존
          request.text = remote_text
          if type(request.memory) == "table" then request.memory.text = remote_text end
          request.status = "conflicted"
          local unmerged_note = "[deferred_reconcile: local=" .. local_text .. "]"
          if not string.find(tostring(request.description or ""), "deferred_reconcile", 1, true) then
            request.description = (request.description and (request.description .. " ") or "") .. unmerged_note
          end
          magi.log("engram: managed conflict fallback: preserved remote text, merged evidence, and flagged conflicted for " .. request.object_id)
        end

        request.expected_revision = latest_revision
        request.operation_id = magi.nonce()
        local receipt, err = magi.apply_experience(request)
        if receipt then
          return receipt
        end
        magi.log("engram: managed conflict reconciliation retry " .. attempt .. " failed: " .. tostring(err))
      end
    else
      -- memory 외 kind의 경우
      request.expected_revision = latest_revision
      request.operation_id = magi.nonce()
      local receipt, err = magi.apply_experience(request)
      if receipt then return receipt end
    end
  end

  return false
end

local function flush_managed()
  local state = managed_state()
  if not state then return false end
  -- Round-robin by object, preserving operation order within each object. A
  -- group of repeatedly failing objects must not starve later unrelated work.
  local order, groups = {}, {}
  for _, request in ipairs(state.queue) do
    if not groups[request.object_id] then
      groups[request.object_id] = {}
      order[#order + 1] = request.object_id
    end
    local group = groups[request.object_id]
    group[#group + 1] = request
  end
  local start = 1
  for index, id in ipairs(order) do if id == state.last_attempt then start = index % #order + 1 end end
  local queue = {}
  for offset = 0, #order - 1 do
    for _, request in ipairs(groups[order[(start + offset - 1) % #order + 1]]) do queue[#queue + 1] = request end
  end
  state.queue = queue
  local i, attempts, blocked = 1, 0, {}
  while i <= #state.queue and attempts < 8 do
    local request = state.queue[i]
    if blocked[request.object_id] then
      i = i + 1
    else
      local object = state.objects[request.object_id]
      if not request.expected_revision then
        request.expected_revision = object.revision or ""
      end
      state.last_attempt = request.object_id
      if not persist_managed(state) then return false end
      local receipt, err = magi.apply_experience(request)
      attempts = attempts + 1
      if not receipt then
        local err_str = tostring(err or "")
        local resolved_receipt = false
        if string.find(err_str, "conflict", 1, true) then
          resolved_receipt = reconcile_managed_conflict(request)
        end
        if resolved_receipt then
          object.revision = resolved_receipt.revision
          table.remove(state.queue, i)
          if not persist_managed(state) then return false end
        else
          magi.log("engram: managed experience pending: " .. err_str)
          blocked[request.object_id] = true
          i = i + 1
        end
      else
        object.revision = receipt.revision
        table.remove(state.queue, i)
        if not persist_managed(state) then return false end
      end
    end
  end
  return #state.queue == 0
end

local function managed_payload(key)
  local state = managed_state()
  if not state then return nil end
  local id = state.links[key]
  local object = id and state.objects[id]
  return object and object.payload or nil
end

local function enqueue_managed(key, payload, old_key)
  local state = managed_state()
  if not state then return false end
  if #state.queue >= 256 then
    magi.log("engram: managed outbox is full; local change requires reconciliation")
    return false
  end
  local id = (old_key and state.links[old_key]) or state.links[key] or magi.nonce()
  state.links[key] = id
  local object = state.objects[id] or {}
  state.objects[id] = object
  object.payload = payload
  local request = {}
  for k, v in pairs(payload) do request[k] = v end
  request.object_id = id
  request.operation_id = magi.nonce()
  request.observation_id = magi.nonce()
  state.queue[#state.queue + 1] = request
  if not persist_managed(state) then return false end
  return flush_managed()
end

local function enqueue_managed_object(id, expected_revision, payload)
  local state = managed_state()
  if not state then return false, "" end
  if #state.queue >= 256 then
    magi.log("engram: managed outbox is full; local change requires reconciliation")
    return false, ""
  end
  local object = state.objects[id] or {}
  state.objects[id] = object
  local effective_rev = (object.revision and object.revision ~= "") and object.revision or expected_revision
  if effective_rev and effective_rev ~= "" then
    object.revision = effective_rev
  end
  object.payload = payload
  local request = {}
  for k, v in pairs(payload) do request[k] = v end
  request.object_id = id
  if effective_rev and effective_rev ~= "" then
    request.expected_revision = effective_rev
  end
  request.operation_id = magi.nonce()
  request.observation_id = magi.nonce()
  state.queue[#state.queue + 1] = request
  if not persist_managed(state) then return false, "" end
  local ok = flush_managed()
  return ok, (object and object.revision) or effective_rev or ""
end

-- append_lesson lands one lesson row. replaces(행 번호)가 유효하면 그 행을 **제자리 교체**한다:
-- 옛 행은 삭제가 아니라 아카이브 섹션으로 내려간다(사유 표기) — 정정된 주장과 새 주장이
-- 나란히 남는 것도, 정정이 흔적 없이 사라지는 것도 원장의 실패이기 때문이다. 활성 표가
-- 상한을 넘으면 오래된 행부터 같은 곳으로 내려간다.
local function append_lesson(entry, replaces)
  local active, archive = split_ledger(magi.read_file(SUMMARY))
  local row = {
    sanitize_cell(entry.timestamp), sanitize_cell(entry.username),
    sanitize_cell(entry.category), sanitize_cell(entry.task),
    sanitize_cell(entry.approach), sanitize_cell(outcome_label(entry.outcome)),
    sanitize_cell(entry.lesson),
  }
  -- 디스크 중복 가드: 작업+접근+결과 시그니처가 활성 표에 이미 있으면 스킵.
  -- (정정 경로는 예외 — 정정은 옛 행과 겹치는 것이 정상이다.)
  local signature = row[4] .. " | " .. row[5] .. " | " .. row[6]
  if not replaces and string.find(active, signature, 1, true) then return false end

  -- 활성부를 줄 단위로 해체해 데이터 행 위치를 잡는다. 빈 줄을 **보존**하는 분해다 —
  -- "[^\n]+"는 빈 줄을 삼켜, 수제 산문의 문단 나눔과 헤딩 앞 공백을 첫 기록에 전부
  -- 뭉개버렸다. 행 판정은 원장 파싱(ledger_rows)과 같은 ledger_cells 하나다 — 여기서 다른
  -- 기준을 쓰면 다이제스트의 (n)과 번호가 어긋나 replaces가 옆 행을 아카이브한다.
  local lines, data_at = {}, {}
  local rest = active
  while rest ~= "" do
    local nl = string.find(rest, "\n", 1, true)
    local line
    if nl then
      line = string.sub(rest, 1, nl - 1)
      rest = string.sub(rest, nl + 1)
    else
      line = rest
      rest = ""
    end
    lines[#lines + 1] = line
    if ledger_cells(line) then data_at[#data_at + 1] = #lines end
  end
  while #lines > 0 and lines[#lines] == "" do table.remove(lines) end
  if #data_at == 0 and not string.find(active, LEDGER_HEADER, 1, true) then
    -- 표가 없으면 표 골격을 **덧붙인다** — 통째 교체는 사람이 손으로 써 둔 산문(표 없는
    -- SESSION_SUMMARY.md)을 첫 교훈 기록에 통째로 날려버렸다.
    if #lines > 0 then lines[#lines + 1] = "" end
    lines[#lines + 1] = LEDGER_TITLE
    lines[#lines + 1] = ""
    lines[#lines + 1] = LEDGER_HEADER
    lines[#lines + 1] = LEDGER_DIVIDER
  end

  local function to_archive(line, why)
    if archive == "" then
      archive = ARCHIVE_TITLE .. "\n" .. LEDGER_HEADER .. "\n" .. LEDGER_DIVIDER .. "\n"
    end
    archive = string.gsub(archive, "%s*$", "") .. "\n" .. line .. " ← " .. why .. "\n"
  end

  local anchored = false
  local previous_row = nil
  if replaces and data_at[replaces] then
    -- 내용 앵커: 번호는 사이드카 호출 **전**의 다이제스트에서 왔고, 느린 호출 동안 다른
    -- 턴이나 사람이 원장을 고치면 번호가 밀린다(위치 주소의 숙명). 대상 행이 새 교훈과
    -- 토큰을 실제로 공유할 때만 정정으로 믿고, 아니면 일반 추가로 강등한다 — 엉뚱한 행을
    -- 아카이브하는 것보다 중복 한 줄이 낫다.
    local target = lines[data_at[replaces]]
    local a = tokenize(sanitize_cell(entry.task) .. " " .. sanitize_cell(entry.lesson))
    local inter = 0
    for w in pairs(tokenize(target)) do
      if a[w] then inter = inter + 1 end
    end
    if inter >= 2 then
      previous_row = target
      to_archive(target, "정정됨(" .. row[1] .. ")")
      table.remove(lines, data_at[replaces])
      anchored = true
    end
  end
  -- 정정이 일반 추가로 강등되는 순간, 정정이라서 면제했던 중복 가드 **둘 다** 다시 선다:
  -- 이 행은 어느 중복 검사도 통과한 적이 없다. 시그니처(작업|접근|결과)는 사이드카가 접근을
  -- 패러프레이즈하면 못 잡으므로, 교훈 텍스트의 토큰 유사도 백스톱(lesson_is_duplicate)까지
  -- 함께 세운다 — 강등된 채 그냥 덧붙이면 옛 주장과 미정정 새 주장이 나란히 남는다.
  if replaces and not anchored
    and (string.find(active, signature, 1, true) or lesson_is_duplicate(entry.lesson)) then
    -- 무음이면 안 된다: 정정이 여기서 죽으면 낡은 주장이 원장의 현재로 남는데, 로그가 없으면
    -- 그 손실을 아무도 못 본다. 다음 검증 턴이 번호를 제대로 짚으면 정정은 여전히 착지한다.
    magi.log("engram: 정정 강등+중복으로 미기록(대상 행 불일치) — " .. string.sub(sanitize_cell(entry.lesson), 1, 80))
    return false
  end
  local new_row = "| " .. table.concat(row, " | ") .. " |"
  lines[#lines + 1] = new_row

  -- 상한: 오래된 데이터 행부터 아카이브로. (원장 행만 — 사람이 쓴 다른 표는 건드리지 않는다)
  local n = 0
  for _, l in ipairs(lines) do
    if ledger_cells(l) then n = n + 1 end
  end
  local i = 1
  while n > MAX_ACTIVE_ROWS and i <= #lines do
    if ledger_cells(lines[i]) then
      to_archive(lines[i], "오래됨(상한 " .. MAX_ACTIVE_ROWS .. "행)")
      table.remove(lines, i)
      n = n - 1
    else
      i = i + 1
    end
  end

  local out = table.concat(lines, "\n") .. "\n"
  if archive ~= "" then out = out .. "\n" .. string.gsub(archive, "%s+$", "\n") end
  local written, err = magi.write_file(SUMMARY, out)
  if not written then
    magi.log("engram: lesson was not written: " .. tostring(err))
    return false
  end
  return true, new_row, previous_row
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

-- 결정론적 유사-스킬 백스톱. 사이드카의 "같은 주제면 name 재사용" 규칙은 소프트해서 뚫리고,
-- 뚫릴 때마다 유사 스킬이 하나 늘었다(실측 — 난립의 주범). 새 이름의 설명이 기존 스킬 설명과
-- 토큰 과반 이상 겹치면 생성을 막는다(save_skill이 nil 반환 — 쌍둥이 이름을 돌려주면
-- 취소 창(undo)이 멀쩡한 기존 스킬을 지울 수 있어 일부러 안 돌려준다).
local function similar_skill(slug, desc)
  local a, na = tokenize(desc)
  if na < 3 then return nil end
  for line in string.gmatch(magi.store_get("skill_index") or "", "[^\n]+") do
    local name, d = string.match(line, "^([^\t]*)\t(.*)$")
    if name and name ~= slug then
      local b, nb = tokenize(d)
      if nb > 0 then
        local inter = 0
        for w in pairs(a) do
          if b[w] then inter = inter + 1 end
        end
        if inter / math.min(na, nb) >= 0.7 then return name end
      end
    end
  end
  return nil
end

local function skill_indexed(slug)
  for line in string.gmatch(magi.store_get("skill_index") or "", "[^\n]+") do
    if string.match(line, "^([^\t]*)\t") == slug then return true end
  end
  return false
end

local function save_skill(skill)
  local slug = slugify(skill.name)
  local existing = magi.read_file(SKILLS_DIR .. "/" .. slug .. "/SKILL.md") or ""
  local snapshot = magi.store_get("managed_skill_snapshot:" .. magi.workdir() .. ":" .. slug)
  local verify_snapshot = magi.store_get("managed_verify_snapshot:" .. magi.workdir() .. ":" .. slug)
  local current_verify = magi.read_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh") or ""
  if existing ~= "" and ((snapshot and snapshot ~= "" and existing ~= snapshot)
    or (verify_snapshot and current_verify ~= verify_snapshot)
    or not snapshot or snapshot == "") then
    magi.log("engram: skill has independent edits; automatic overwrite refused: " .. slug)
    return nil
  end
  local desc = tostring(skill.description or skill.technique or "")
  if not skill_indexed(slug) then
    local twin = similar_skill(slug, desc)
    if twin then
      magi.log("engram: 유사 스킬 '" .. twin .. "' 존재 — '" .. slug .. "' 생성 스킵(난립 방지)")
      return nil
    end
  end
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
  -- **산문은 스크립트가 아니다.** 앞 판본은 200자를 넘는 verify 를 내용을 안 보고 scripts/verify.sh 로
  -- 썼다. 실물 2026-09-05(PPT 컴패니언): 사이드카가 「read_notes 로 고지문을 확인하고 read_slide 로
  -- 실측한다」는 한국어 문장을 verify 에 담았고, 그것이 verify.sh 가 되어 스킬 본문이 「실행해 확인한다」고
  -- 적혔다. 다음 런의 모델은 bash 로 그 파일을 돌리려 했고(exit 127), 카운슬은 실행 증거를 요구했고,
  -- 기억은 「검증 스크립트 실행 증거 없음」을 교훈으로 굳혔다 — 세 런에 걸쳐 되풀이됐다.
  -- 스크립트로 쓰는 것은 셸로 보이는 것뿐이다: 한글이 없고, 첫 줄이 shebang 이거나 명령처럼 시작한다.
  local asScript = verify ~= "" and #verify > 200 and looksLikeShell(verify)
  if verify ~= "" then
    body[#body + 1] = "## 검증"
    if asScript then
      -- 긴 검증 스크립트는 부속 파일로 분리 — magi가 번들 리소스 매니페스트로
      -- 스킬 디렉토리 파일 목록을 본문에 노출하므로 상대 참조가 해석된다.
      local written, err = magi.write_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh", verify)
      if not written then magi.log("engram: verification script not written: " .. tostring(err)); return nil end
      body[#body + 1] = "scripts/verify.sh 를 실행해 확인한다(셸 스크립트)."
    else
      body[#body + 1] = verify
    end
    body[#body + 1] = ""
  end
  if not asScript then
    -- 머지 저장이 부속 스크립트에서 인라인(또는 무검증)으로 돌아오면, 이전 판의 verify.sh는
    -- 본문이 더는 가리키지 않는 고아다 — 지운다. 취소 스냅샷은 저장 전에 떠 있으므로
    -- undo가 되살린다.
    local vpath = SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh"
    if magi.read_file(vpath) then magi.remove_file(vpath) end
  end
  local avoid = tostring(skill.avoid or "")
  if avoid ~= "" then
    body[#body + 1] = "## 주의 (쓰면 안 되는 경우 / 알려진 함정)"
    body[#body + 1] = avoid
    body[#body + 1] = ""
  end
  body[#body + 1] = "<!-- engram: 작업 이력에서 자동 추출된 스킬 -->"
  local written, err = magi.write_file(SKILLS_DIR .. "/" .. slug .. "/SKILL.md", table.concat(body, "\n"))
  if not written then magi.log("engram: skill not written: " .. tostring(err)); return nil end
  magi.store_set("managed_skill_snapshot:" .. magi.workdir() .. ":" .. slug, table.concat(body, "\n"))
  magi.store_set("managed_verify_snapshot:" .. magi.workdir() .. ":" .. slug, magi.read_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh") or "")
  index_skill(slug, desc)
  -- 생성 시각이 망각 시계의 시작점이다. last-used가 로드 시에만 찍히던 시절, 정리의 표적인
  -- "한 번도 안 쓰인 스킬"이 바로 그 이유로 날짜가 없어 영원히 스킵됐다 — 망각 기전이
  -- 존재하는데 작동하는 걸 아무도 못 본 이유가 이것이었다.
  local lastused = load_lastused()
  if not lastused[slug] then
    lastused[slug] = string.sub(magi.time(), 1, 10)
    save_lastused(lastused)
  end
  return slug
end

-- 기존 교훈과 의미·어휘 유사도가 높으면 새 관리 객체를 만들지 않고 기존 객체의 evidence 만 보강한다.
-- 서로 다른 2개 이상의 세션에서 관측되면 hypothesis -> observed 로 자동 승격한다.
local function append_evidence_to_existing(sid, host_outcome, lesson_text)
  if type(magi.search_experience) ~= "function" then return false end
  local hits = magi.search_experience{
    query = lesson_text,
    kind = "memory",
    state = "active",
    limit = 3,
    min_similarity = 0.78,
  }
  if type(hits) ~= "table" or #hits == 0 then return false end
  local hit = hits[1]
  if type(hit) ~= "table" or not hit.object_id or hit.object_id == "" then return false end
  local ev_list = {}
  local sessions = {}
  if type(hit.evidence) == "table" then
    for _, item in ipairs(hit.evidence) do
      if type(item) == "table" then
        ev_list[#ev_list + 1] = item
        if item.session and item.session ~= "" then
          sessions[tostring(item.session)] = true
        end
      end
    end
  end
  local sid_str = tostring(sid or "")
  local out_str = tostring(host_outcome or "")
  local already = false
  for _, item in ipairs(ev_list) do
    if tostring(item.session or "") == sid_str and tostring(item.outcome or "") == out_str then
      already = true
      break
    end
  end
  if not already and sid_str ~= "" then
    ev_list[#ev_list + 1] = { session = sid_str, outcome = out_str }
    sessions[sid_str] = true
  end
  local session_count = 0
  for _ in pairs(sessions) do session_count = session_count + 1 end
  local new_status = tostring(hit.status or "")
  if (new_status == "" or new_status == "hypothesis") and session_count >= 2 then
    new_status = "observed"
  end
  local payload = {
    kind = "memory",
    state = "active",
    host_outcome = host_outcome ~= "" and host_outcome or hit.host_outcome,
    text = tostring(hit.text or lesson_text),
    evidence = ev_list,
  }
  if hit.description and hit.description ~= "" then payload.description = hit.description end
  if hit.memory_kind and hit.memory_kind ~= "" then payload.memory_kind = hit.memory_kind end
  if type(hit.areas) == "table" and #hit.areas > 0 then payload.areas = hit.areas end
  if hit.tier and hit.tier ~= "" then payload.tier = hit.tier end
  if new_status ~= "" then payload.status = new_status end
  if hit.valid_until and hit.valid_until ~= "" then payload.valid_until = hit.valid_until end
  if type(hit.links) == "table" and #hit.links > 0 then payload.links = hit.links end
  enqueue_managed_object(hit.object_id, hit.revision, payload)
  magi.log("engram: 기존 기억 근거 보강 (" .. hit.object_id .. ", 세션 " .. session_count .. "개)")
  return true
end

local function build_memory_payload(lesson, host_outcome, text)
  local payload = {
    kind = "memory",
    state = "active",
    host_outcome = host_outcome,
    text = text,
  }
  if type(lesson.description) == "string" and lesson.description ~= "" then
    payload.description = lesson.description
  end
  if type(lesson.kind) == "string" and lesson.kind ~= "" then
    payload.memory_kind = lesson.kind
  end
  if type(lesson.areas) == "table" and #lesson.areas > 0 then
    payload.areas = lesson.areas
  end
  if type(lesson.tier) == "string" and lesson.tier ~= "" then
    payload.tier = lesson.tier
  end
  if type(lesson.status) == "string" and lesson.status ~= "" then
    payload.status = lesson.status
  end
  if type(lesson.valid_until) == "string" and lesson.valid_until ~= "" then
    payload.valid_until = lesson.valid_until
  end
  if type(lesson.evidence) == "table" and #lesson.evidence > 0 then
    payload.evidence = lesson.evidence
  end
  if type(lesson.links) == "table" and #lesson.links > 0 then
    payload.links = lesson.links
  end
  return payload
end

local OBSERVATION_CHAR_LIMIT = 1500

local function utf8_safe_head(s, max_bytes)
  if #s <= max_bytes then return s end
  local i = max_bytes
  while i > 0 do
    local b = string.byte(s, i)
    if b < 128 then
      return string.sub(s, 1, i)
    elseif b >= 192 then
      return string.sub(s, 1, i - 1)
    end
    i = i - 1
  end
  return ""
end

local function utf8_safe_tail(s, max_bytes)
  if #s <= max_bytes then return s end
  local start = #s - max_bytes + 1
  while start <= #s do
    local b = string.byte(s, start)
    if b < 128 or b >= 192 then break end
    start = start + 1
  end
  return string.sub(s, start)
end

local function truncate_observation(txt)
  txt = tostring(txt or "")
  if #txt <= OBSERVATION_CHAR_LIMIT then return txt end
  local head = utf8_safe_head(txt, 700)
  local tail = utf8_safe_tail(txt, 700)
  return head .. "\n... [truncated] ...\n" .. tail
end

local function push_reflection_observation(sid, host_outcome, text)
  if not text or text == "" then return end
  local key = "reflection_inbox_v1:" .. magi.workdir()
  local prev = magi.store_get(key) or ""
  local items = magi.json_decode(prev == "" and "[]" or prev)
  if type(items) ~= "table" then items = {} end
  items[#items + 1] = {
    session = tostring(sid or ""),
    at = magi.time(),
    outcome = tostring(host_outcome or ""),
    text = truncate_observation(text),
  }
  while #items > 25 do table.remove(items, 1) end
  magi.store_compare_exchange(key, prev, magi.json_encode(items))
  magi.store_set("dream_dirty_v1:" .. magi.workdir(), "1")
end

local function record_extracted_lesson(sid, lesson, user, host_outcome)
  if type(lesson) ~= "table" or not lesson.task or not lesson.lesson then return end
  -- This analyzer sees conversation excerpts, not controlled comparisons.
  -- Preserve its claims separately and never label a successful turn as
  -- causal evidence for one technique. A model cannot upgrade this field.
  local evidence_key = "learning_evidence_v1:" .. magi.workdir()
  local previous_evidence = magi.store_get(evidence_key) or ""
  local evidence = magi.json_decode(previous_evidence == "" and "[]" or previous_evidence)
  if type(evidence) ~= "table" then
    magi.log("engram: learning evidence is malformed; preserving stored data")
  else
    evidence[#evidence + 1] = {
      session = sid, at = magi.time(), host_outcome = host_outcome,
      task = tostring(lesson.task), change = tostring(lesson.change or lesson.approach or ""),
      evidence = tostring(lesson.evidence or ""), attribution = "confounded",
      source = "conversation_excerpt", verified_application = false,
    }
    while #evidence > 100 do table.remove(evidence, 1) end
    local saved, why = magi.store_compare_exchange(evidence_key, previous_evidence, magi.json_encode(evidence))
    if not saved then magi.log("engram: learning evidence not saved: " .. tostring(why)) end
  end
  local outcome = tostring(lesson.outcome or "none")
  if outcome == "none" then return end
  local key = lesson_key(lesson.task, lesson.approach)
  local seen = recorded[sid]
  if not seen then seen = {} recorded[sid] = seen end
  if seen[key] then return end
  seen[key] = true
  -- 정정(replaces)은 옛 행과 겹치는 것이 정상이라 유사-중복 백스톱을 타지 않는다 —
  -- 백스톱이 정정을 막으면 낡은 주장이 원장의 현재로 영영 남는다. 단 검증은 한다:
  -- 범위 밖이거나 정수가 아닌 번호는 정정이 아니라 오지정이고, 그대로 믿으면 두 중복
  -- 가드를 다 우회한 채 엉뚱한 행을 아카이브할 수 있다 → 일반 append 경로로 강등.
  local replaces = tonumber(lesson.replaces)
  if replaces then
    replaces = math.floor(replaces)
    if replaces < 1 or replaces > #ledger_rows() or replaces ~= tonumber(lesson.replaces) then
      replaces = nil
    end
  end
  local full_text = "[" .. tostring(lesson.category or "기타") .. "] " .. tostring(lesson.task)
    .. " — " .. tostring(lesson.approach or "") .. " → " .. outcome .. ": " .. tostring(lesson.lesson)
  if not replaces then
    if append_evidence_to_existing(sid, host_outcome, full_text) then
      push_reflection_observation(sid, host_outcome, full_text)
      return
    end
    if lesson_is_duplicate(lesson.lesson) then
      magi.log("engram: 유사 교훈 이미 기록됨 — 스킵: " .. tostring(lesson.task))
      return
    end
  end
  local ok, new_row, previous_row = append_lesson({
    timestamp = magi.time(),
    username = magi.store_get("username") or user or "user",
    category = tostring(lesson.category or "기타"),
    task = tostring(lesson.task), approach = tostring(lesson.approach or ""),
    outcome = outcome, lesson = tostring(lesson.lesson),
  }, replaces)
  if ok then
    magi.log("engram: 교훈 기록 — " .. tostring(lesson.task))
    local payload = build_memory_payload(lesson, host_outcome, full_text)
    local synced = enqueue_managed("lesson:" .. new_row, payload, previous_row and ("lesson:" .. previous_row) or nil)
    if not synced then magi.notify(sid, "engram: 교훈은 로컬에 기록했지만 경험 저장소 반영은 대기 중입니다.") end
    push_reflection_observation(sid, host_outcome, full_text)
  end
end

-- 캡처: 결과가 확정된 턴 → 사이드카 분석 → 교훈 기록 + 검증된 스킬 저장.
-- 이 핸들러는 호스트의 관찰 워커에서 돌므로 느린 사이드카가 턴을 막지 않는다.
local function analyze_and_record(sid, hint, user, used_csv, host_outcome, override_input)
  local input = override_input
  if not input then
    local r = recent[sid]
    if not r or #r < 2 then return end -- 직전 시도가 될 맥락 부족
    input = render_window(r)
    if hint and hint ~= "" then input = input .. "\n\n" .. hint end
  end
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
  if not result.lesson and type(result.lessons) ~= "table" and not result.skill then
    magi.log("engram: 기록할 교훈/스킬 없음(사소하거나 미확정)")
    return
  end

  if type(result.lessons) == "table" then
    for _, item in ipairs(result.lessons) do
      record_extracted_lesson(sid, item, user, host_outcome)
    end
  elseif result.lesson then
    record_extracted_lesson(sid, result.lesson, user, host_outcome)
  end

  -- 검증된 성공의 스킬만 저장 (사이드카 프롬프트의 결정 규칙이 1차 게이트)
  -- [plugins.engram] author_skills = false 면 교훈만 남기고 스킬은 짓지 않는다. 모델이 지은 스킬은
  -- 손으로 쓴 스킬과 같은 자리에 광고돼 다음 런의 규칙이 된다 — 실물 2026-09-05(PPT): 지은 스킬
  -- 하나가 세 런에 걸쳐 존재하지 않는 검증 스크립트를 찾게 했다. 컴패니언마다 끌 수 있어야 한다.
  local skill = result.skill
  -- The analyzer cannot certify its own output. Keep the host's structural
  -- outcome separate from lesson.outcome and the natural-language hint.
  if skill and host_outcome ~= "verified" then
    magi.log("engram: skill promotion rejected: host outcome=" .. tostring(host_outcome))
    skill = nil
  end
  if skill and not authorSkills() then
    magi.log("engram: skill authoring is off for this companion ([plugins.engram] author_skills) — lesson only")
    skill = nil
  end
  if skill and skill.name and skill.technique then
    -- 되돌리기 스냅샷은 저장 **전**에 뜬다: 같은 name 재사용(머지·진화) 저장의 취소는
    -- "삭제"가 아니라 "이전 판 복원"이어야 한다 — 삭제는 이번 턴이 만든 것이 아니라
    -- 이전에 승인받고 사람이 다듬었을 수도 있는 스킬 전체를 지운다(4라운드 검수, 실측).
    local pslug = slugify(skill.name)
    local prev_body = magi.read_file(SKILLS_DIR .. "/" .. pslug .. "/SKILL.md") or ""
    local prev_verify = magi.read_file(SKILLS_DIR .. "/" .. pslug .. "/scripts/verify.sh") or ""
    local prev_managed = managed_payload("skill:" .. pslug)
    local slug = save_skill(skill)
    -- nil = 유사-스킬 백스톱이 생성을 막았다. 여기서 그대로 이어가면 nil 연결(concat)이
    -- 핸들러 전체를 죽여 이 턴의 prune까지 건너뛴다 — 백스톱이 발동할 때마다.
    if not slug then return end
    magi.log("engram: 스킬 저장 — " .. slug)
    -- 세션 키와 함께: 취소 창이 스토어 전역이면, 다른 세션의 무관한 질문에 "아니요"라
    -- 답한 것이 이 스킬을 지웠다. 창은 저장을 목격한 그 대화의 것이다.
    -- 창 키는 세션별이다: 한 칸짜리 전역 창은 다른 세션의 저장이 이 세션의 창을 덮어,
    -- 방금 "N이라고 하면 되돌립니다"라고 들은 사용자의 N이 조용히 무시됐다.
    magi.store_set("last_skill:" .. sid, sid .. "\t" .. slug)
    magi.store_set("last_skill_prev:" .. sid, prev_body)
    magi.store_set("last_skill_prev_verify:" .. sid, prev_verify)
    magi.store_set("last_skill_saved:" .. sid, magi.read_file(SKILLS_DIR .. "/" .. slug .. "/SKILL.md") or "")
    magi.store_set("last_skill_saved_verify:" .. sid, magi.read_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh") or "")
    magi.store_set("last_skill_prev_managed:" .. sid, prev_managed and magi.json_encode(prev_managed) or "")
    magi.notify(sid, "engram: 검증된 성공에서 스킬 '" .. slug
      .. "' 을(를) 자동 저장했습니다. 잘못 저장됐으면 'N' 또는 '스킬 취소'라고 입력하면 되돌립니다.")
    local synced = enqueue_managed("skill:" .. slug, {
        kind = "skill", state = "active", host_outcome = host_outcome,
        name = slug,
        description = tostring(skill.description or ""),
        body = tostring(skill.technique or ""),
        document_snapshot = magi.read_file(SKILLS_DIR .. "/" .. slug .. "/SKILL.md") or "",
        verification_snapshot = magi.read_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh") or "",
    })
    if not synced then magi.notify(sid, "engram: 스킬은 로컬에 저장했지만 경험 저장소 반영은 대기 중입니다.") end
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

-- 취소 창의 스냅샷 키 정리 — 창을 닫는 모든 경로(취소 실행·비부정 응답)가 같이 부른다.
local function clear_undo_window(sid)
  magi.store_set("last_skill:" .. sid, "")
  magi.store_set("last_skill_prev:" .. sid, "")
  magi.store_set("last_skill_prev_verify:" .. sid, "")
  magi.store_set("last_skill_saved:" .. sid, "")
  magi.store_set("last_skill_saved_verify:" .. sid, "")
  magi.store_set("last_skill_prev_managed:" .. sid, "")
end

-- 마지막 자동 저장 스킬의 취소(undo): 저장 직후 "다음 사용자 메시지"까지가 취소 창.
-- 두 모드: 이번 턴이 스킬을 **만든** 것이면 삭제, 기존 스킬을 **덮어쓴** 것이면(머지 저장)
-- 저장 전 스냅샷으로 복원 — 삭제는 이전에 승인받은 스킬 전체를 날리는 데이터 손실이었다.
local function undo_last_skill(sid)
  local rec = magi.store_get("last_skill:" .. sid)
  if not rec or rec == "" then return false end
  local owner, slug = string.match(rec, "^([^\t]*)\t(.*)$")
  if not slug or slug == "" or owner ~= sid then return false end
  local saved = magi.store_get("last_skill_saved:" .. sid)
  local current = magi.read_file(SKILLS_DIR .. "/" .. slug .. "/SKILL.md") or ""
  local current_verify = magi.read_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh") or ""
  if saved and saved ~= "" and (current ~= saved or current_verify ~= (magi.store_get("last_skill_saved_verify:" .. sid) or "")) then
    enqueue_managed("skill:" .. slug, {kind = "skill", state = "withdrawn"})
    magi.notify(sid, "engram: 저장 후 스킬이 변경되어 자동으로 되돌리지 않았습니다. 변경 내용을 확인해 주세요.")
    clear_undo_window(sid)
    return true
  end
  local old_payload = magi.store_get("last_skill_prev_managed:" .. sid) or ""
  local payload = old_payload ~= "" and magi.json_decode(old_payload) or {kind = "skill", state = "withdrawn"}
  local synced = enqueue_managed("skill:" .. slug, payload)
  if not synced then magi.notify(sid, "engram: 경험 저장소의 스킬 되돌림은 대기 중입니다.") end
  local prev = magi.store_get("last_skill_prev:" .. sid) or ""
  if prev ~= "" then
    magi.write_file(SKILLS_DIR .. "/" .. slug .. "/SKILL.md", prev)
    magi.store_set("managed_skill_snapshot:" .. magi.workdir() .. ":" .. slug, prev)
    local vpath = SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh"
    local pv = magi.store_get("last_skill_prev_verify:" .. sid) or ""
    if pv ~= "" then
      magi.write_file(vpath, pv)
    elseif magi.read_file(vpath) then
      magi.remove_file(vpath) -- 이번 저장이 새로 만든 부속 — 이전 판엔 없었다
    end
    magi.store_set("managed_verify_snapshot:" .. magi.workdir() .. ":" .. slug, pv)
    -- 인덱스 설명도 이전 판의 frontmatter에서 되살린다. yaml_quote 역파싱 대신 원문 그대로 —
    -- 인덱스는 유사도 토큰용이라 따옴표가 남아도 무해하다. (사용실적·망각시계는 안 건드린다:
    -- 이 스킬은 이번 턴 전부터 있었고 시계도 그때부터 돌고 있었다.)
    local pdesc = string.match(prev, "\ndescription:%s*([^\n]*)") or ""
    index_skill(slug, pdesc)
    clear_undo_window(sid)
    magi.notify(sid, "engram: 스킬 '" .. slug .. "' 을(를) 이번 턴 저장 전의 판으로 되돌렸습니다.")
    magi.log("engram: 스킬 되돌림(이전 판 복원) — " .. slug)
    return true
  end
  magi.remove_file(SKILLS_DIR .. "/" .. slug)
  magi.store_set("managed_skill_snapshot:" .. magi.workdir() .. ":" .. slug, "")
  magi.store_set("managed_verify_snapshot:" .. magi.workdir() .. ":" .. slug, "")
  -- 인덱스·사용실적·망각시계에서도 제거 — 시계를 남기면 같은 이름으로 재생성된 스킬이
  -- 취소된 전임자의 날짜를 물려받아 조기 아카이브된다.
  for _, key in ipairs({ "skill_index", "skill_usage", "skill_lastused" }) do
    local out = {}
    for line in string.gmatch(magi.store_get(key) or "", "[^\n]+") do
      if string.match(line, "^([^\t]*)\t") ~= slug then out[#out + 1] = line end
    end
    magi.store_set(key, table.concat(out, "\n"))
  end
  clear_undo_window(sid)
  magi.notify(sid, "engram: 스킬 '" .. slug .. "' 저장을 취소(삭제)했습니다.")
  magi.log("engram: 스킬 취소 — " .. slug)
  return true
end

magi.on("user_message", function(ev)
  flush_managed()
  -- 취소 창: 직전에 자동 저장된 스킬이 있으면, 이 메시지가 짧은 부정이면 되돌리고
  -- 아니면 창을 닫는다(one-shot — 다음-턴 취소 시맨틱).
  local last = magi.store_get("last_skill:" .. ev.session)
  if last and last ~= "" then
    local owner = string.match(last, "^([^\t]*)\t") or ""
    if owner == ev.session then
      if is_denial(ev.text) then
        undo_last_skill(ev.session)
      else
        clear_undo_window(ev.session)
      end
    end
  end
  push_recent(ev.session, "user", ev.text)
end)

-- 스킬 사용 실적 원장(플러그인 스토어): slug\t로드수\t성공수\t실패수.
-- 호스트가 turn_finished 페이로드에 실어주는 "이번 턴에 로드된 스킬" ×
-- 구조적 outcome으로 결정론적으로 계측한다 — 큐레이션(병합/정리)의 실측 근거.
-- Last-used dates (YYYY-MM-DD per skill slug) drive date-based pruning. Stored as a
-- newline-joined "slug\tdate" map alongside skill_usage; the load/save helpers live above
-- save_skill, which stamps a skill's creation date as the forgetting clock's start.

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

local function epoch_of(ts)
  ts = tostring(ts or "")
  local d = to_days(string.sub(ts, 1, 10))
  if not d then return nil end
  local h, m, sec = ts:match("T(%d%d):(%d%d):(%d%d)")
  return d * 86400 + (tonumber(h) or 0) * 3600 + (tonumber(m) or 0) * 60 + (tonumber(sec) or 0)
end

-- PRUNE_DAYS default; overridable per-user with /engram-prune-days (stored).
local PRUNE_DAYS_DEFAULT = 7 -- legacy explicit opt-in only; default retention is host-local 30/90 days

local function legacy_prune_enabled()
  local n = tonumber(magi.store_get("prune_days") or "")
  return n and n > 0
end

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
  -- Do not move files by default: host-local cold/archive-candidate policy
  -- preserves shared objects. Keep an explicitly configured legacy cutoff.
  if not legacy_prune_enabled() then return end
  local today = to_days(string.sub(magi.time(), 1, 10))
  if not today then return end
  local cutoff = prune_days()
  local lastused = load_lastused()
  local idx = magi.store_get("skill_index") or ""
  local archived = {}
  local dated = false
  for line in string.gmatch(idx, "[^\n]+") do
    local slug = string.match(line, "^([^\t]*)\t")
    if slug then
      local path = SKILLS_DIR .. "/" .. slug .. "/SKILL.md"
      local body = magi.read_file(path)
      if body and string.find(body, ENGRAM_MARKER, 1, true) then -- engram-owned only
        local ld = to_days(lastused[slug])
        if not ld then
          -- 시계 없는 스킬(이 수정 이전에 만들어졌거나 기록이 유실됨)은 스킵이 아니라
          -- **오늘부터 시계를 시작**한다. 스킵은 "한 번도 안 쓰인 스킬은 영원히 산다"였고,
          -- 그것은 정리가 정확히 잡으라던 대상이다.
          lastused[slug] = string.sub(magi.time(), 1, 10)
          dated = true
        elseif (today - ld) >= cutoff then
          local snapshot = magi.store_get("managed_skill_snapshot:" .. magi.workdir() .. ":" .. slug)
          local verify_snapshot = magi.store_get("managed_verify_snapshot:" .. magi.workdir() .. ":" .. slug)
          local current_verify = magi.read_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh") or ""
          if (snapshot and snapshot ~= "" and snapshot ~= body)
            or (verify_snapshot and current_verify ~= verify_snapshot) then
            magi.log("engram: edited skill preserved instead of archiving: " .. slug)
          else
          magi.write_file(SKILLS_DIR .. "/.archive/" .. slug .. "/SKILL.md", body)
          -- 부속 스크립트도 함께 — 본문만 옮기면 verify.sh가 고아로 남는다.
          local verify = magi.read_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh")
          if verify and verify ~= "" then
            magi.write_file(SKILLS_DIR .. "/.archive/" .. slug .. "/scripts/verify.sh", verify)
            magi.remove_file(SKILLS_DIR .. "/" .. slug .. "/scripts/verify.sh")
          end
          magi.remove_file(path)
          if managed_payload("skill:" .. slug) then
            enqueue_managed("skill:" .. slug, {kind = "skill", state = "archived"})
          end
          archived[slug] = true
          end
        end
      end
    end
  end
  if dated then save_lastused(lastused) end
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

-- Loading is an observation, not evidence of application. Even a verified turn
-- cannot attribute its success to each loaded skill; never extend retention here.
local function update_usage(skills_csv, outcome)
  if not skills_csv or skills_csv == "" then return end
  local map, order = {}, {}
  for line in string.gmatch(magi.store_get("skill_usage") or "", "[^\n]+") do
    local n, l, o, b = string.match(line, "^([^\t]*)\t(%d+)\t(%d+)\t(%d+)$")
    if n then map[n] = { tonumber(l), tonumber(o), tonumber(b) } order[#order + 1] = n end
  end
  for name in string.gmatch(skills_csv, "[^,]+") do
    local m = map[name]
    if not m then m = { 0, 0, 0 } map[name] = m order[#order + 1] = name end
    m[1] = m[1] + 1
    if outcome == "verified" then m[2] = m[2] + 1
    elseif outcome == "unverified" or outcome == "guard" or outcome == "error" then m[3] = m[3] + 1 end
  end
  local out = {}
  for _, n in ipairs(order) do
    local m = map[n]
    out[#out + 1] = n .. "\t" .. m[1] .. "\t" .. m[2] .. "\t" .. m[3]
  end
  magi.store_set("skill_usage", table.concat(out, "\n"))
end

local FACTS_BATCH_DEFAULT = 4
local REFLECT_INTERVAL = 25
local REFLECT_MIN_OBSERVATIONS = 3
local REFLECT_FAILURE_LIMIT = 3
local CORE_CHAR_BUDGET = 2400
local CORE_DREAM_TARGET = 1920 -- 80% of 2400
local PRESSURE_COOLDOWN_SECONDS = 300 -- 최소 5분 쿨다운으로 스래싱 폭풍 방어
local PRESSURE_HYSTERESIS_CHARS = 200 -- 직전 드림 대비 최소 +200자 유입 시에만 재트리거
local NURSERY_GRACE_PERIOD_SECONDS = 2 * 3600 -- 생성/수정 2시간 이내 신생 기억 강등 보호(Grace Period)
local MAX_UNREFLECTED_BACKLOG = 15 -- 미성찰 인박스 15건 이상 시 backlog_overflow 압박 감지

local REFLECT_PROMPT = [[당신은 경험 저장소의 성찰(reflection) 통합기다.
입력으로 최근 작업 관찰(inbox)과 관련 기존 기억(managed memory) 목록이 주어진다.
JSON 객체 하나로만 응답하라(코드펜스·설명 금지):
{"actions":[{"op":"create|update|conflict|discard","object_id":"기존 객체 ID (create/discard 시 생략 가능)","description":"언제 필요한지 200자 이내 요약","text":"통합·정제된 기억 본문","kind":"lesson|fact|procedure|preference|pitfall","areas":["구현|디버깅|분석|설정|리팩터링|문서"],"tier":"core|reference","status":"hypothesis|observed|conflicted|disconfirmed","valid_until":"YYYY-MM-DD 또는 빈 문자열"}]}
규칙:
- 기존 기억과 같은 주제면 새 객체를 만들지 말고 update로 통합하라.
- 서로 모순되는 관찰이 확인되면 status="conflicted"로 표시하라.
- 틀렸음이 확인된 기억은 status="disconfirmed"로 표시하라.
- 새로운 교훈이 없으면 {"actions":[]} 를 반환하라.]]

local DREAM_PROMPT = [[당신은 경험 저장소의 백그라운드 재정리(dream) 통합기다.
입력으로 유사도가 높거나 충돌(conflicted) 상태인 기억 클러스터 목록이 주어진다.
각 클러스터를 하나의 정본(keep_id)으로 병합하고 나머지(archive_ids)를 보관 처리하라.
규칙:
- 도메인/언어/프레임워크가 서로 다른 기억은 억지로 병합하지 말고 분리된 상태를 유지하라.
- 클러스터 내에 서로 다른 도메인이나 성격이 섞여 있어 단일 정본으로 묶을 수 없다면 해당 항목은 merges에 포함하지 마라.
- 보존할 가치가 있는 구체적인 수치·명령어·경로는 임의로 삭제하지 마라.
JSON 객체 하나로만 응답하라(코드펜스·설명 금지):
{"merges":[{"keep_id":"남길 객체 ID","archive_ids":["보관할 중복 객체 ID"],"description":"200자 이내 트리거 요약","text":"병합된 정본 본문","kind":"lesson|fact|procedure|preference|pitfall","areas":["구현|디버깅|분석|설정|리팩터링|문서"],"tier":"core|reference","status":"observed|conflicted"}]}]]

local DREAM_PRESSURE_PROMPT = [[당신은 경험 저장소의 긴급 압박(pressure dream) 압축기다.
코어 티어의 예산 초과(2,400자 상한)를 해소하기 위해 고밀도 핵심 규칙으로 압축하라.
규칙:
- 도메인/언어/프레임워크가 서로 다른 기억은 억지로 병합하지 마라.
- 원본의 구체적인 에러 로그, CLI 명령어, 포트 번호, 파일 경로는 절대 임의로 삭제하지 말고 reference 객체에 온전히 보존(Split & Link)하라.
- 코어 정본(keep_id)에는 한 줄짜리 핵심 트리거 규칙과 [ref: archive_id] 포인터만 남겨라.
JSON 객체 하나로만 응답하라(코드펜스·설명 금지):
{"merges":[{"keep_id":"남길 객체 ID","archive_ids":["reference로 보존할 객체 ID"],"description":"200자 이내 트리거 요약","text":"한 줄짜리 핵심 규칙 + [ref: archive_id] 포인터","kind":"lesson|fact|procedure|preference|pitfall","areas":["구현|디버깅|분석|설정|리팩터링|문서"],"tier":"core","status":"observed"}]}]]

local function is_dream_session(sid)
  local s = string.lower(tostring(sid or ""))
  return s == "dream" or s == "engram_dream" or string.find(s, "engram%-dream") ~= nil
end

local function is_classifier_blocked(reason)
  local r = string.lower(tostring(reason or ""))
  return string.find(r, "safety", 1, true) ~= nil or string.find(r, "classifier", 1, true) ~= nil
end

local function facts_batch_size(outcome)
  local cfg = tonumber(magi.store_get("facts_batch") or "")
  if cfg and cfg >= 1 then return math.floor(cfg) end
  if outcome == "verified" or outcome == "error" or outcome == "guard" then
    return 1
  end
  return FACTS_BATCH_DEFAULT
end

local function notify_knowledge_change()
  if type(magi.notify_slot_change) == "function" then
    magi.notify_slot_change("knowledge")
  end
end

local function load_facts_inbox()
  local key = "engram_inbox_v1:" .. magi.workdir()
  local raw = magi.store_get(key) or ""
  local list = magi.json_decode(raw == "" and "[]" or raw)
  if type(list) ~= "table" then return {}, raw, key end
  return list, raw, key
end

local function flush_facts_inbox(force, fallback_batch)
  local list, raw, key = load_facts_inbox()
  if #list == 0 then return false end
  local need = fallback_batch or FACTS_BATCH_DEFAULT
  if not force and #list < need then return false end
  magi.store_compare_exchange(key, raw, "[]")
  local parts = {}
  local last_sid = "batch"
  local last_user = nil
  local last_outcome = "unverified"
  local skills_seen, skills_list = {}, {}
  for idx, item in ipairs(list) do
    if type(item) == "table" then
      last_sid = tostring(item.session or last_sid)
      if item.user and item.user ~= "" then last_user = item.user end
      if item.outcome and item.outcome ~= "" then last_outcome = item.outcome end
      if item.skills and item.skills ~= "" then
        for sk in string.gmatch(tostring(item.skills), "[^,]+") do
          if not skills_seen[sk] then
            skills_seen[sk] = true
            skills_list[#skills_list + 1] = sk
          end
        end
      end
      parts[#parts + 1] = "[배치 턴 " .. idx .. "/" .. #list .. "]\n"
        .. tostring(item.window or "") .. "\n" .. tostring(item.hint or "")
    end
  end
  if #parts == 0 then return false end
  analyze_and_record(
    last_sid,
    "",
    last_user,
    table.concat(skills_list, ","),
    last_outcome,
    table.concat(parts, "\n\n===\n\n")
  )
  notify_knowledge_change()
  return true
end

local function enqueue_facts_inbox(sid, hint, user, used_csv, outcome)
  local r = recent[sid]
  if not r or #r < 2 then return false end
  local window = render_window(r)
  if #window < 20 then return false end
  local batch_n = facts_batch_size(outcome)
  if batch_n <= 1 then
    analyze_and_record(sid, hint, user, used_csv, outcome)
    return true
  end
  local list, raw, key = load_facts_inbox()
  list[#list + 1] = {
    session = tostring(sid or ""),
    at = magi.time(),
    outcome = tostring(outcome or ""),
    hint = truncate_observation(hint),
    user = user,
    skills = used_csv,
    window = truncate_observation(window),
  }
  while #list > 32 do table.remove(list, 1) end
  magi.store_compare_exchange(key, raw, magi.json_encode(list))
  notify_knowledge_change()
  if #list >= batch_n then
    return flush_facts_inbox( true, batch_n )
  end
  return false
end

local function reflection_item_sig(it)
  if type(it) ~= "table" then return "" end
  return tostring(it.session or "") .. "|" .. tostring(it.at or "") .. "|" .. tostring(it.text or "")
end

local function is_payload_error(err)
  local s = string.lower(tostring(err or ""))
  -- Transport and capacity failures say nothing about the observations: keep those in the inbox.
  -- Checked first, because their messages can contain the words and digits below.
  for _, word in ipairs{ "timeout", "timed out", "deadline", "connection", "rate limit",
                         "too many requests", "overloaded", "temporarily", "status 429", "status 5" } do
    if string.find(s, word, 1, true) then return false end
  end
  for _, word in ipairs{ "bad request", "context length", "context_length", "too long", "token limit",
                         "safety", "safeguard", "blocked" } do
    if string.find(s, word, 1, true) then return true end
  end
  -- A bare "400" also appears in ports, durations and request ids, so it has to be named as a status.
  for _, label in ipairs{ "status", "http", "code", "returned" } do
    if string.find(s, label .. "[^%d]*400%D") or string.find(s, label .. "[^%d]*400$") then return true end
  end
  return false
end

local function isolate_poison_pills(rkey, processed_sigs)
  local dead_key = "reflection_dead_letter_v1:" .. magi.workdir()
  for attempt = 1, 3 do
    local cur_raw = magi.store_get(rkey) or ""
    local cur_items = magi.json_decode(cur_raw == "" and "[]" or cur_raw)
    if type(cur_items) ~= "table" then break end
    local next_items = {}
    local dead_items = {}
    local changed = false
    for _, it in ipairs(cur_items) do
      if type(it) == "table" then
        local sig = reflection_item_sig(it)
        if processed_sigs[sig] then
          local fc = (tonumber(it._fail_count) or 0) + 1
          it._fail_count = fc
          changed = true
          if fc >= REFLECT_FAILURE_LIMIT then
            dead_items[#dead_items + 1] = it
            magi.log("engram: 독성 페이로드(Poison Pill) 격리 — " .. REFLECT_FAILURE_LIMIT .. "회 연속 실패로 dead-letter로 이동: " .. string.sub(tostring(it.text or ""), 1, 60))
          else
            next_items[#next_items + 1] = it
          end
        else
          next_items[#next_items + 1] = it
        end
      end
    end
    if changed then
      if #dead_items > 0 then
        local dead_written = false
        for dl_attempt = 1, 3 do
          local prev_dead = magi.store_get(dead_key) or ""
          local dl_list = magi.json_decode(prev_dead == "" and "[]" or prev_dead)
          if type(dl_list) ~= "table" then dl_list = {} end
          -- This loop reruns when the inbox write below loses a race, so an item may already be here.
          local present = {}
          for _, dit in ipairs(dl_list) do present[reflection_item_sig(dit)] = true end
          for _, dit in ipairs(dead_items) do
            if not present[reflection_item_sig(dit)] then dl_list[#dl_list + 1] = dit end
          end
          while #dl_list > 200 do table.remove(dl_list, 1) end
          if magi.store_compare_exchange(dead_key, prev_dead, magi.json_encode(dl_list)) then
            dead_written = true
            break
          end
        end
        -- Taking items out of the inbox with nowhere recorded to hold them would lose them.
        if not dead_written then
          magi.log("engram: dead-letter write lost its race; items stay in the inbox")
          break
        end
      end
      if magi.store_compare_exchange(rkey, cur_raw, magi.json_encode(next_items)) then
        break
      end
    else
      break
    end
  end
end

-- Takes out only what was restored: another companion may have isolated more since it was read.
local function remove_dead_letters(dead_key, restored)
  for _ = 1, 3 do
    local raw = magi.store_get(dead_key) or ""
    local list = magi.json_decode(raw == "" and "[]" or raw)
    if type(list) ~= "table" then return end
    local kept = {}
    for _, it in ipairs(list) do
      if not restored[reflection_item_sig(it)] then kept[#kept + 1] = it end
    end
    if magi.store_compare_exchange(dead_key, raw, magi.json_encode(kept)) then return end
  end
  magi.log("engram: could not clear restored dead-letter items; the next restore skips them")
end

local function run_reflection(trigger_reason, explicit, compaction_summary)
  flush_facts_inbox(true, 1)
  local rkey = "reflection_inbox_v1:" .. magi.workdir()
  local fail_key = "reflection_failures_v1:" .. magi.workdir()
  local fail_ts_key = "reflection_fail_epoch_v1:" .. magi.workdir()
  local fails = tonumber(magi.store_get(fail_key) or "0") or 0
  local now_epoch = epoch_of(magi.time()) or 0

  if explicit then
    fails = 0
    magi.store_set(fail_key, "0")
    magi.store_set(fail_ts_key, "0")
    -- 수동 /engram-reflect 실행 시 dead-letter에 격리되었던 항목들을 inbox로 복구하여 재평가 기회를 부여
    local dead_key = "reflection_dead_letter_v1:" .. magi.workdir()
    local dead_raw = magi.store_get(dead_key) or ""
    local dead_items = magi.json_decode(dead_raw == "" and "[]" or dead_raw)
    if type(dead_items) == "table" and #dead_items > 0 then
      for attempt = 1, 3 do
        local cur_rraw = magi.store_get(rkey) or ""
        local cur_inbox = magi.json_decode(cur_rraw == "" and "[]" or cur_rraw)
        if type(cur_inbox) ~= "table" then cur_inbox = {} end
        -- An earlier restore that stopped before clearing the dead-letter list must not double items.
        local present, restored = {}, {}
        for _, it in ipairs(cur_inbox) do present[reflection_item_sig(it)] = true end
        for _, dit in ipairs(dead_items) do
          if type(dit) == "table" then
            local sig = reflection_item_sig(dit)
            restored[sig] = true
            if not present[sig] then
              present[sig] = true
              dit._fail_count = 0
              cur_inbox[#cur_inbox + 1] = dit
            end
          end
        end
        if magi.store_compare_exchange(rkey, cur_rraw, magi.json_encode(cur_inbox)) then
          remove_dead_letters(dead_key, restored)
          magi.log("engram: 수동 reflection 실행으로 dead-letter " .. #dead_items .. "건을 inbox로 복원했습니다.")
          break
        end
      end
    end
  elseif fails >= REFLECT_FAILURE_LIMIT then
    local last_fail_ts = tonumber(magi.store_get(fail_ts_key) or "0") or 0
    local backoff = math.min(3600, 300 * math.floor(2 ^ math.min(fails - REFLECT_FAILURE_LIMIT, 5)))
    if last_fail_ts > 0 and (now_epoch - last_fail_ts) < backoff then
      local remaining = backoff - (now_epoch - last_fail_ts)
      magi.log("engram: reflection 서킷 브레이커 작동 중 (연속 실패 " .. fails .. "회, 백오프 잔여 " .. remaining .. "초) — 스킵")
      return "engram: reflection 서킷 브레이커가 열려 있습니다 (백오프 잔여: " .. remaining .. "초, /engram-reflect 로 즉시 재시도 가능)."
    end
    magi.log("engram: reflection 서킷 브레이커 half-open 시험 실행 중 (연속 실패 " .. fails .. "회 후 백오프 경과)")
  end

  local rraw = magi.store_get(rkey) or ""
  local items = magi.json_decode(rraw == "" and "[]" or rraw)
  if type(items) ~= "table" then items = {} end
  if #items == 0 and (not compaction_summary or compaction_summary == "") then
    return "engram: 성찰할 새 관찰 항목이 없습니다."
  end
  if #items == 0 and (not compaction_summary or compaction_summary == "") then
    return "engram: 성찰할 새 관찰 항목이 없습니다."
  end

  local obs_lines = {}
  local query_parts = {}
  local processed_sigs = {}
  for i, it in ipairs(items) do
    if type(it) == "table" and it.text then
      local sig = reflection_item_sig(it)
      processed_sigs[sig] = true
      obs_lines[#obs_lines + 1] = "(" .. i .. ") [session=" .. tostring(it.session or "") .. " outcome=" .. tostring(it.outcome or "") .. "] " .. tostring(it.text)
      query_parts[#query_parts + 1] = tostring(it.text)
    end
  end
  if compaction_summary and compaction_summary ~= "" then
    obs_lines[#obs_lines + 1] = "(compaction_summary) " .. tostring(compaction_summary)
    query_parts[#query_parts + 1] = tostring(compaction_summary)
  end
  if #obs_lines == 0 then
    magi.store_compare_exchange(rkey, rraw, "[]")
    return "engram: 성찰할 새 관찰 항목이 없습니다."
  end

  local existing_by_id = {}
  local mem_lines = {}
  if type(magi.search_experience) == "function" then
    local hits = magi.search_experience{
      query = table.concat(query_parts, " "),
      kind = "memory",
      state = "active",
      limit = 10,
      min_similarity = 0.20,
    }
    if type(hits) == "table" then
      for _, h in ipairs(hits) do
        if type(h) == "table" and h.object_id then
          existing_by_id[h.object_id] = h
          mem_lines[#mem_lines + 1] = "- id=" .. tostring(h.object_id)
            .. " rev=" .. tostring(h.revision or "")
            .. " tier=" .. tostring(h.tier or "reference")
            .. " status=" .. tostring(h.status or "observed")
            .. " desc=" .. tostring(h.description or "")
            .. " :: " .. tostring(h.text or "")
        end
      end
    end
  end

  local prompt_text = "[성찰 트리거] " .. tostring(trigger_reason or "manual")
    .. "\n\n[최근 관찰 inbox]\n" .. table.concat(obs_lines, "\n")
  if #mem_lines > 0 then
    prompt_text = prompt_text .. "\n\n[관련 기존 관리 기억]\n" .. table.concat(mem_lines, "\n")
  end

  local model = magi.store_get("sidecar_model")
  local raw, err = magi.analyze{ system = REFLECT_PROMPT, text = prompt_text, model = model }
  if not raw then
    fails = fails + 1
    magi.store_set(fail_key, tostring(fails))
    magi.store_set(fail_ts_key, tostring(now_epoch))
    if is_payload_error(err) then
      isolate_poison_pills(rkey, processed_sigs)
    else
      magi.log("engram: 일시적 모델 통신/용량 오류 — inbox 관찰 항목을 보존하고 서킷 브레이커만 갱신합니다: " .. tostring(err))
    end
    magi.log("engram: reflection 분석 실패 (" .. fails .. "/" .. REFLECT_FAILURE_LIMIT .. "): " .. tostring(err))
    return "engram: reflection 실패 — " .. tostring(err)
  end
  local parsed = parse_cortex_json(raw)
  if type(parsed) ~= "table" or type(parsed.actions) ~= "table" then
    fails = fails + 1
    magi.store_set(fail_key, tostring(fails))
    magi.store_set(fail_ts_key, tostring(now_epoch))
    isolate_poison_pills(rkey, processed_sigs)
    magi.log("engram: reflection 응답 파싱 실패 (" .. fails .. "/" .. REFLECT_FAILURE_LIMIT .. ")")
    return "engram: reflection 응답 형식 오류"
  end

  magi.store_set(fail_key, "0")
  magi.store_set(fail_ts_key, "0")

  -- Set-Difference CAS Drain: 처리 완료된 관찰 항목만 인박스에서 차집합 제거
  for attempt = 1, 3 do
    local cur_raw = magi.store_get(rkey) or ""
    local cur_items = magi.json_decode(cur_raw == "" and "[]" or cur_raw)
    local remaining = {}
    if type(cur_items) == "table" then
      for _, it in ipairs(cur_items) do
        if type(it) == "table" then
          local sig = reflection_item_sig(it)
          if not processed_sigs[sig] then
            remaining[#remaining + 1] = it
          end
        end
      end
    end
    local next_raw = magi.json_encode(remaining)
    if magi.store_compare_exchange(rkey, cur_raw, next_raw) then
      break
    end
  end
  local applied = 0
  for _, act in ipairs(parsed.actions) do
    if type(act) == "table" then
      local op = tostring(act.op or "")
      local oid = tostring(act.object_id or "")
      local prev = existing_by_id[oid]
      if (op == "update" or op == "conflict") and prev then
        local st = tostring(act.status or (op == "conflict" and "conflicted" or prev.status or "observed"))
        local payload = {
          kind = "memory",
          state = st == "disconfirmed" and "archived" or "active",
          host_outcome = prev.host_outcome or "verified",
          text = tostring(act.text and act.text ~= "" and act.text or prev.text),
          description = tostring(act.description and act.description ~= "" and act.description or prev.description or ""),
          memory_kind = tostring(act.kind and act.kind ~= "" and act.kind or prev.memory_kind or "lesson"),
          tier = tostring(act.tier and act.tier ~= "" and act.tier or prev.tier or "reference"),
          status = st,
          valid_until = tostring(act.valid_until or prev.valid_until or ""),
          evidence = prev.evidence,
          links = prev.links,
        }
        if type(act.areas) == "table" and #act.areas > 0 then
          payload.areas = act.areas
        elseif type(prev.areas) == "table" and #prev.areas > 0 then
          payload.areas = prev.areas
        end
        if enqueue_managed_object(oid, prev.revision, payload) then
          applied = applied + 1
        end
      elseif op == "create" and act.text and tostring(act.text) ~= "" then
        local payload = {
          kind = "memory",
          state = "active",
          host_outcome = "ungated",
          text = tostring(act.text),
          description = tostring(act.description or ""),
          memory_kind = tostring(act.kind or "lesson"),
          tier = "reference",
          status = "hypothesis",
          valid_until = tostring(act.valid_until or ""),
        }
        if type(act.areas) == "table" and #act.areas > 0 then payload.areas = act.areas end
        if enqueue_managed("reflect:" .. magi.nonce(), payload) then
          applied = applied + 1
        end
      end
    end
  end
  magi.log("engram: reflection 완료 (" .. applied .. "건 반영, 사유=" .. tostring(trigger_reason or "manual") .. ")")
  notify_knowledge_change()
  return "engram: reflection 완료 — " .. applied .. "건 반영"
end

local function reflection_backlog_count()
  local rraw = magi.store_get("reflection_inbox_v1:" .. magi.workdir()) or ""
  local items = magi.json_decode(rraw == "" and "[]" or rraw)
  local n = type(items) == "table" and #items or 0
  return n + #(load_facts_inbox())
end

local function run_dream(origin, reason)
  origin = origin or "manual"
  reason = reason or "manual"
  prune_stale()
  if type(magi.search_experience) ~= "function" then
    return "engram: dream 완료 (경험 검색 브리지 비활성)"
  end

  -- 트리거별 실행 범위 분리: backlog_overflow인 경우 인박스 배치 소화 우선
  if origin == "pressure" and reason == "backlog_overflow" then
    run_reflection("pressure_backlog", false)
  end

  local all = magi.search_experience{
    query = "",
    kind = "memory",
    state = "active",
    limit = 200,
    min_similarity = 0,
  }
  if type(all) ~= "table" or #all == 0 then
    return "engram: dream 완료 (활성 기억 0건, LLM 0회 호출)"
  end

  local today = to_days(string.sub(magi.time(), 1, 10))
  local now_epoch = epoch_of(magi.time()) or 0
  local expired_n, promoted_n, demoted_n, merged_n = 0, 0, 0, 0
  local active_records = {}

  -- 1단계 (결정론적 패스, LLM 0 토큰): 만료 정리 + hypothesis->observed 승격
  for _, rec in ipairs(all) do
    if type(rec) == "table" and rec.object_id then
      local vu = tostring(rec.valid_until or "")
      local vu_days = vu ~= "" and to_days(vu) or nil
      if today and vu_days and vu_days < today then
        local payload = {
          kind = "memory",
          state = "archived",
          host_outcome = rec.host_outcome,
          text = tostring(rec.text or ""),
          description = tostring(rec.description or ""),
          memory_kind = tostring(rec.memory_kind or ""),
          areas = rec.areas,
          tier = tostring(rec.tier or "reference"),
          status = "disconfirmed",
          valid_until = vu,
          evidence = rec.evidence,
          links = rec.links,
        }
        local ok, new_rev = enqueue_managed_object(rec.object_id, rec.revision, payload)
        if ok then
          expired_n = expired_n + 1
          if new_rev and new_rev ~= "" then rec.revision = new_rev end
        end
      else
        local sessions = {}
        if type(rec.evidence) == "table" then
          for _, ev in ipairs(rec.evidence) do
            if type(ev) == "table" and ev.session and ev.session ~= "" then
              sessions[tostring(ev.session)] = true
            end
          end
        end
        local scount = 0
        for _ in pairs(sessions) do scount = scount + 1 end
        rec._session_count = scount
        if tostring(rec.status or "") == "hypothesis" and scount >= 2 then
          rec.status = "observed"
          local payload = {
            kind = "memory",
            state = "active",
            host_outcome = rec.host_outcome,
            text = tostring(rec.text or ""),
            description = tostring(rec.description or ""),
            memory_kind = tostring(rec.memory_kind or ""),
            areas = rec.areas,
            tier = tostring(rec.tier or "reference"),
            status = "observed",
            valid_until = vu,
            evidence = rec.evidence,
            links = rec.links,
          }
          local ok, new_rev = enqueue_managed_object(rec.object_id, rec.revision, payload)
          if ok then
            promoted_n = promoted_n + 1
            if new_rev and new_rev ~= "" then rec.revision = new_rev end
          end
        end
        active_records[#active_records + 1] = rec
      end
    end
  end

  -- 코어 티어 예산 관리 (2,400자 상한의 80% = 1,920자 이내로 초과분 reference 강등)
  local core_list = {}
  local core_chars = 0
  for _, rec in ipairs(active_records) do
    if tostring(rec.tier or "") == "core" and tostring(rec.status or "observed") == "observed" then
      local summary = tostring(rec.description and rec.description ~= "" and rec.description or rec.text or "")
      local cost = #("- [" .. tostring(rec.memory_kind or "lesson") .. "] " .. summary .. "\n")
      rec._core_cost = cost
      core_chars = core_chars + cost

      -- 유예 기간(Grace Period) 및 KeepScore 산출
      local rec_epoch = tonumber(rec.updated_epoch or "0") or (rec.updated_at and epoch_of(rec.updated_at)) or 0
      local age_seconds = math.max(0, now_epoch - rec_epoch)
      local is_nursery = rec_epoch > 0 and (age_seconds < NURSERY_GRACE_PERIOD_SECONDS)
      rec._is_nursery = is_nursery
      local age_days = age_seconds / 86400
      local keep_score = (rec._session_count or 1) / (age_days + 1)
      rec._keep_score = keep_score

      core_list[#core_list + 1] = rec
    end
  end

  -- backlog_overflow의 경우 인박스 소화 후 이미 코어가 정상이면 추가 LLM/강등 스킵
  if origin == "pressure" and reason == "backlog_overflow" and core_chars <= CORE_DREAM_TARGET then
    local summary = string.format("engram: dream 완료 (오리진=%s, 사유=%s, 백로그 소화 완료, 코어=%d자 정상)", origin, reason, core_chars)
    magi.log(summary)
    return summary
  end

  if core_chars > CORE_DREAM_TARGET then
    -- 강등 우선순위 정렬:
    -- 1) 신규 기억 유예(Nursery) 항목은 맨 뒤로 보호
    -- 2) KeepScore(재인용/경과일수)가 낮은 항목(오래되었는데 인용 없는 죽은 기억)부터 강등
    table.sort(core_list, function(a, b)
      if a._is_nursery ~= b._is_nursery then
        return not a._is_nursery
      end
      if math.abs((a._keep_score or 0) - (b._keep_score or 0)) > 0.0001 then
        return (a._keep_score or 0) < (b._keep_score or 0)
      end
      return tostring(a.updated_at or "") < tostring(b.updated_at or "")
    end)
    for _, rec in ipairs(core_list) do
      if core_chars <= CORE_DREAM_TARGET then break end
      rec.tier = "reference"
      local payload = {
        kind = "memory",
        state = "active",
        host_outcome = rec.host_outcome,
        text = tostring(rec.text or ""),
        description = tostring(rec.description or ""),
        memory_kind = tostring(rec.memory_kind or ""),
        areas = rec.areas,
        tier = "reference",
        status = tostring(rec.status or "observed"),
        valid_until = tostring(rec.valid_until or ""),
        evidence = rec.evidence,
        links = rec.links,
      }
      local ok, new_rev = enqueue_managed_object(rec.object_id, rec.revision, payload)
      if ok then
        core_chars = core_chars - (rec._core_cost or 0)
        demoted_n = demoted_n + 1
        if new_rev and new_rev ~= "" then rec.revision = new_rev end
      end
    end
  end

  -- 2단계 (하이브리드 병합):
  -- idle: 유사도 >= 0.82 중복 클러스터나 conflicted 상태가 있을 때만 호출
  -- pressure: 여전히 1,920자 초과 시 코어 항목들을 대상으로 초고밀도 합성(Split & Link) 호출
  local by_id = {}
  for _, rec in ipairs(active_records) do by_id[rec.object_id] = rec end
  local visited = {}
  local clusters = {}

  local function have_compatible_areas(a, b)
    if not a or not b then return true end
    local areas_a = type(a.areas) == "table" and a.areas or {}
    local areas_b = type(b.areas) == "table" and b.areas or {}
    if #areas_a == 0 or #areas_b == 0 then return true end
    local set_a = {}
    for _, area in ipairs(areas_a) do
      local s = string.lower(string.gsub(tostring(area or ""), "^%s*(.-)%s*$", "%1"))
      if s ~= "" then set_a[s] = true end
    end
    for _, area in ipairs(areas_b) do
      local s = string.lower(string.gsub(tostring(area or ""), "^%s*(.-)%s*$", "%1"))
      if s ~= "" and set_a[s] then return true end
    end
    return false
  end

  if origin == "pressure" and core_chars > CORE_DREAM_TARGET then
    -- Pressure 모드: 남은 코어 항목들을 유사도 기준 또는 인접 클러스터로 묶어 고밀도 축약
    for _, rec in ipairs(active_records) do
      if tostring(rec.tier or "") == "core" and not visited[rec.object_id] then
        local q = tostring(rec.description or "") .. " " .. tostring(rec.text or "")
        local sim_hits = magi.search_experience{
          query = q,
          kind = "memory",
          state = "active",
          limit = 4,
          min_similarity = 0.65,
        }
        local group = { rec }
        visited[rec.object_id] = true
        if type(sim_hits) == "table" then
          for _, h in ipairs(sim_hits) do
            if type(h) == "table" and h.object_id and h.object_id ~= rec.object_id and not visited[h.object_id] and by_id[h.object_id] then
              if tostring(by_id[h.object_id].tier or "") == "core" and have_compatible_areas(rec, by_id[h.object_id]) then
                visited[h.object_id] = true
                group[#group + 1] = by_id[h.object_id]
              end
            end
          end
        end
        if #group >= 2 then
          clusters[#clusters + 1] = group
        end
      end
    end
  else
    for _, rec in ipairs(active_records) do
      if not visited[rec.object_id] then
        local q = tostring(rec.description or "") .. " " .. tostring(rec.text or "")
        local sim_hits = magi.search_experience{
          query = q,
          kind = "memory",
          state = "active",
          limit = 6,
          min_similarity = 0.82,
        }
        local group = { rec }
        visited[rec.object_id] = true
        if type(sim_hits) == "table" then
          for _, h in ipairs(sim_hits) do
            if type(h) == "table" and h.object_id and h.object_id ~= rec.object_id and not visited[h.object_id] and by_id[h.object_id] then
              if have_compatible_areas(rec, by_id[h.object_id]) then
                visited[h.object_id] = true
                group[#group + 1] = by_id[h.object_id]
              end
            end
          end
        end
        if #group >= 2 or tostring(rec.status or "") == "conflicted" then
          clusters[#clusters + 1] = group
        end
      end
    end
  end

  if #clusters > 0 then
    local cblocks = {}
    for ci, grp in ipairs(clusters) do
      local lines = { "[클러스터 " .. ci .. "]" }
      for _, m in ipairs(grp) do
        lines[#lines + 1] = "- id=" .. tostring(m.object_id)
          .. " status=" .. tostring(m.status or "observed")
          .. " tier=" .. tostring(m.tier or "reference")
          .. " desc=" .. tostring(m.description or "")
          .. " :: " .. tostring(m.text or "")
      end
      cblocks[#cblocks + 1] = table.concat(lines, "\n")
    end
    local model = magi.store_get("sidecar_model")
    local sys_prompt = (origin == "pressure") and DREAM_PRESSURE_PROMPT or DREAM_PROMPT
    local raw = magi.analyze{ system = sys_prompt, text = table.concat(cblocks, "\n\n"), model = model }
    local parsed = parse_cortex_json(raw)
    if type(parsed) == "table" and type(parsed.merges) == "table" then
      for _, mg in ipairs(parsed.merges) do
        if type(mg) == "table" and mg.keep_id and by_id[mg.keep_id] and mg.text and tostring(mg.text) ~= "" then
          local keep = by_id[mg.keep_id]
          local links = {}
          if type(keep.links) == "table" then
            for _, lk in ipairs(keep.links) do links[#links + 1] = lk end
          end
          if type(mg.archive_ids) == "table" then
            for _, aid in ipairs(mg.archive_ids) do
              aid = tostring(aid or "")
              if aid ~= "" and aid ~= keep.object_id and by_id[aid] then
                links[#links + 1] = aid
                local target = by_id[aid]
                -- Pressure 모드에서는 원본 디테일이 손실되지 않도록 state="active", tier="reference"로 보존(Split & Link)
                local archive_state = (origin == "pressure") and "active" or "archived"
                local archive_tier = "reference"
                local ok, new_rev = enqueue_managed_object(aid, target.revision, {
                  kind = "memory",
                  state = archive_state,
                  host_outcome = target.host_outcome,
                  text = tostring(target.text or ""),
                  description = tostring(target.description or ""),
                  memory_kind = tostring(target.memory_kind or ""),
                  areas = target.areas,
                  tier = archive_tier,
                  status = tostring(target.status or "observed"),
                  valid_until = tostring(target.valid_until or ""),
                  evidence = target.evidence,
                  links = { keep.object_id },
                })
                if ok and new_rev and new_rev ~= "" then target.revision = new_rev end
              end
            end
          end
          local keep_payload = {
            kind = "memory",
            state = "active",
            host_outcome = keep.host_outcome or "verified",
            text = tostring(mg.text),
            description = tostring(mg.description and mg.description ~= "" and mg.description or keep.description or ""),
            memory_kind = tostring(mg.kind and mg.kind ~= "" and mg.kind or keep.memory_kind or "lesson"),
            areas = type(mg.areas) == "table" and #mg.areas > 0 and mg.areas or keep.areas,
            tier = tostring(mg.tier and mg.tier ~= "" and mg.tier or keep.tier or "reference"),
            status = tostring(mg.status and mg.status ~= "" and mg.status or "observed"),
            valid_until = tostring(keep.valid_until or ""),
            evidence = keep.evidence,
            links = links,
          }
          local ok, new_rev = enqueue_managed_object(keep.object_id, keep.revision, keep_payload)
          if ok then
            merged_n = merged_n + 1
            if new_rev and new_rev ~= "" then keep.revision = new_rev end
          end
        end
      end
    end
  end

  -- 최종 수축 계약 검증 (Contract Guarantee: 1,920자 이하 필수 보장)
  if origin == "pressure" and core_chars > CORE_DREAM_TARGET then
    for _, rec in ipairs(core_list) do
      if core_chars <= CORE_DREAM_TARGET then break end
      if tostring(rec.tier or "") == "core" then
        rec.tier = "reference"
        local payload = {
          kind = "memory",
          state = "active",
          host_outcome = rec.host_outcome,
          text = tostring(rec.text or ""),
          description = tostring(rec.description or ""),
          memory_kind = tostring(rec.memory_kind or ""),
          areas = rec.areas,
          tier = "reference",
          status = tostring(rec.status or "observed"),
          valid_until = tostring(rec.valid_until or ""),
          evidence = rec.evidence,
          links = rec.links,
        }
        local ok, new_rev = enqueue_managed_object(rec.object_id, rec.revision, payload)
        if ok then
          core_chars = core_chars - (rec._core_cost or 0)
          demoted_n = demoted_n + 1
          if new_rev and new_rev ~= "" then rec.revision = new_rev end
        end
      end
    end
  end

  local summary = string.format(
    "engram: dream 완료 (오리진=%s, 사유=%s, 만료=%d, 승격=%d, 코어강등=%d, 클러스터병합=%d, LLM호출=%d, 최종코어=%d자)",
    tostring(origin), tostring(reason), expired_n, promoted_n, demoted_n, merged_n, #clusters > 0 and 1 or 0, core_chars
  )
  magi.store_set("dream_last_date_v1:" .. magi.workdir(), string.sub(magi.time(), 1, 10))
  magi.store_set("dream_dirty_v1:" .. magi.workdir(), "0")
  magi.store_set("pressure_last_chars_v1:" .. magi.workdir(), tostring(core_chars))
  magi.log(summary)
  notify_knowledge_change()
  return summary
end

-- 턴 누적 트리거 조건: 25턴 이상 쌓였더라도 실제로 성찰할 만한 관찰(교훈 후보)이
-- 최소 REFLECT_MIN_OBSERVATIONS(기본 3건) 이상 누적되었고 그중 최소 1건이 확정 결과(verified/error/guard)일 때만 실행.
local function has_worthwhile_reflection_backlog()
  local min_obs = tonumber(magi.store_get("reflect_min_observations") or "") or REFLECT_MIN_OBSERVATIONS
  local rkey = "reflection_inbox_v1:" .. magi.workdir()
  local rraw = magi.store_get(rkey) or ""
  local items = magi.json_decode(rraw == "" and "[]" or rraw)
  if type(items) ~= "table" then items = {} end
  local fitems = load_facts_inbox()
  local total = #items + #fitems
  if total < min_obs then
    return false
  end
  for _, it in ipairs(items) do
    local out = type(it) == "table" and tostring(it.outcome or "") or ""
    if out == "verified" or out == "error" or out == "guard" then
      return true
    end
  end
  return #items >= min_obs
end

-- 유휴 dream: 사용자 입력도 진행 중인 턴도 없는 유휴 구간(호스트 "idle" 이벤트, 기본 30분)에
-- 이전 dream 실행 후 최소 12시간이 지났고 새 관찰(dream_dirty_v1)이 쌓였을 때만 실행한다.
-- 사람이 작업하는 동안에는 토큰도 시간도 쓰지 않는다.
local DREAM_INTERVAL_SECONDS = 12 * 3600

local function maybe_idle_dream()
  if not flag("auto_dream", true) or not recordLessons() then return end
  local dkey = "dream_dirty_v1:" .. magi.workdir()
  if (magi.store_get(dkey) or "") ~= "1" then return end

  local ekey = "dream_last_epoch_v1:" .. magi.workdir()
  local last_epoch = tonumber(magi.store_get(ekey) or "0") or 0
  local now_epoch = epoch_of(magi.time()) or 0
  if last_epoch > 0 and (now_epoch - last_epoch) < DREAM_INTERVAL_SECONDS then
    return
  end

  magi.store_set(dkey, "0")
  magi.store_set(ekey, tostring(now_epoch))
  magi.store_set("dream_last_date_v1:" .. magi.workdir(), string.sub(magi.time(), 1, 10))
  run_dream("idle", "interval")
end

local function check_memory_pressure()
  if not flag("auto_dream", true) or not recordLessons() then
    return false, nil, 0
  end

  -- 1) 코어 티어 문자 수 초과 압박
  local core_chars = 0
  if type(magi.search_experience) == "function" then
    local cores = magi.search_experience{
      query = "",
      kind = "memory",
      state = "active",
      limit = 200,
      min_similarity = 0,
    }
    if type(cores) == "table" then
      for _, rec in ipairs(cores) do
        if tostring(rec.tier or "") == "core" and tostring(rec.status or "observed") == "observed" then
          local summary = tostring(rec.description and rec.description ~= "" and rec.description or rec.text or "")
          core_chars = core_chars + #("- [" .. tostring(rec.memory_kind or "lesson") .. "] " .. summary .. "\n")
        end
      end
    end
  end
  if core_chars >= CORE_CHAR_BUDGET then
    return true, "core_overflow", core_chars
  end

  -- 2) 미성찰 인박스 백로그 과적 압박
  local backlog_n = reflection_backlog_count()
  if backlog_n >= MAX_UNREFLECTED_BACKLOG then
    return true, "backlog_overflow", core_chars
  end

  return false, nil, core_chars
end

local function maybe_pressure_dream(reason, core_chars)
  if not flag("auto_dream", true) or not recordLessons() then return false end

  local now_epoch = epoch_of(magi.time()) or 0
  local ekey = "pressure_last_epoch_v1:" .. magi.workdir()
  local last_epoch = tonumber(magi.store_get(ekey) or "0") or 0

  -- 1. 전용 최소 쿨다운 (5분) 스래싱 방어
  if last_epoch > 0 and (now_epoch - last_epoch) < PRESSURE_COOLDOWN_SECONDS then
    return false
  end

  -- 2. 워터마크 히스테리시스 점검 (core_overflow 시)
  local ckey = "pressure_last_chars_v1:" .. magi.workdir()
  local last_chars = tonumber(magi.store_get(ckey) or "0") or 0
  if reason == "core_overflow" and last_chars > 0 and core_chars > 0 then
    if (core_chars - last_chars) < PRESSURE_HYSTERESIS_CHARS and core_chars <= CORE_CHAR_BUDGET + 100 then
      return false
    end
  end

  magi.store_set(ekey, tostring(now_epoch))
  magi.store_set(ckey, tostring(core_chars or 0))
  run_dream("pressure", reason)
  return true
end

magi.on("turn_finished", function(ev)
  flush_managed()
  if is_dream_session(ev.session) then
    return
  end
  push_recent(ev.session, "assistant", ev.text)
  local outcome = ev.outcome or "done"
  update_usage(ev.skills, outcome)
  if ANALYZE_OUTCOMES[outcome] and recordLessons() and not is_classifier_blocked(ev.reason) then
    local user = ev.user
    if user == "" then user = nil end
    enqueue_facts_inbox(ev.session, outcome_hint(outcome, ev.reason), user, ev.skills, outcome)
  end
  -- 턴이 많이 쌓이는 경우(25턴마다)에도 성찰할 만한 관찰 백로그(최소 3건 이상)가 있을 때만
  -- 백그라운드에서 성찰(reflection)로 저장소를 갱신하되, 현 세션의 `# Core memory` 시스템 프롬프트
  -- 주입은 호스트(`coreBlockFor` / `resetMemoryDelivery`)가 다음 세션 컴팩션이나 재시작까지 지연한다.
  local tkey = "reflection_turns_v1:" .. magi.workdir()
  local turns = (tonumber(magi.store_get(tkey) or "0") or 0) + 1
  if turns >= REFLECT_INTERVAL then
    if recordLessons() and has_worthwhile_reflection_backlog() then
      magi.store_set(tkey, "0")
      run_reflection("turn_interval", false)
    else
      magi.store_set(tkey, tostring(turns))
    end
  else
    magi.store_set(tkey, tostring(turns))
  end

  -- Pressure Dream 점검 (코어 2,400자 상한 초과 또는 미성찰 백로그 과적 시 긴급 압축 발동)
  local need_pressure, p_reason, p_chars = check_memory_pressure()
  if need_pressure then
    maybe_pressure_dream(p_reason, p_chars)
  end

  prune_stale() -- archive engram-owned skills unused past the cutoff (count/date only)
end)

magi.on("context_compacted", function(ev)
  if not recordLessons() then return end
  if ev and is_dream_session(ev.session) then return end
  magi.store_set("reflection_turns_v1:" .. magi.workdir(), "0")
  run_reflection("context_compacted", false, ev and ev.summary or nil)
end)

magi.on("shutdown", function()
  if not recordLessons() then return end
  magi.store_set("reflection_turns_v1:" .. magi.workdir(), "0")
  if has_worthwhile_reflection_backlog() then
    run_reflection("shutdown", false)
  else
    flush_facts_inbox(true, 1)
  end
end)

-- 유휴(기본 30분 무입력·무진행 턴): 쌓인 관찰이 1건이라도 있으면 성찰하고,
-- 최소 12시간이 지났으며 새 관찰이 있었으면 dream까지 돈다.
magi.on("idle", function()
  embed_lease_touch()
  if not recordLessons() then return end
  if reflection_backlog_count() > 0 then
    magi.store_set("reflection_turns_v1:" .. magi.workdir(), "0")
    run_reflection("idle", false)
  end
  maybe_idle_dream()
end)

magi.register_command{
  name = "engram-reflect",
  description = "누적된 관찰 inbox와 관리 기억을 즉시 대조해 성찰(reflection)을 수행합니다.",
  execute = function()
    return run_reflection("manual_command", true)
  end,
}

magi.register_command{
  name = "engram-flush-facts",
  description = "누적된 팩트(Facts) inbox를 즉시 분석 및 영구 저장소에 반영합니다.",
  execute = function()
    local ok = flush_facts_inbox(true, 1)
    if ok then
      return "engram: 팩트 inbox 플러시 완료."
    else
      return "engram: 플러시할 팩트 inbox가 비어 있습니다."
    end
  end,
}

magi.register_command{
  name = "engram-dream",
  description = "기억 만료·가설 승격·코어 티어 예산 정리 및 중복 클러스터 병합(dream)을 수행합니다.",
  execute = function()
    return run_dream()
  end,
}

local function load_reflection_inbox()
  local raw = magi.store_get("reflection_inbox_v1:" .. magi.workdir()) or ""
  local items = magi.json_decode(raw == "" and "[]" or raw)
  if type(items) ~= "table" then return {} end
  return items
end

magi.register_slot{
  id = "engram",
  target = "knowledge",
  title = "Engram 자가 학습 메모리",
  description = "단기 관찰 사실과 세션 간 교훈을 관리합니다.",
  query = function()
    local facts_inbox = load_facts_inbox()
    local refl_inbox = load_reflection_inbox()
    local facts_count = (facts_inbox and #facts_inbox) or 0
    local refl_count = (refl_inbox and #refl_inbox) or 0
    local dirty = (magi.store_get("dream_dirty_v1:" .. magi.workdir()) or "0") ~= "0"
    local total_pending = facts_count + refl_count

    local status_label = "정상"
    local status_variant = "info"
    if dirty or total_pending > 0 then
      status_label = "정리 대기"
      status_variant = "warning"
    end

    return {
      status = {
        label = status_label,
        variant = status_variant,
        badge = total_pending,
      },
      items = {
        { id = "facts", label = "미정리 팩트", value = tostring(facts_count) .. "건", warning = facts_count > 10 },
        { id = "reflection", label = "대기 교훈", value = tostring(refl_count) .. "건", warning = refl_count > 5 },
        { id = "dream", label = "드림 상태", value = dirty and "정리 필요" or "최신", warning = dirty },
      },
      actions = {
        { id = "reflect", label = "회고 (Reflect)", description = "사실에서 교훈 도출", variant = "primary" },
        { id = "dream", label = "드림 (Dream)", description = "장기 기억 압축 및 스킬화" },
        { id = "flush_facts", label = "팩트 플러시", description = "인박스 즉시 저장" },
      },
    }
  end,
  execute = function(action_id)
    if action_id == "reflect" or action_id == "engram-reflect" then
      return run_reflection("manual_slot", true)
    elseif action_id == "dream" or action_id == "engram-dream" then
      return run_dream()
    elseif action_id == "flush_facts" or action_id == "engram-flush-facts" then
      local ok = flush_facts_inbox(true, 1)
      if ok then
        return "engram: 팩트 inbox 플러시 완료."
      else
        return "engram: 플러시할 팩트 inbox가 비어 있습니다."
      end
    end
    error("unknown slot action: " .. tostring(action_id))
  end,
}

magi.register_tool{
  name = "engram_dream",
  description = "Engram 관리 기억의 만료 정리, 승격, 코어 예산 조정 및 중복 병합을 수행하는 내부 도구입니다.",
  internal = true,
  schema = { type = "object", properties = {} },
  execute = function()
    return run_dream()
  end,
}

-- Evidence remains local to this workspace and separate from causal claims.
magi.register_command{
  name = "engram-evidence",
  description = "최근 학습의 변경·대화 근거·불확실성을 조회합니다 (최대 10개).",
  execute = function()
    local rows = magi.json_decode(magi.store_get("learning_evidence_v1:" .. magi.workdir()) or "[]")
    if type(rows) ~= "table" then return "engram: learning evidence is malformed; stored data was preserved" end
    local out = {}
    for i = math.max(1, #rows - 9), #rows do out[#out + 1] = rows[i] end
    return magi.json_encode(out)
  end,
}

-- 로컬 임베딩 서버 연결 및 백그라운드 컨테이너/모델 프로비저닝.
-- 이미 떠 있는 루프백 서버가 있으면 즉시 연결하고(magi.set_embed_endpoint), 없으면 docker/podman 으로
-- magi-engram-embed(ollama/ollama) 컨테이너 기동과 bge-m3 모델 내려받기를 백그라운드 파이프(magi.pipe)로
-- 진행한다. 준비가 끝날 때까지 회상은 어휘 검색으로 동작하며 턴을 블로킹하지 않는다.
-- config.toml 에 embed_model 을 직접 둔 사용자는 그 설정을 존중해 손대지 않는다.
local EMBED_DEFAULT_URL = "http://127.0.0.1:11434/v1"
local EMBED_FALLBACK_URL = "http://127.0.0.1:11435/v1"
local EMBED_DEFAULT_MODEL = "bge-m3"
local EMBED_CONTAINER = "magi-engram-embed"
local EMBED_VOLUME = "magi-engram-ollama:/root/.ollama"
local EMBED_IMAGE = "ollama/ollama"
local embed_state = { why = "아직 확인하지 않았습니다" }
local embed_bg = { pipe = nil, stage = nil, rt = nil, arch = nil, want = nil, url = nil }

local function embed_setting(key)
  local v = magi.store_get(key)
  if v == nil then return nil end
  v = tostring(v)
  if v == "" then return nil end
  return v
end

local function embed_target_url()
  return embed_setting("embed_url") or embed_setting("embed_managed_url") or EMBED_DEFAULT_URL
end

-- 사용자가 config.toml 에 embed_model 을 직접 정했는가. 읽을 수 없으면(설정 경로 없음 등) 아니라고 본다.
local function user_embed_configured()
  local ok, v = pcall(magi.get_config_key, "embed_model", "")
  return ok and v ~= nil and tostring(v) ~= ""
end

local function in_test_env()
  if type(magi.in_test) == "function" and magi.in_test() then
    return true
  end
  if flag("in_test", false) then
    return true
  end
  return false
end

local function embed_close_pipe()
  if embed_bg.pipe then
    pcall(embed_bg.pipe.close)
    embed_bg.pipe = nil
  end
end

-- 컨테이너 런타임(docker -> podman)과 데몬 아키텍처(Server.Arch)를 감지해 캐시한다.
local function embed_select_runtime(force)
  if force then
    magi.store_set("embed_runtime", "")
    magi.store_set("embed_arch", "")
    magi.store_set("embed_image", "")
  end
  local cached_rt = embed_setting("embed_runtime")
  if cached_rt then
    local r = magi.exec(cached_rt, { "version", "--format", "{{.Server.Arch}}" }, { timeout = "5s" })
    if r and r.code == 0 then
      local arch = (r.stdout or ""):gsub("%s+", "")
      if arch == "" then arch = "unknown" end
      local img = embed_setting("embed_image") or EMBED_IMAGE
      magi.store_set("embed_arch", arch)
      magi.store_set("embed_image", img)
      return cached_rt, arch, img
    end
  end
  for _, cand in ipairs({ "docker", "podman" }) do
    local r = magi.exec(cand, { "version", "--format", "{{.Server.Arch}}" }, { timeout = "5s" })
    if r and r.code == 0 then
      local arch = (r.stdout or ""):gsub("%s+", "")
      if arch == "" then arch = "unknown" end
      magi.store_set("embed_runtime", cand)
      magi.store_set("embed_arch", arch)
      magi.store_set("embed_image", EMBED_IMAGE)
      return cand, arch, EMBED_IMAGE
    end
  end
  return nil, nil, nil
end

-- base 의 /models 에서 want 모델(태그 포함, 예: bge-m3:latest)을 찾아 그 id 를 돌려준다.
-- Ollama 는 설치된 모델이 0개일 때 {"object":"list","data":null} 을 반환하므로 이를 빈 목록으로 처리한다.
local function embed_probe(base, want)
  -- This runs on the user_message and turn_finished paths with the plugin lock held.
  local r, err = magi.http{ url = (base:gsub("/+$", "")) .. "/models", timeout = "2s" }
  if not r then return nil, tostring(err) end
  if r.status ~= 200 then return nil, "HTTP " .. tostring(r.status) end
  local doc = magi.json_decode(r.body or "")
  if type(doc) ~= "table" or (type(doc.data) ~= "table" and doc.object ~= "list") then
    return nil, "모델 목록 형식이 아닙니다"
  end
  for _, m in ipairs(type(doc.data) == "table" and doc.data or {}) do
    local id = type(m) == "table" and tostring(m.id or "") or ""
    if id == want or id:sub(1, #want + 1) == want .. ":" then return id end
  end
  return nil, want .. " 모델이 없습니다"
end

local function embed_connect()
  if user_embed_configured() then
    embed_state = { why = "config.toml 의 embed_model 설정을 사용합니다(플러그인은 연결하지 않음)" }
    return false
  end
  local url = embed_target_url()
  local want = embed_setting("embed_model") or EMBED_DEFAULT_MODEL
  local id, why = embed_probe(url, want)
  if not id and embed_setting("embed_url") == nil and url ~= EMBED_DEFAULT_URL then
    local def_id = embed_probe(EMBED_DEFAULT_URL, want)
    if def_id then
      url = EMBED_DEFAULT_URL
      id = def_id
    end
  end
  if not id then
    embed_state = { url = url, why = why }
    return false
  end
  local ok, err = magi.set_embed_endpoint(url, id)
  if not ok then
    embed_state = { url = url, why = tostring(err) }
    return false
  end
  embed_close_pipe()
  embed_bg.stage = nil
  embed_state = { url = url, model = id }
  return true
end

local function embed_launch_pull(rt, arch, url, want)
  embed_close_pipe()
  local p, err = magi.pipe(rt, { "exec", EMBED_CONTAINER, "ollama", "pull", want }, { idle = "10m" })
  if not p then
    embed_bg.stage = nil
    embed_state = { url = url, why = want .. " 모델이 없습니다 (내려받기 시작 실패: " .. tostring(err) .. ")" }
    return false
  end
  embed_bg = { pipe = p, stage = "pull", rt = rt, arch = arch, want = want, url = url }
  embed_state = { url = url, why = want .. " 모델이 없습니다 — 백그라운드에서 내려받는 중 (" .. rt .. "/" .. arch .. ")" }
  magi.log("engram: 임베딩 모델 백그라운드 내려받기 시작 — " .. want .. " (" .. rt .. "/" .. arch .. ")")
  return false
end

local function embed_start(explicit)
  if user_embed_configured() then
    embed_state = { why = "config.toml 의 embed_model 설정을 사용합니다(플러그인은 연결하지 않음)" }
    return false
  end
  if embed_connect() then return true end
  if not explicit and in_test_env() and embed_setting("embed_auto_start") ~= "true" then
    return false
  end
  if not explicit then
    if embed_setting("embed_url") ~= nil and embed_setting("embed_auto_start") == nil then
      return false
    end
    if not flag("embed_auto_start", true) then
      return false
    end
  end
  if embed_bg.pipe and embed_bg.pipe.alive() then
    return false
  end
  local url = embed_target_url()
  local want = embed_setting("embed_model") or EMBED_DEFAULT_MODEL
  local rt, arch, img = embed_select_runtime(false)
  if not rt then
    embed_state = { url = url, why = tostring(embed_state.why or "연결 실패") .. " (docker/podman 런타임을 찾지 못함)" }
    return false
  end
  local ins = magi.exec(rt, { "inspect", "--format", "{{.State.Running}}", EMBED_CONTAINER }, { timeout = "5s" })
  if ins and ins.code == 0 then
    local running = (ins.stdout or ""):gsub("%s+", "") == "true"
    if not running then
      magi.exec(rt, { "start", EMBED_CONTAINER }, { timeout = "15s" })
    end
    if embed_connect() then return true end
    url = embed_target_url()
    if embed_state.why == want .. " 모델이 없습니다" then
      return embed_launch_pull(rt, arch, url, want)
    end
    embed_bg = { pipe = nil, stage = "wait", rt = rt, arch = arch, want = want, url = url }
    embed_state = { url = url, why = "컨테이너 기동 대기 중 (" .. rt .. "/" .. arch .. ")" }
    return false
  end
  -- 기본 11434 포트를 다른 로컬 서버(예: 호스트 Ollama)가 이미 쓰고 있고 모델만 없는 상태라면
  -- 포트 충돌을 피하기 위해 전용 루프백 포트(11435)로 컨테이너를 띄운다.
  if embed_setting("embed_url") == nil and url == EMBED_DEFAULT_URL and embed_state.why == want .. " 모델이 없습니다" then
    url = EMBED_FALLBACK_URL
    magi.store_set("embed_managed_url", url)
  end
  local port = url:match("^https?://[^:/]+:(%d+)") or "11434"
  local p, err = magi.pipe(rt, {
    "run", "-d", "--name", EMBED_CONTAINER,
    "-p", "127.0.0.1:" .. port .. ":11434",
    "-v", EMBED_VOLUME,
    img,
  }, { idle = "10m" })
  if not p then
    embed_state = { url = url, why = "컨테이너 실행 실패: " .. tostring(err) }
    return false
  end
  embed_bg = { pipe = p, stage = "run", rt = rt, arch = arch, want = want, url = url }
  embed_state = { url = url, why = "컨테이너 준비 중 — 백그라운드에서 기동 중 (" .. rt .. "/" .. arch .. ")" }
  magi.log("engram: 임베딩 컨테이너 백그라운드 기동 시작 — " .. EMBED_CONTAINER .. " (" .. rt .. "/" .. arch .. ")")
  return false
end

local function embed_tick()
  if embed_state.model or user_embed_configured() or not embed_bg.stage then
    return embed_state.model ~= nil
  end
  if embed_bg.pipe then
    if embed_bg.pipe.alive() then
      return false
    end
    embed_close_pipe()
  end
  local stage = embed_bg.stage
  local rt, arch = embed_bg.rt, embed_bg.arch
  local want = embed_bg.want or embed_setting("embed_model") or EMBED_DEFAULT_MODEL
  local url = embed_bg.url or embed_target_url()
  if embed_connect() then
    magi.log("engram: 임베딩 서버 자동 연결 완료 — " .. tostring(embed_state.url) .. " (" .. tostring(embed_state.model) .. ")")
    return true
  end
  if stage == "run" or stage == "wait" then
    if embed_state.why == want .. " 모델이 없습니다" then
      return embed_launch_pull(rt, arch, url, want)
    end
    embed_bg.stage = "wait"
    embed_state = { url = url, why = "컨테이너 기동 대기 중 (" .. tostring(rt) .. "/" .. tostring(arch) .. ")" }
    return false
  end
  embed_bg.stage = nil
  return false
end

local function embed_status()
  local rt = embed_setting("embed_runtime")
  local arch = embed_setting("embed_arch")
  local extra = (rt and arch) and (" [" .. rt .. "/" .. arch .. "]") or ""
  if embed_state.model then
    return "engram: 임베딩 서버 연결됨 — " .. embed_state.url .. " (" .. embed_state.model .. ")" .. extra
  end
  return "engram: 임베딩 서버 미연결 — " .. (embed_state.url and (embed_state.url .. ": ") or "") .. tostring(embed_state.why) .. extra ..
    "\n의미 검색을 쓰려면 `/engram-embed start` 로 컨테이너·모델을 준비하거나, 루프백 서버에 bge-m3 를 받은 뒤 `/engram-embed connect` 를 실행하세요." ..
    "\n다른 주소·모델은 [plugins.engram] embed_url / embed_model 로 지정합니다(루프백만 허용)."
end

-- ── 임베딩 컨테이너 수명: 컴패니언 임대(lease) ───────────────────────────────
-- 컨테이너 하나를 여러 magi 프로세스(컴패니언)가 같이 쓴다. 프로세스마다 임대 한 줄을 플러그인
-- 저장소(프로세스 간 공유)에 두고 활동할 때마다 갱신한다. 종료하는 프로세스는 자기 임대를 지우고,
-- 살아 있는 임대가 하나도 남지 않으면 — 즉 연결한 컴패니언이 모두 끝나면 — 컨테이너를 멈춘다.
-- 오래 갱신되지 않은 임대(EMBED_LEASE_STALE)는 죽은 프로세스의 것으로 보고 정리한다. 오래 잠든
-- 프로세스의 임대가 그렇게 지워졌다가 다시 깨어나면, 다음 활동에서 컨테이너를 다시 띄운다.
local EMBED_LEASE_KEY = "embed_leases_v1"
local EMBED_LEASE_STALE = 6 * 3600
local embed_lease_id = magi.nonce(8)

local function embed_managed_here()
  return embed_setting("embed_url") == nil and not user_embed_configured()
end

-- lease_update(fn): 임대 표를 CAS로 고친다. fn(map, now)가 표를 바꾸고 결과를 돌려준다.
local function lease_update(fn)
  for _ = 1, 5 do
    local raw = magi.store_get(EMBED_LEASE_KEY) or ""
    local map = magi.json_decode(raw == "" and "{}" or raw)
    if type(map) ~= "table" then map = {} end
    local now = epoch_of(magi.time()) or 0
    local result = fn(map, now)
    local enc = next(map) == nil and "" or magi.json_encode(map)
    if magi.store_compare_exchange(EMBED_LEASE_KEY, raw, enc) then return result end
  end
  return nil
end

-- 임대를 갱신하고, 자기 임대가 사라져 있었는지(다른 프로세스가 컨테이너를 멈췄을 수 있음)를 돌려준다.
embed_lease_touch = function()
  return lease_update(function(map, now)
    local missing = map[embed_lease_id] == nil
    map[embed_lease_id] = now
    return missing
  end)
end

-- 자기 임대를 지우고, 살아 있는 다른 임대가 남았는지를 돌려준다(오래된 임대는 함께 정리).
local function embed_lease_release()
  return lease_update(function(map, now)
    map[embed_lease_id] = nil
    local alive = false
    for id, at in pairs(map) do
      if now - (tonumber(at) or 0) > EMBED_LEASE_STALE then
        map[id] = nil
      else
        alive = true
      end
    end
    return alive
  end)
end

local embed_last_health_check = 0

local function embed_activity()
  if in_test_env() then return end
  local was_missing = embed_lease_touch()
  local now = epoch_of(magi.time()) or 0
  -- 처음 붙는 경우(startup)가 아니라 활동 중인데 임대가 사라져 있었다면, 다른 컴패니언이 마지막으로
  -- 끝나며 컨테이너를 멈춘 것이다. 우리가 관리하는 컨테이너면 다시 띄운다.
  if was_missing and embed_state.model and embed_managed_here() then
    embed_state = { why = "다른 컴패니언 종료로 컨테이너가 멈춰 다시 기동합니다" }
    embed_start(false)
  elseif embed_state.model and embed_managed_here() and (now - embed_last_health_check) >= 60 then
    embed_last_health_check = now
    local want = embed_setting("embed_model") or EMBED_DEFAULT_MODEL
    local id = embed_probe(embed_state.url or embed_target_url(), want)
    if not id then
      magi.log("engram: 임베딩 서버 연결 끊김 감지 — 자동 재기동 시도")
      embed_state = { why = "임베딩 컨테이너 응답 없음(재기동 시도)" }
      embed_start(false)
    end
  end
  embed_tick()
end

magi.on("startup", function()
  embed_lease_touch()
  embed_start(false)
end)

magi.on("user_message", function()
  embed_activity()
end)

magi.on("turn_finished", function()
  embed_activity()
end)

magi.on("shutdown", function()
  if in_test_env() then return end
  embed_close_pipe()
  local others = embed_lease_release()
  if others ~= false then return end -- 다른 컴패니언이 아직 쓰는 중(또는 임대 갱신 실패 → 안전하게 유지)
  if not flag("embed_auto_stop", true) or not embed_managed_here() then return end
  local rt = embed_setting("embed_runtime")
  if not rt then return end
  local r = magi.exec(rt, { "stop", "-t", "2", EMBED_CONTAINER }, { timeout = "20s" })
  if r and r.code == 0 then
    magi.log("engram: 연결한 컴패니언이 모두 종료되어 임베딩 컨테이너를 멈췄습니다 — " .. EMBED_CONTAINER)
  end
end)

magi.register_command{
  name = "engram-embed",
  description = "로컬 임베딩 서버 상태 확인/연결/기동 (/engram-embed [status|connect|start|reselect])",
  execute = function(args)
    if type(args) == "table" then args = table.concat(args, " ") end
    local sub = ((tostring(args or "")):match("^%s*(%S*)") or ""):lower()
    if sub == "" or sub == "status" then
      embed_tick()
      return embed_status()
    elseif sub == "connect" then
      embed_connect()
      return embed_status()
    elseif sub == "start" then
      embed_start(true)
      embed_tick()
      return embed_status()
    elseif sub == "reselect" then
      local rt, arch, img = embed_select_runtime(true)
      if not rt then
        return "engram: 컨테이너 런타임(docker/podman)을 찾지 못했습니다."
      end
      return "engram: 컨테이너 런타임 재선택 완료 — " .. rt .. " (arch=" .. arch .. ", image=" .. img .. ")"
    end
    return "engram: 사용법 /engram-embed [status|connect|start|reselect]"
  end,
}

-- /engram-prune-days [N] — show or set the unused-skill archive cutoff (days). Human-
-- edited skills are never archived regardless of the cutoff.
magi.register_command{
  name = "engram-prune-days",
  description = "구형 스킬 파일 자동 보관 설정 (0: 끄기, 예: /engram-prune-days 90)",
  execute = function(args)
    local n = tonumber((args or ""):match("^%s*(%d+)%s*$"))
    if not n and (args or ""):match("%S") then
      return "engram: 일수는 양의 정수로, 자동 이동을 끄려면 0으로 입력해 주세요."
    end
    if n == 0 then
      magi.store_set("prune_days", "")
      return "engram: 스킬 파일 자동 이동을 껐습니다. 관리 객체는 30일 후 cold, 90일 후 보관 후보로 표시하며 원문은 유지합니다."
    end
    if n and n > 0 then
      magi.store_set("prune_days", tostring(math.floor(n)))
      return "engram: 미사용 스킬 아카이브 기준을 " .. math.floor(n) .. "일로 설정했습니다."
    end
    if not legacy_prune_enabled() then
      return "engram: 스킬 파일 자동 이동은 꺼져 있습니다. 관리 객체는 30일 후 cold, 90일 후 보관 후보로 표시합니다. 기존 자동 이동을 켜려면 `/engram-prune-days <일수>`, 끄려면 `0`을 지정하세요."
    end
    return "engram: 현재 아카이브 기준 " .. prune_days() .. "일. 변경하려면 `/engram-prune-days <일수>`."
  end,
}

-- 회상: 최근 교훈을 컨텍스트로 주입(원장이 없으면 침묵). 행/문자 상한으로 바운드.
magi.register_context_provider{
  name = "engram-lessons",
  provide = function(q)
    embed_tick()
    if not flag("inject_ledger", true) then return {} end
    -- 의미 임베딩이 연결된 상태에서는 layered.Store가 <core_memory>와 <recalled_experience>로
    -- 기억을 회상하므로 SESSION_SUMMARY.md 표를 매 턴 중복 주입하지 않는다.
    if (embed_state.model ~= nil or user_embed_configured()) and not flag("force_inject_ledger", false) then
      return {}
    end
    -- 활성부만: 아카이브(정정으로 밀려난 옛 주장·상한 초과분)는 주입되지 않는다.
    local raw = split_ledger(magi.read_file(SUMMARY))
    if not raw or raw == "" then return {} end
    -- 관련성 게이트: 현재 질의와 토큰이 겹치는 교훈 행만 주입한다. 무관한 행(예:
    -- 세션 첫 턴의 교훈)이 매 스텝 실리면 모델 추론이 그 옛 요청을 계속 되뇐다
    -- (실측 버그 리포트). 질의가 짧아 겹침 판단이 무의미하면 최근 행으로 폴백.
    local qset, qn = tokenize(q.prompt or "")
    local rows = {}
    for line in string.gmatch(raw, "[^\n]+") do
      local t = string.gsub(line, "^%s+", "")
      -- 행 판정은 원장의 단일 판정(ledger_cells)이다 — 4라운드가 사이드카 다이제스트에서
      -- 막은 사람 표가 리콜 주입으로는 그대로 새고 있었다(5라운드).
      if ledger_cells(line) then
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
