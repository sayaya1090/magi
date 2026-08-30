package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.KnowledgeElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = KnowledgeTestModule.class)
public interface KnowledgeTestComponent {
    KnowledgeElement knowledgeElement();
}
