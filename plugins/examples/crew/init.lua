-- crew — 격리된 체크아웃에서 일하는 워커 하나. 이 예제가 존재하는 이유는 조립이다.
--
-- magi는 스폰·격리·판정·병합·되돌리기·발자국 읽기를 각각 제공하는데, 그 조각들이 실제로
-- 맞물리는지는 아무도 엮어보기 전까지 모른다. 이 파일이 그 전부를 한 번에 쓴다:
--
--   magi.spawn{workspace="clone", review=…}   자기 체크아웃에서 일하고, 끝났다고 하면 판정
--   magi.child_steps(sid)                     무엇을 했는지 (말이 아니라 발자국)
--   magi.merge_child(sid)                     받아들이면 부모 트리에 얹기
--   magi.restore_child(sid)                   받아들이지 않으면 되돌리기
--
-- 대장이 판정하는 방식이 이 예제의 요점이다. 자식의 마지막 한 마디는 자식의 자기 보고이고,
-- 빌드를 돌려보고 실패한 자식과 아예 안 돌린 자식은 같은 문장으로 끝난다. 그래서 판정은
-- 발자국을 본다 — 무엇을 불렀고, 무엇이 실패했는지.

local ISOLATED_WORKER = [[
너는 워커다. 너에게 주어진 것은 이 저장소의 **네 전용 체크아웃**이다 — 여기서 무엇을 고치든
다른 누구의 작업과 부딪히지 않는다. 마음 놓고 고치되, 고친 것이 동작한다는 것을 **직접 확인**한다.

절차:
1. 요구사항이 무엇을 요구하는지 파악하고, 관련 코드를 읽는다.
2. 고친다.
3. **검증한다.** 빌드하고 테스트를 돌린다. 돌려보지 않은 코드로 끝내지 않는다.
4. 무엇을 왜 고쳤는지, 무엇으로 확인했는지 한 문단으로 답한다.

확인하지 않은 것을 확인했다고 말하지 않는다. 막혔으면 무엇을 시도했고 어떤 오류가 났는지
그대로 적는다 — 그게 다음 판단의 재료다.
]]

-- ran_command(steps, needle) — 자식이 **그 명령을** 실제로 돌렸는가.
--
-- "bash를 불렀는가"로는 부족하다. `ls`도 bash고, 빌드를 돌리라고 시킨 워커가 `cat`만 하고
-- "동작한다"고 끝내는 것이 이 저장소가 가장 자주 본 거짓 완료다. 그래서 인자 안의 명령문을 본다.
-- 실패한 호출도 센다 — **돌려보고 실패한 것과 아예 안 돌린 것은 다른 사건**이고, 다음 라운드에
-- 시킬 말이 서로 다르다.
local function ran_command(steps, needle)
  for _, s in ipairs(steps) do
    local cmd = type(s.args) == "table" and (s.args.command or s.args.cmd)
    if s.name == "bash" and type(cmd) == "string" and cmd:find(needle, 1, true) then
      return true, s.failed
    end
  end
  return false, false
end

-- failures(steps) — 실패한 호출들을 원문 그대로 모은다. 요약하지 않는 것이 요점이다:
-- 다음 라운드가 필요한 이유가 바로 그 원문이고, 그것만은 재구성할 수 없다.
local function failures(steps)
  local out = {}
  for _, s in ipairs(steps) do
    if s.failed then out[#out+1] = s.name .. ": " .. (s.output or "") end
  end
  return out
end

magi.register_tool{
  name = "crew_work",
  subagent = true,
  enabled = false,          -- 기본 꺼짐. magi는 에이전트를 기본으로 제공하지 않는다.
  -- 쓰는 자식마다 자기 체크아웃. 아래 spawn이 workspace="clone"을 이미 적듯 이 툴의 계약이고,
  -- 선언하면 호스트가 스폰 지점에서 강제한다 — 그리고 그 힘으로 한 스텝이 crew_work를 두 번
  -- 부르면 두 워커가 동시에 돈다(서로 다른 트리라 부딪힐 것이 없다).
  isolated_children = true,
  group = "crew",
  description = "격리된 체크아웃에서 워커에게 작업을 시킨다. 워커가 끝났다고 하면 그 발자국을 " ..
    "보고 판정하고, 받아들이면 변경을 이 트리에 병합하고 아니면 되돌린다. 여러 파일에 걸친 " ..
    "구현 작업에 쓴다 — 읽기만 하면 될 일에는 쓰지 말 것(격리 비용만 든다).",
  schema = {
    type = "object",
    properties = {
      task = { type = "string", description = "워커가 할 일. 사용자가 말한 그대로." },
      verify = { type = "string", description = "검증 명령. 워커가 이걸 돌렸는지로 판정한다. 예: go test ./..." },
    },
    required = { "task" },
  },
  execute = function(args)
    local task = args and args.task
    if not task or task == "" then return "crew_work: task가 비어 있다", true end
    local verify = args.verify

    local last_reason
    local r = magi.spawn{
      system    = ISOLATED_WORKER,
      prompt    = task,                    -- verbatim. 여기서 다시 쓰지 않는다.
      workspace = "clone",                 -- 자기 체크아웃
      groups    = { "crew" },              -- 이 그룹의 스킬만 보인다
      tools     = { "read", "grep", "glob", "list", "write", "edit", "multiedit", "bash" },
      max_steps = 40,
      timeout   = 900,

      -- 판정. 자식이 "끝났다"고 할 때마다 불린다. 문자열을 돌려주면 같은 세션에서 계속한다 —
      -- 새 자식이 아니므로 이미 읽은 것을 다시 읽지 않는다.
      -- session_id가 인자로 온다. 이 콜백은 spawn이 반환하기 전에 불리므로 r은 아직 nil이고,
      -- 그것 없이는 자식의 마지막 한 마디밖에 읽을 것이 없다 — 발자국이 검사하려는 바로 그 진술.
      review = function(round, text, steps_used, session_id)
        local steps = magi.child_steps(session_id)
        if not steps then return nil end   -- 발자국을 못 읽으면 판정할 근거가 없다. 받아들인다.

        local bad = failures(steps)
        if #bad > 0 and round < 3 then
          last_reason = "아직 실패가 남아 있다:\n" .. table.concat(bad, "\n")
          return last_reason
        end
        -- 검증 명령이 지정됐으면, **그 명령을** 실제로 돌렸는지 본다.
        if verify and round < 3 then
          local tried, failed = ran_command(steps, verify)
          if not tried then
            last_reason = "검증을 돌린 흔적이 없다. `" .. verify .. "` 를 실제로 실행하고 그 출력을 근거로 답하라."
            return last_reason
          end
          if failed then
            last_reason = "`" .. verify .. "` 가 실패한 채로 끝났다. 출력을 근거로 고치거나, " ..
              "고칠 수 없으면 무엇이 막는지 그대로 적어라."
            return last_reason
          end
        end
        return nil                          -- 수락
      end,
    }

    if r.err and r.err ~= "" then
      -- 끝까지 못 간 작업의 잔재를 남길지는 부르는 쪽 판단이다. 여기서는 되돌린다 —
      -- 이 툴의 계약이 "받아들이거나 되돌리거나"이기 때문이다.
      local undone = {}
      for _, p in ipairs(magi.restore_child(r.session_id) or {}) do
        if not p.restored then undone[#undone+1] = p.path .. " (" .. (p.reason or "") .. ")" end
      end
      local msg = "워커가 끝내지 못했다: " .. r.err .. "\n\n" .. (r.text or "")
      if #undone > 0 then
        -- 절반만 되돌리고 깨끗하다고 말하는 것이 아무것도 안 하는 것보다 나쁘다.
        msg = msg .. "\n\n⚠ 되돌리지 못한 것:\n" .. table.concat(undone, "\n")
      end
      return msg, true
    end

    -- 아무것도 안 바꾼 자식. 커밋 범위가 비었다는 것은 판단이 아니라 사실이고, 가장 싼
    -- 거짓 완료 탐지다 — "고쳤다"는 문단과 빈 diff가 함께 오는 경우.
    if r.base_commit ~= "" and r.base_commit == r.head_commit then
      return "워커가 끝났다고 했지만 **바꾼 것이 없다**(커밋 범위가 비었다).\n\n" .. (r.text or ""), true
    end

    local ok, err = magi.merge_child(r.session_id)
    if not ok then
      return "워커는 끝냈지만 병합하지 못했다: " .. tostring(err) ..
        "\n\n워커의 체크아웃은 " .. (r.workspace or "?") .. " 에 그대로 있다 (" ..
        (r.base_commit or "?"):sub(1, 7) .. ".." .. (r.head_commit or "?"):sub(1, 7) .. ").\n\n" ..
        (r.text or ""), true
    end
    return (r.text or "") .. "\n\n(워커의 변경을 이 트리에 병합했다. 커밋하지 않았으므로 " ..
      "`git diff` 로 읽을 수 있다.)"
  end,
}
