package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.BridgeCompanionSource;
import dev.sayaya.magi.client.interfaces.api.BridgeFleetRepository;
import dev.sayaya.magi.client.interfaces.api.FetchFleetCommander;
import dev.sayaya.magi.client.usecase.CompanionSource;
import dev.sayaya.magi.client.usecase.FleetCommander;
import dev.sayaya.magi.client.usecase.FleetRepository;

/**
 * 포트에 무엇을 물릴지 — 갈래는 없다.
 *
 * 데모냐 아니냐를 여기서 묻지 않는 이유: 그것은 화면의 성질이 아니라 <b>회선의 성질</b>이다.
 * 뒤에 데몬이 없을 때 답하는 일은 회선의 이음매(Console.raw/stream)에 걸린 목이 맡고, 그
 * 목은 데모에서만 실리는 모듈 하나에 산다(demo-ui). 그래서 이 화면은 데모를 모른다.
 */
@Module
public abstract class CompanionModule {
    @Binds abstract CompanionSource source(BridgeCompanionSource impl);

    /** 명단은 셸의 것이다 — 이 화면은 그것을 받아 그린다. */
    @Binds abstract FleetRepository repository(BridgeFleetRepository impl);

    @Binds abstract FleetCommander commander(FetchFleetCommander impl);
}
