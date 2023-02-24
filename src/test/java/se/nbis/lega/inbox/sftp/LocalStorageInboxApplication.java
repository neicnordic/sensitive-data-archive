package se.nbis.lega.inbox.sftp;

import com.google.gson.Gson;
import org.mockito.stubbing.Answer;
import org.springframework.amqp.core.MessagePostProcessor;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.SpringBootConfiguration;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.ComponentScan;
import org.springframework.web.client.RestTemplate;
import se.nbis.lega.inbox.pojo.FileDescriptor;

import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;

import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;

/**
 * Test Spring Boot application's main class with some configuration and some beans defined.
 */
@ComponentScan(basePackages = "se.nbis.lega.inbox")
@SpringBootConfiguration
public class LocalStorageInboxApplication {

    private String exchange;
    private String routingKeyFiles;

    private Gson gson;

    @Bean
    public RabbitTemplate rabbitTemplate() {
        RabbitTemplate mock = mock(RabbitTemplate.class);
        doAnswer((Answer<Void>) invocationOnMock -> {
            String routingKey = invocationOnMock.getArgument(1).toString();
            BlockingQueue<FileDescriptor> queue = routingKey.equals(routingKeyFiles) ? fileBlockingQueue() : hashBlockingQueue();
            queue.put(gson.fromJson(invocationOnMock.getArgument(2).toString(), FileDescriptor.class));
            return null;
        }).when(mock).convertAndSend(eq(exchange), anyString(), anyString(), any(MessagePostProcessor.class));
        return mock;
    }

    @Bean
    public RestTemplate restTemplate() {
        return mock(RestTemplate.class);
    }

    @Bean
    public BlockingQueue<FileDescriptor> fileBlockingQueue() {
        return new LinkedBlockingQueue<>();
    }

    @Bean
    public BlockingQueue<FileDescriptor> hashBlockingQueue() {
        return new LinkedBlockingQueue<>();
    }

    @Value("${inbox.mq.exchange}")
    public void setExchange(String exchange) {
        this.exchange = exchange;
    }

    @Value("${inbox.mq.routing-key.files}")
    public void setRoutingKeyFiles(String routingKeyFiles) {
        this.routingKeyFiles = routingKeyFiles;
    }

    @Autowired
    public void setGson(Gson gson) {
        this.gson = gson;
    }

}
