package app

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/lang"
	"github.com/sayaya1090/magi/internal/core/report"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// InteractiveOpened is called only after a live UI subscribes. Asking is native:
// no model request, project survey or synthetic turn runs before explicit consent.
func (a *App) InteractiveOpened(ctx context.Context, sid session.SessionID, locale string) {
	if !a.cfg.Interactive || a.cfg.CodingTaskExecution || a.cfg.CompanionProfileUnpublished == nil || a.profileIntroNudged.Load() || ctx.Err() != nil {
		return
	}
	s := a.sessionInfo(ctx, sid)
	if s.Workdir == "" || !a.cfg.CompanionProfileUnpublished(s.Workdir) {
		return
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return
	}
	for _, ev := range evs {
		if ev.Type == event.TypeSessionCreated {
			var d event.SessionCreatedData
			if json.Unmarshal(ev.Data, &d) == nil && d.Parent != "" {
				return
			}
		}
	}
	korean := strings.HasPrefix(strings.ToLower(locale), "ko") || (locale == "" && lang.Detect(lastUserPromptText(evs)) == "Korean (한국어)")
	q, prompt := companionIntroductionQuestion(korean)
	a.mu.Lock()
	if a.closed || a.holding || a.stateLocked(sid).cancel != nil || !a.profileIntroNudged.CompareAndSwap(false, true) {
		a.mu.Unlock()
		return
	}
	askCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	a.profileIntroCancel = cancel
	a.wg.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.wg.Done()
		defer cancel()
		stop := context.AfterFunc(a.bgContext(), cancel)
		defer stop()
		log.Printf("companion profile: startup approval requested")
		callID := "companion-introduction-" + newID()
		ask := a.askUserFn(askCtx, s, 0, &session.ToolCall{CallID: callID, Name: "ask_user"})
		answer, err := ask(q)
		if err != nil || answer != q.Options[0] {
			if err != nil {
				data, _ := json.Marshal(event.QuestionAnsweredData{CallID: callID + "#1", Answer: ""})
				a.publishTransient(sid, event.TypeQuestionAnswered, event.Actor{Kind: event.ActorSystem, ID: "companion-introduction"}, data)
			}
			if ctx.Err() != nil {
				a.profileIntroNudged.Store(false)
			}
			log.Printf("companion profile: startup survey deferred")
			return
		}
		if askCtx.Err() != nil || !a.cfg.CompanionProfileUnpublished(s.Workdir) {
			return
		}
		log.Printf("companion profile: startup survey approved")
		// A real menu choice becomes the user's scoped request, just as a composer
		// suggestion does. No old task is replayed and no permission mode is changed.
		if err := a.Submit(askCtx, command.SubmitPrompt{SessionID: sid, Actor: event.Actor{Kind: event.ActorUser, ID: "companion-introduction"}, Parts: []session.Part{{Kind: session.PartText, Text: prompt}}}); err != nil {
			log.Printf("companion profile: cannot start approved survey: %v", err)
		}
	}()
}

func companionIntroductionQuestion(korean bool) (port.Question, string) {
	question := "This companion has no offered work yet. May I inspect project documentation and relevant source to describe what other companions can request?"
	options := []string{"Inspect and describe offered work", "Not now"}
	tried := "No offered tasks or callable operations are registered. No project survey or model request has started."
	stakes := "Approve: add a scoped request to this chat and use the model to read relevant files, then fill missing descriptions. Defer or dismiss: no survey. Access permissions, callable operations and source files stay unchanged."
	lean := "Inspect when you want other companions to discover this workspace; otherwise defer."
	prompt := "I approved the startup workspace survey. Read relevant project documentation and source only to identify this workspace's purpose, responsibilities and concrete work other companions may request, including required inputs, expected outputs and limits. Do not change project files, run builds, grant access or publish callable operations. Preserve user-authored descriptions. Summarize your findings; the harness will extract missing metadata after the turn. Do not resume previous development tasks."
	if korean {
		question = "아직 이 컴패니언이 제공할 작업이 등록되지 않았습니다. 프로젝트 문서와 관련 소스를 읽고 다른 컴패니언이 요청할 수 있는 작업을 정리할까요?"
		options = []string{"살펴보고 제공 작업 정리", "나중에"}
		tried = "제공 작업과 직접 호출 기능이 모두 비어 있습니다. 아직 프로젝트 조사나 모델 요청을 시작하지 않았습니다."
		stakes = "승인하면 이 채팅에 조사 요청을 추가하고 모델이 관련 파일을 읽어 빈 설명을 정리합니다. 보류하거나 창을 닫으면 조사하지 않습니다. 접근 권한·직접 호출 도구·프로젝트 파일은 변경하지 않습니다."
		lean = "다른 컴패니언이 이 작업공간을 찾고 일을 요청하게 하려면 정리하고, 지금 필요하지 않으면 보류합니다."
		prompt = "시작 안내에서 프로젝트 조사를 승인했습니다. 관련 개발문서와 소스를 읽어 이 작업공간의 목적·담당 범위와 다른 컴패니언이 요청할 수 있는 구체적인 작업, 필요한 입력·기대 결과·제약을 정리해 주세요. 프로젝트 파일 변경, 빌드 실행, 접근 권한 추가, 직접 호출 도구 등록은 하지 마세요. 사용자가 작성한 기존 설명을 보존하세요. 조사 결과를 요약하면 턴 종료 후 하네스가 비어 있는 설명을 추출합니다. 이전 개발 작업을 재개하지 마세요."
	}
	return port.Question{Text: question, Options: options, Grounds: report.Default.Fill(map[string]string{"tried": tried, "stakes": stakes, "lean": lean}), Index: 1, Total: 1}, prompt
}
