package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.CompanionElement;
import dev.sayaya.magi.client.interfaces.FleetElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = CompanionTestModule.class)
public interface CompanionTestComponent {
    CompanionElement companionElement();
    FleetElement fleetElement();
}
