package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.FetchAccessSource;
import dev.sayaya.magi.client.usecase.AccessSource;

@Module
public abstract class AccessModule {
    @Binds abstract AccessSource source(FetchAccessSource impl);
}
