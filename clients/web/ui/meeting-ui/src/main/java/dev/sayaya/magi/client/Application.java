package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.RenderSharing;

/**
 * 회의실 모듈(주소 v=meet — 운영의 그 주소, 방은 &m=)의 진입점.
 * 말도 시트도 셸이 들여놓은 뒤에 이 렌더가 불린다.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        MeetingComponent component = DaggerMeetingComponent.create();
        RenderSharing.next((Render) frame -> {
            component.meetingElement().mount(frame);
            return true;
        });
    }
}
