package dev.sayaya.magi.client;

import dagger.Component;
import dev.sayaya.magi.client.interfaces.MeetingElement;

import javax.inject.Singleton;

@Singleton
@Component(modules = MeetingTestModule.class)
public interface MeetingTestComponent {
    MeetingElement meetingElement();
}
