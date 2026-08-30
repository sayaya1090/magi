package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.RenderSharing;

/** 접근 화면 모듈(주소 v=access — 운영의 그 주소)의 진입점. */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        AccessComponent component = DaggerAccessComponent.create();
        RenderSharing.next((Render) frame -> {
            component.accessElement().mount(frame);
            return true;
        });
    }
}
