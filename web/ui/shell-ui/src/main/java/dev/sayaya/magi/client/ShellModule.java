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

/** 유스케이스 포트 → interfaces 구현 바인딩. 테스트는 같은 자리에 가짜를 물린다. */
@Module
public abstract class ShellModule {
    @Binds abstract ModuleLoader loader(ScriptModuleLoader impl);
    @Binds abstract RailView rail(RailElement impl);
    @Binds abstract FrameView frame(FrameElement impl);
    @Binds abstract RosterSource roster(FetchRosterSource impl);
}
