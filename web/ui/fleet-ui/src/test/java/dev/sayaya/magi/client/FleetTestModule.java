package dev.sayaya.magi.client;

import dagger.Binds;
import dagger.Module;
import dev.sayaya.magi.client.usecase.FleetCommander;
import dev.sayaya.magi.client.usecase.FleetRepository;

@Module
public abstract class FleetTestModule {
    @Binds abstract FleetRepository repository(FakeFleetRepository impl);
    @Binds abstract FleetCommander commander(FakeFleetCommander impl);
}
