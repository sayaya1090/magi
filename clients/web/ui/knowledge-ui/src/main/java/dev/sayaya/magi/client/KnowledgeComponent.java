package dev.sayaya.magi.client;

import dev.sayaya.magi.client.interfaces.KnowledgeElement;

import javax.inject.Singleton;

@Singleton
@dagger.Component(modules = KnowledgeModule.class)
public interface KnowledgeComponent {
    KnowledgeElement knowledgeElement();
}
