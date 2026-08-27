package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;

/**
 * 화면이 세상에 대는 포트 — interfaces/api(BridgeCompanionSource)가 구현한다.
 * 셸이 있으면 전부 창 브리지 구독(요청 0 추가), 없으면(단독/테스트) 제 회선 폴백.
 */
public interface CompanionSource {
    interface Listener {
        /** 지금 보는 컴패니언 — null은 "컴패니언 화면이 아니다"(그리던 것을 세워 둔다). */
        void context(CompanionContext ctxOrNull);

        /** 전사 전체(파싱된 배열), 아직/못 읽었으면 null. */
        void transcript(Object rowsOrNull);

        /** 턴이 열려 있는가 — 진행 바의 사실. */
        void turn(boolean open, double forSec);
    }

    void start(Listener l);

    /** 명단 — 셸이 호스팅하면 그 구독으로, 단독이면 /fleet 한 번. 사실판이 읽는다. */
    void roster(java.util.function.Consumer<Object> listOrNull);

    /** 이 컴패니언의 지난 일 목록(/history). */
    void history(CompanionContext ctx, java.util.function.Consumer<Object> listOrNull);

    /** 지난 한 세션의 전사(/transcript…&session=) — 스트림이 아니라 한 번의 읽기다. */
    void pastTranscript(CompanionContext ctx, String session,
                        java.util.function.Consumer<Object> rowsOrNull);

    /**
     * 턴 곁에서 도는 것들(/jobs) — 스폰된 자식, 뒤로 돌린 명령, 그리고 줄 서 있는 말.
     * 로그에 없는 사실이라 데몬에게 묻는다(백그라운드 명령은 프로세스이고, 자식이 아직 도는지는
     * 세션 로그가 답할 수 없다).
     */
    void jobs(CompanionContext ctx, java.util.function.Consumer<Object> gotOrNull);

    /** 이 컴패니언이 남에게 건넨 일(/handoffs) — 그 일이 어떻게 되고 있는지. */
    void handoffs(CompanionContext ctx, java.util.function.Consumer<Object> listOrNull);

    /** 예약된 일(/cron) — 언제 다시 돌 것인가, 또는 왜 영영 안 도는가. */
    void cron(CompanionContext ctx, java.util.function.Consumer<Object> listOrNull);

    /** 이 컴패니언의 계획(/plan) — 오른쪽 판이 읽는다. */
    void plan(CompanionContext ctx, java.util.function.Consumer<Object> listOrNull);

    /** 컨텍스트 창(/context) — 사실판의 그 줄. */
    void context(CompanionContext ctx, java.util.function.Consumer<Object> infoOrNull);

    /** 지금 접기(/compact) — 답이 오면 컨텍스트를 다시 읽는 것은 호출자의 몫. */
    void compact(CompanionContext ctx, Runnable done);

    /** 대상 컴패니언으로 한 마디 — why는 거부 사유, 성공이면 빈 문자열. */
    void submit(CompanionContext ctx, String text, java.util.function.Consumer<String> why);

    // ── 사실판이 바꿀 수 있는 것들 ────────────────────────────────────────────
    // 읽기와 쓰기가 쌍으로 온다. 무엇이 됐는지는 <b>데몬이 말한 것</b>으로만 그린다: 거부된
    // 바꿈이 눈에 띄게 되돌아와야, 콘솔이 아무도 서 있지 않은 모드를 주장하지 않는다.

    /** 이 컴패니언이 닿을 수 있는 모델 이름들(/model) — 콘솔의 설정이 아니라 그 데몬의 대답이다. */
    void models(CompanionContext ctx, java.util.function.Consumer<Object> namesOrNull);

    /** 모델을 바꾼다(/model). */
    void model(CompanionContext ctx, String name, java.util.function.Consumer<String> why);

    /** 결재 방식을 바꾼다(/permission): ask · auto · allow · deny. */
    void permission(CompanionContext ctx, String mode, java.util.function.Consumer<String> why);

    /** 이 컴패니언이 가진 도구 이름들(/tools) — 빈 답은 "없다"가 아니라 "물어볼 수 없는 데몬"이다. */
    void tools(CompanionContext ctx, java.util.function.Consumer<Object> namesOrNull);

    /** 턴의 지도(/loop) — 갈라져 나온 세션이면 그 원본과 그 뒤의 차이도 함께. */
    void loop(CompanionContext ctx, java.util.function.Consumer<Object> shapeOrNull);

    /**
     * 결재 요청에 실을 보고서의 뼈대(/report-format)를 읽는다 — {from, sections:[{key,prompt}]}.
     * from은 그 뼈대가 어디서 왔는지다(이 워크스페이스·이 콘솔·아직 아무것도).
     */
    void reportFormat(CompanionContext ctx, java.util.function.Consumer<Object> shapeOrNull);

    /** 그 뼈대를 이 컴패니언의 워크스페이스에 쓴다 — 짝지은 key/prompt 목록. */
    void reportFormat(CompanionContext ctx, java.util.List<String> keys, java.util.List<String> prompts,
                      java.util.function.Consumer<String> why);
}
