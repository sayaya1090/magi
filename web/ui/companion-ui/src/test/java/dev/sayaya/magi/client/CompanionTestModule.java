package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.usecase.CompanionSource;

@Module
public abstract class CompanionTestModule {
    @Binds abstract CompanionSource source(FakeCompanionSource impl);
}
