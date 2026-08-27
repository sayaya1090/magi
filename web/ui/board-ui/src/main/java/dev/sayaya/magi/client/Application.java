package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.RenderSharing;

/** 보드 화면 모듈(주소 v=board — 운영의 그 주소)의 진입점. */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        BoardComponent component = DaggerBoardComponent.create();
        RenderSharing.next((Render) frame -> {
            component.boardElement().mount(frame);
            return true;
        });
    }
}
