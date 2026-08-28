package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.FrameElement;
import dev.sayaya.magi.client.interfaces.RailElement;
import dev.sayaya.magi.client.interfaces.api.FetchRosterSource;
import dev.sayaya.magi.client.interfaces.api.ScriptModuleLoader;
import dev.sayaya.magi.client.usecase.FrameView;
import dev.sayaya.magi.client.usecase.ModuleLoader;
import dev.sayaya.magi.client.usecase.RailView;
import dev.sayaya.magi.client.usecase.RosterSource;


/** 유스케이스 포트 → interfaces 구현 바인딩. 데모면 이 모듈 제 목을, 테스트는 가짜를 문다. */
@Module
public abstract class ShellModule {
    @Binds abstract ModuleLoader loader(ScriptModuleLoader impl);
    @Binds abstract RailView rail(RailElement impl);
    @Binds abstract FrameView frame(FrameElement impl);

    /**
     * 명단과 스트림의 주인은 셸이다 — 회선 하나. 데모냐 아니냐는 <b>여기서 묻지 않는다</b>:
     * 그것은 화면의 성질이 아니라 회선의 성질이고, 그 자리에 목이 선다(bridge.Demo/demo-ui).
     */
    @Binds abstract RosterSource roster(FetchRosterSource impl);
}
