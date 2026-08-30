package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.BridgeCompanionSource;
import dev.sayaya.magi.client.interfaces.api.FetchWorkspaceSource;
import dev.sayaya.magi.client.usecase.CompanionSource;
import dev.sayaya.magi.client.usecase.WorkspaceSource;

/**
 * 포트에 무엇을 물릴지 — 갈래는 없다. 뒤에 데몬이 없을 때 답하는 일은 회선의 이음매에 걸린
 * 목이 맡고(demo-ui), 그래서 이 화면은 데모를 모른다.
 */
@Module
public abstract class CodingModule {
    @Binds abstract CompanionSource source(BridgeCompanionSource impl);

    @Binds abstract WorkspaceSource workspace(FetchWorkspaceSource impl);
}
