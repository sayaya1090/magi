package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.RenderSharing;

/** 맵 화면 모듈(주소 v=map — 운영의 그 주소)의 진입점. */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        MapComponent component = DaggerMapComponent.create();
        RenderSharing.next((Render) frame -> {
            component.mapElement().mount(frame);
            return true;
        });
    }
}
