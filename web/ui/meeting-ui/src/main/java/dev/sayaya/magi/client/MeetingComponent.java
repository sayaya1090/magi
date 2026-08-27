package dev.sayaya.magi.client;

import dagger.Component;
import dev.sayaya.magi.client.interfaces.MeetingElement;

import javax.inject.Singleton;

@Singleton
@Component(modules = MeetingModule.class)
public interface MeetingComponent {
    MeetingElement meetingElement();
}
