package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Labels;
import dev.sayaya.magi.bridge.PaneSharing;
import dev.sayaya.magi.bridge.Render;

/**
 * 코딩 에이전트(타입 1)의 자식 UI — 컴패니언 패널이 내준 자리를 채운다.
 *
 * 가운데는 대화다: 전사와, 그 대화로 한 마디 보내는 상자. 왼쪽은 워크스페이스다:
 * 파일 트리와 git. 위와 오른쪽은 부모의 몫이라 여기서 그리지 않는다.
 *
 * 부모가 없으면(모듈만 단독으로 열어본 경우) 렌더가 갈 곳이 없다 — PaneSharing이 조용히
 * 무시하고, 테스트 페이지는 제 프레임에 직접 앉힌다.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        CodingComponent component = DaggerCodingComponent.create();
        PaneSharing.next("centre", (Render) frame -> {
            Labels.load(() -> component.conversation().mount(frame));
            return true;
        });
        // 왼쪽은 이 타입에게 워크스페이스다 — 무엇으로 일하고 있는가.
        PaneSharing.next("left", (Render) frame -> {
            Labels.load(() -> component.workspace().mount(frame));
            return true;
        });
    }
}
