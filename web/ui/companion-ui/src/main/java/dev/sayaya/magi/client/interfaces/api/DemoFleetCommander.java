package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.usecase.FleetCommander;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 데모에서 누르는 것들 — 받아들이고 아무 데도 보내지 않는다.
 *
 * 거절하지 않는 이유: 데모의 요점은 "이 콘솔이 무엇을 할 수 있나"이고, 눌러도 아무 일이
 * 없는 컨트롤은 그 답을 못 준다. 진짜로 무엇이 일어났다고 말하지도 않는다 — 뒤에 데몬이 없다.
 */
@Singleton
public class DemoFleetCommander implements FleetCommander {
    @Inject
    public DemoFleetCommander() {}

    @Override
    public void interrupt(FleetAgent a, Runnable then) { then.run(); }

    @Override
    public void answer(FleetAgent a, String text, Runnable then) { then.run(); }
}
