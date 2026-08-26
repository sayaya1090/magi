package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.FleetElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = FleetModule.class)
public interface FleetComponent {
    FleetElement fleetElement();
}
