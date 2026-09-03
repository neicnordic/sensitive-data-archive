package no.uio.ifi.localega.doa.mq;

import org.springframework.amqp.core.Queue;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
@ConditionalOnProperty(name = "outbox.enabled", havingValue = "true")
public class RabbitMQConfiguration {

    @Bean
    @ConditionalOnProperty(name = "outbox.queue-auto-create", havingValue = "true", matchIfMissing = true)
    public Queue exportQueue(@Value("${outbox.queue}") String queueName) {
        return new Queue(queueName, false, true, true);
    }
}