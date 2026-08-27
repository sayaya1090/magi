package dev.sayaya.magi.client;

import dagger.Module;
import dagger.Provides;
import dev.sayaya.magi.bridge.Demo;
import dev.sayaya.magi.client.interfaces.api.BridgeCompanionSource;
import dev.sayaya.magi.client.interfaces.api.BridgeFleetRepository;
import dev.sayaya.magi.client.interfaces.api.DemoCompanionSource;
import dev.sayaya.magi.client.interfaces.api.DemoFleetCommander;
import dev.sayaya.magi.client.interfaces.api.FetchFleetCommander;
import dev.sayaya.magi.client.usecase.CompanionSource;
import dev.sayaya.magi.client.usecase.FleetCommander;
import dev.sayaya.magi.client.usecase.FleetRepository;

import javax.inject.Provider;
import javax.inject.Singleton;

/**
 * 포트에 무엇을 물릴지 — 그리고 <b>데모면 이 모듈 제 목</b>을 문다.
 *
 * 목이 모듈마다 있는 이유는 배포가 모듈마다이기 때문이다: 화면은 저마다 컴파일돼 저마다의
 * 주기로 나가고, 제 창에서 제 회선으로 말한다. 목이 그 옆에 실려 있어야 그 배포에 따라붙고,
 * 페이지가 남의 창에 손을 넣는 일도 없다.
 */
@Module
public abstract class CompanionModule {
    @Provides
    @Singleton
    static CompanionSource source(Provider<BridgeCompanionSource> live, Provider<DemoCompanionSource> demo) {
        return Demo.on() ? demo.get() : live.get();
    }

    /** 명단은 셸의 것이다 — 데모에서도 마찬가지라(셸이 제 목을 갖는다) 갈래가 없다. */
    @Provides
    @Singleton
    static FleetRepository repository(BridgeFleetRepository impl) { return impl; }

    @Provides
    @Singleton
    static FleetCommander commander(Provider<FetchFleetCommander> live, Provider<DemoFleetCommander> demo) {
        return Demo.on() ? demo.get() : live.get();
    }
}
