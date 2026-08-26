package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.FleetElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = FleetTestModule.class)
public interface FleetTestComponent {
    FleetElement fleetElement();
}
