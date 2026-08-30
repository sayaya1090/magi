package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.usecase.CompanionSource;
import dev.sayaya.magi.client.usecase.WorkspaceSource;

@Module
public abstract class CodingTestModule {
    @Binds abstract CompanionSource source(FakeCompanionSource impl);
    @Binds abstract WorkspaceSource workspace(FakeWorkspaceSource impl);
}
