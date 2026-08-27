package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Labels;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.RenderSharing;

/**
 * 지식 화면 모듈(주소 v=skills — 운영의 그 주소)의 진입점. 셸에 렌더를 등록하고,
 * 언어 팩을 읽은 뒤 화면을 프레임에 앉힌다.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        KnowledgeComponent component = DaggerKnowledgeComponent.create();
        RenderSharing.next((Render) frame -> {
            Labels.load(() -> component.knowledgeElement().mount(frame));
            return true;
        });
    }
}
