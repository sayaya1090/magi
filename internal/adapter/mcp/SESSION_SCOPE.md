# 툴 서버를 세션별로 붙이기 — 설계와 남은 일

2026-09-04에 정한 방향이고, 아직 **안 만들었다.** 이 문서는 그 판단의 근거와 실제로 건드려야 하는
자리를 적어 둔다 — 다음에 여는 사람이 같은 측정을 다시 하지 않도록.

## 왜

PowerPoint 창을 둘 띄운 사람이 물었다: "왜 양쪽에 같은 메시지가 와? 세션이 달라야하는거 아니냐".
대화는 갈랐다(`clients/powerpoint/helper/bridges.go`). 그런데 **도구 호출은 안 갈린다.**

측정한 것(2026-09-04):

- `App.AttachToolServer` → `port.ToolServers.Attach(ctx, name, url, headers)` → `mcp.Manager.Attach`.
  어디에도 세션이 없다. 등록은 **데몬 전체**를 덮는다.
- 턴의 도구 목록은 `App.toolSpecs(agent)` 가 `a.tools.List()` 로 만든다. 여기에도 세션이 없다.
- 그래서 한 데몬에 덱 둘이 붙으면, 호출만 보고는 어느 덱인지 **알 길이 없다.** 헬퍼가 활성 문서를
  고르던 것이 그 자리였고, 그 추측이 틀리면 **사람이 보고 있지도 않은 덱이 고쳐진다.**

지금은 「고르지 않는다」로 막아 뒀다(`hand.go` 의 `pick`, DESIGN §4.4 ④ 2026-09-04 개정).
안전하지만 덱 둘을 동시에 쓰지는 못한다.

## 무엇을 만드는가

`Attach` 에 **주인**(세션)을 더한다. 주인이 빈 것은 여태처럼 데몬 전체다.

```
Attach(ctx, name, url, headers)          →  Attach(ctx, owner, name, url, headers)
toolSpecs(agent)                         →  toolSpecs(sid, agent)
```

- 주인이 있는 서버의 도구는 **그 세션에만 광고**된다.
- 실행도 같은 규칙으로 막는다(광고만 막으면 이름을 아는 모델이 그냥 부른다).

## 여기가 어렵다 — 이름 충돌

덱 둘이 같은 이름(`ppt`)으로 붙는다. 등록 이름은 `mcp__ppt__list_slides` 로 **똑같고**, 레지스트리는
이름으로 키를 잡는 평평한 맵이다(`internal/adapter/tool/builtin/registry.go`). 지금은 나중 등록이
앞의 것을 **조용히 덮는다.**

세 갈래를 재 봤다:

1. **서버 이름을 덱별로**(`ppt`, `ppt-a1b2`) — 충돌은 없어지지만 모델이 보는 도구 이름이 덱마다
   다르다. 광고할 때 이름을 되돌리려면 호출 때 다시 매핑해야 한다.
2. **레지스트리를 세션별로** — 조회 경로가 전부 세션을 받아야 한다. 모든 클라이언트 공용이다.
3. **도구 하나가 주인별 클라이언트를 든다**(권장) — 레지스트리는 그대로 평평하다.
   `mcpTool` 이 `client` 하나 대신 `owners map[session.SessionID]*Client` 와 주인 없는 기본 하나를
   들고, `Execute` 는 `env.SessionID` 로 고른다. 같은 이름으로 다른 주인이 붙으면 **덮지 않고
   합친다.** 광고는 그 맵에 자기 세션이 있는지로 정한다.

셋째가 레지스트리를 안 건드리므로 폭이 제일 좁다.

## 건드리는 자리

- `internal/port/port.go` — `ToolServers.Attach` 서명, 도구의 「누구에게 보이는가」.
- `internal/app/app.go` — `AttachToolServer` 가 주인을 나른다.
- `internal/app/prompt.go`·`prompt_frozen.go` — `toolSpecs` 가 세션을 받는다(`sessionToolSpecs` 가
  이미 세션을 들고 있으므로 한 인자만 내려보내면 된다).
- `internal/adapter/mcp/{manager,tool}.go` — 주인별 병합과 라우팅.
- `internal/adapter/daemon/{protocol,doors,client}.go` — `mcp-attach` 가 세션을 싣는다.
- `clients/powerpoint/helper/{attach,main}.go` — 덱의 세션으로 붙이고, `MCPURL(port, deck)` 의
  덱을 그때 채운다(지금은 공용 컴패니언에 안 채운다 — 덮이면 거짓이 되므로).

## 하지 말 것

- **주소에 덱을 적어 두고 세션 범위 없이 쓰기.** 등록이 소켓당 한 벌이라 나중 등록이 앞의 것을
  덮고, 주소가 가리키는 덱은 마지막에 붙은 덱이 된다 — 조용히 남의 덱을 고친다. 지금의
  「못 고른다」보다 나쁘다.
