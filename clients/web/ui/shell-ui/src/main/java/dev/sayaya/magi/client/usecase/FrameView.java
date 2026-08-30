package dev.sayaya.magi.client.usecase;

/** 프레임이 유스케이스에 보이는 면: 렌더(브리지의 Render, Object로 나른다)를 앉히는 것뿐. */
public interface FrameView {
    void mount(Object render);
}
