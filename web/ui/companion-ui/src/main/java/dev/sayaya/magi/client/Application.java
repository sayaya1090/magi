package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Labels;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.RenderSharing;

/**
 * 컴패니언 화면 모듈(타입 1 = 코딩 에이전트)의 진입점 — 타입 전용 UI 모듈이 지켜야 할
 * 계약의 레퍼런스 구현: 셸에 렌더를 등록하고, 컨텍스트(CompanionContext)는 usecase 가
 * 브리지 구독으로 받는다.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        CompanionComponent component = DaggerCompanionComponent.create();
        RenderSharing.next((Render) frame -> {
            Labels.load(() -> {
                // 마운트는 프라미스 콜백 안이라 예외가 조용히 삼켜진다 — 창에 적어 보이게 한다.
                try {
                    component.companionElement().mount(frame);
                } catch (Exception e) {
                    jsinterop.base.Js.asPropertyMap(elemental2.dom.DomGlobal.window)
                            .set("__magi_boot_err", String.valueOf(e));
                    throw e;
                }
            });
            return true;
        });
    }
}
