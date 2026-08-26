package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.FetchFleetCommander;
import dev.sayaya.magi.client.interfaces.api.FetchFleetRepository;
import dev.sayaya.magi.client.usecase.FleetCommander;
import dev.sayaya.magi.client.usecase.FleetRepository;

/**
 * 유스케이스 포트 → interfaces 구현 바인딩. 의존 방향은 안쪽(usecase)이 계약을 갖고
 * 바깥(interfaces/api)이 구현을 대는 쪽이다 — 테스트는 여기서 가짜를 물린다.
 */
@Module
public abstract class FleetModule {
    @Binds abstract FleetRepository repository(FetchFleetRepository impl);
    @Binds abstract FleetCommander commander(FetchFleetCommander impl);
}
