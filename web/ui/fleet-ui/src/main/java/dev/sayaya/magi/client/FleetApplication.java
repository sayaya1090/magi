package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Labels;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.RenderSharing;

/**
 * 플릿 화면 모듈의 진입점 — 셸에 렌더를 등록하고, 언어 팩(기존 콘솔과 같은 파일)을
 * 읽은 뒤 Dagger 그래프를 세워 화면을 프레임에 앉힌다.
 */
public class FleetApplication implements EntryPoint {
    @Override
    public void onModuleLoad() {
        FleetComponent component = DaggerFleetComponent.create();
        RenderSharing.next((Render) frame -> {
            Labels.load(() -> component.fleetElement().mount(frame));
            return true;
        });
    }
}
