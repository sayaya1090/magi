package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Labels;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.RenderSharing;

/**
 * 컴패니언이라는 목적지의 주인 — 목록과 상세, 두 얼굴이 한 모듈이다.
 *
 * 목록(주소에 ?d= 가 없을 때)은 이 모듈이 그린다: 어떤 타입의 컴패니언이든 표에서는 같은
 * 것을 답하기 때문이다(무엇이 기다리고, 무엇이 도는가). 상세는 레이아웃만 이 모듈의
 * 것이고 — 위의 사실판과 오른쪽 판 — 가운데와 왼쪽은 타입의 자식 UI가 정의한다
 * (PaneSharing 슬롯). 자식의 이름은 셸의 카탈로그가 풀어 컨텍스트에 실어 보낸다.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        CompanionComponent component = DaggerCompanionComponent.create();
        RenderSharing.next((Render) frame -> {
            Labels.load(() -> {
                // 주소가 컴패니언을 대면 상세, 아니면 목록 — 판단은 셸이 흘리는 컨텍스트가 한다.
                boolean detail = dev.sayaya.magi.bridge.Windows.companionAimed();
                // 마운트는 프라미스 콜백 안이라 예외가 조용히 삼켜진다 — 창에 적어 보이게 한다.
                try {
                    if (detail) component.companionElement().mount(frame);
                    else component.fleetElement().mount(frame);
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
