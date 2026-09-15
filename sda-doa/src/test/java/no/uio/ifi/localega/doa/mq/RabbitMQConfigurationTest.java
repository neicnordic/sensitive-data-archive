package no.uio.ifi.localega.doa.mq;

import org.junit.jupiter.api.Test;
import org.springframework.amqp.core.Queue;
import org.springframework.boot.test.context.runner.ApplicationContextRunner;

import static org.assertj.core.api.Assertions.assertThat;

class RabbitMQConfigurationTest {

    private final ApplicationContextRunner contextRunner = new ApplicationContextRunner()
            .withUserConfiguration(RabbitMQConfiguration.class);

    @Test
    void createsQueueWithExistingDefaultsWhenAutoCreationIsNotConfigured() {
        contextRunner
                .withPropertyValues(
                        "outbox.enabled=true",
                        "outbox.queue=custom-export-requests"
                )
                .run(context -> {
                    assertThat(context).hasSingleBean(Queue.class);
                    Queue queue = context.getBean(Queue.class);
                    assertThat(queue.getName()).isEqualTo("custom-export-requests");
                    assertThat(queue.isDurable()).isFalse();
                    assertThat(queue.isExclusive()).isTrue();
                    assertThat(queue.isAutoDelete()).isTrue();
                });
    }

    @Test
    void doesNotCreateQueueWhenAutoCreationIsDisabled() {
        contextRunner
                .withPropertyValues(
                        "outbox.enabled=true",
                        "outbox.queue=custom-export-requests",
                        "outbox.queue-auto-create=false"
                )
                .run(context -> assertThat(context).doesNotHaveBean(Queue.class));
    }

    @Test
    void doesNotCreateQueueWhenOutboxIsDisabled() {
        contextRunner
                .withPropertyValues(
                        "outbox.enabled=false",
                        "outbox.queue=custom-export-requests"
                )
                .run(context -> assertThat(context).doesNotHaveBean(Queue.class));
    }
}