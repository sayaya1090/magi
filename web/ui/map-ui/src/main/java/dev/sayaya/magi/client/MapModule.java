package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.interfaces.api.FetchMapSource;
import dev.sayaya.magi.client.usecase.MapSource;

@Module
public abstract class MapModule {
    @Binds abstract MapSource source(FetchMapSource impl);
}
