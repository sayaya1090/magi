package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.BridgeCompanionSource;
import dev.sayaya.magi.client.interfaces.api.FetchWorkspaceSource;
import dev.sayaya.magi.client.usecase.CompanionSource;
import dev.sayaya.magi.client.usecase.WorkspaceSource;

@Module
public abstract class CodingModule {
    @Binds abstract CompanionSource source(BridgeCompanionSource impl);
    @Binds abstract WorkspaceSource workspace(FetchWorkspaceSource impl);
}
