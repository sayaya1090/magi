package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.BridgeCompanionSource;
import dev.sayaya.magi.client.interfaces.api.FetchFleetCommander;
import dev.sayaya.magi.client.interfaces.api.FetchFleetRepository;
import dev.sayaya.magi.client.usecase.CompanionSource;
import dev.sayaya.magi.client.usecase.FleetCommander;
import dev.sayaya.magi.client.usecase.FleetRepository;

/** 유스케이스 포트 → interfaces 구현 바인딩. 테스트는 같은 자리에 가짜를 물린다. */
@Module
public abstract class CompanionModule {
    @Binds abstract CompanionSource source(BridgeCompanionSource impl);
    // 목록도 이 모듈의 것이다 — 컴패니언이라는 목적지의 두 얼굴(목록과 상세)이 한 모듈이다.
    @Binds abstract FleetRepository repository(FetchFleetRepository impl);
    @Binds abstract FleetCommander commander(FetchFleetCommander impl);
}
