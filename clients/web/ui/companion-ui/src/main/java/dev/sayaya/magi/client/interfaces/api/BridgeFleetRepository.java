package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.magi.client.usecase.FleetRepository;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 명단은 <b>셸의 것</b>이다 — 이 화면은 그것을 받아 그린다.
 *
 * 이 클래스가 회선을 갖지 않는 이유: /fleet과 /events를 셸도 읽고 여기서도 읽으면 한 API에
 * 주인이 둘이고, 그것은 단일 원천이 아니라는 증거다(창당 스트림 하나라는 규칙도 그 폴백이
 * 깨고 있었다). 셸이 없는 자리 — 이 모듈만 띄운 테스트 페이지 — 에서는 그래프가 가짜를
 * 물고, 데모에서는 데모 목을 문다. 어느 쪽이든 <b>여기서 회선을 열지는 않는다</b>.
 */
@Singleton
public class BridgeFleetRepository implements FleetRepository {
    @Inject
    public BridgeFleetRepository() {}

    @Override
    public void watch(RosterHandler h) {
        RosterSharing.subscribe(o -> h.roster(o == null ? null : Js.uncheckedCast(o)));
    }

    @Override
    public void refresh() { RosterSharing.refresh(); }
}
