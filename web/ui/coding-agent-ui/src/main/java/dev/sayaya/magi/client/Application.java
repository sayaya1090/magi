package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.PaneSharing;
import dev.sayaya.magi.bridge.Render;

/**
 * 코딩 에이전트(타입 1)의 자식 UI — 컴패니언 패널이 내준 자리를 채운다.
 *
 * 가운데는 대화(전사), 왼쪽은 워크스페이스(파일과 git), 도크는 한 마디 보내는 상자다.
 * 세 자리 다 부모가 옷을 입혀 건넨다: 어느 상자가 창 바닥에 고정돼 있는지, 그 높이가
 * 본문의 바닥 여백이 되는지, 기둥이 열렸는지 — 자식은 하나도 모른다. 말(언어 팩)도
 * 부모가 들여놓은 뒤에 이 렌더가 불린다.
 *
 * 부모가 없으면(모듈만 단독으로 열어본 경우) 렌더는 갈 곳이 없다 — PaneSharing이 조용히
 * 무시하고, 테스트 페이지는 제 프레임에 직접 앉힌다.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        CodingComponent component = DaggerCodingComponent.create();
        PaneSharing.next("centre", (Render) frame -> {
            component.conversation().mount(frame);
            return true;
        });
        // 왼쪽은 이 타입에게 워크스페이스다 — 무엇으로 일하고 있는가.
        PaneSharing.next("left", (Render) frame -> {
            component.workspace().mount(frame);
            return true;
        });
        // 도크는 한 마디를 보내는 자리다. 이것이 창 바닥의 고정 상자라는 것은 부모의 사실이다.
        PaneSharing.next("dock", (Render) frame -> {
            component.conversation().mountComposer(frame);
            return true;
        });
    }
}
