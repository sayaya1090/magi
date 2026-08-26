package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;

/**
 * 컴패니언 상세 — "기본" 타입 UI이자, 타입 전용 UI 모듈이 지켜야 할 계약의 레퍼런스 구현.
 * CompanionContext(socket·peer·type)를 받아 대화(SSE)+컴포저·사실판·워크스페이스를 그린다.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        // TODO: RenderSharing.register(...) — CompanionContext 구독 후 렌더
    }
}
