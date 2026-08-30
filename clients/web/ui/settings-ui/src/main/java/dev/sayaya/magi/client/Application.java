package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.May;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.RenderSharing;

/**
 * 환경설정 화면(주소 v=settings — 운영의 그 주소)의 진입점.
 * 능력(무엇을 할 수 있는 사람인가)은 창에 하나라, 그것이 도착한 뒤에 그린다.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        SettingsComponent component = DaggerSettingsComponent.create();
        RenderSharing.next((Render) frame -> {
            May.load(() -> component.settingsElement().mount(frame));
            return true;
        });
    }
}
